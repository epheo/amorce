package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/epheo/amorce/internal/site"
)

// State is a snapshot; frontends render it and never mutate it.
type State struct {
	Step       Step
	Machine    Machine
	Discovered bool
	Problem    string // discovery failure, shown on every screen

	Keymaps  []string
	Keyboard string

	Purposes  []Purpose
	PurposeID string

	Net        NetReport
	NetTesting bool
	NetChosen  bool

	Plan     DiskPlan
	Hostname string

	// Site is set when a site file on the media names this machine.
	Site *SiteRun
	// SiteFile is the media's site file, kept as is on the installed system.
	SiteFile []byte
	// ReportTo is the first machine, from the site file; nil reports nowhere.
	ReportTo *site.Report
	// SiteNames are the machines the site file names.
	SiteNames []string
	// SSHKeys come from the site file; the wizard has no screen for them.
	SSHKeys []string

	// Profile is "worker" when this machine joins the first machine's
	// cluster; ReportTo names that machine.
	Profile string
	// Clusters are the first machines heard on the network.
	Clusters []Cluster
	// Join is the attended join under way or done, if any.
	Join *JoinState

	Install InstallState
}

func (s State) Purpose() (Purpose, bool) {
	for _, p := range s.Purposes {
		if p.ID == s.PurposeID {
			return p, true
		}
	}
	return Purpose{}, false
}

func (s State) Disk(name string) (Disk, bool) {
	for _, d := range s.Machine.Disks {
		if d.Name == name {
			return d, true
		}
	}
	return Disk{}, false
}

type Engine struct {
	sys System

	mu    sync.Mutex
	st    State
	subs  map[int]chan struct{}
	next  int
	netGo int // generation of the latest network test
}

func New(sys System) *Engine {
	return &Engine{sys: sys, subs: map[int]chan struct{}{}, st: State{Keyboard: "us"}}
}

// Start discovers the machine in the background.
func (e *Engine) Start(ctx context.Context) {
	e.update(func(s *State) { s.Keymaps = e.sys.Keymaps() })
	go func() {
		m, err := e.sys.Discover(ctx)
		ps, perr := e.sys.Purposes(ctx)
		e.update(func(s *State) {
			s.Machine, s.Discovered = m, true
			s.Purposes = ps
			switch {
			case err != nil:
				s.Problem = err.Error()
			case perr != nil:
				s.Problem = perr.Error()
			case len(ps) == 0:
				s.Problem = "No system image found on the install media."
			}
			if len(ps) > 0 && s.PurposeID == "" {
				s.PurposeID = BestFit(m, ps).ID
			}
		})
		go e.reportLoop(ctx)
		go e.sys.Clusters(ctx, e.heard)
		if !e.startSite(ctx) {
			e.RefreshNetwork(ctx)
		}
		e.sys.Neighbors(ctx, e.setNeighbor)
	}()
}

func (e *Engine) setNeighbor(nic string, n Neighbor) {
	e.update(func(s *State) {
		for i := range s.Machine.NICs {
			if s.Machine.NICs[i].Name == nic {
				nb := n
				s.Machine.NICs[i].Neighbor = &nb
			}
		}
	})
}

// Subscribe returns a channel that receives after every change.
func (e *Engine) Subscribe() (<-chan struct{}, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := e.next
	e.next++
	ch := make(chan struct{}, 1)
	e.subs[id] = ch
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, id)
		e.mu.Unlock()
	}
}

func (e *Engine) Snapshot() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.st
	s.Install.Steps = slices.Clone(s.Install.Steps)
	s.Install.Log = slices.Clone(s.Install.Log)
	s.Plan.Pools = slices.Clone(s.Plan.Pools)
	s.Plan.Unused = slices.Clone(s.Plan.Unused)
	s.Machine.NICs = slices.Clone(s.Machine.NICs)
	s.Clusters = slices.Clone(s.Clusters)
	if s.Join != nil {
		j := *s.Join
		s.Join = &j
	}
	if s.Site != nil {
		run := *s.Site
		s.Site = &run
	}
	return s
}

func (e *Engine) update(f func(*State)) {
	e.mu.Lock()
	f(&e.st)
	for _, ch := range e.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	e.mu.Unlock()
}

// Go moves every frontend to step; installing is only reachable through Install.
func (e *Engine) Go(step Step) {
	e.update(func(s *State) {
		if s.Step >= StepInstalling || step >= StepInstalling {
			return
		}
		s.Step = step
	})
}

func (e *Engine) SetKeyboard(name string) error {
	if err := e.sys.SetKeymap(name); err != nil {
		return err
	}
	e.update(func(s *State) { s.Keyboard = name })
	return nil
}

func (e *Engine) RefreshNetwork(ctx context.Context) {
	e.runNet(ctx, func(ctx context.Context) NetReport { return e.sys.Network(ctx) })
}

// TestNetwork applies cfg to the live system and tests it in the background.
func (e *Engine) TestNetwork(ctx context.Context, cfg NetConfig) {
	e.runNet(ctx, func(ctx context.Context) NetReport { return e.sys.TestNetwork(ctx, cfg) })
}

func (e *Engine) runNet(ctx context.Context, f func(context.Context) NetReport) {
	e.mu.Lock()
	e.netGo++
	gen := e.netGo
	e.mu.Unlock()
	e.update(func(s *State) { s.NetTesting = true })
	go func() {
		r := f(ctx)
		e.update(func(s *State) {
			if gen != e.netGo {
				return
			}
			s.Net, s.NetTesting = r, false
		})
	}()
}

// ChooseNetwork keeps cfg for the installed system.
func (e *Engine) ChooseNetwork(cfg NetConfig) {
	e.update(func(s *State) {
		s.Net.Config = cfg
		s.NetChosen = true
	})
}

// ChoosePurpose makes this machine the first of a new cluster of purpose id.
func (e *Engine) ChoosePurpose(id string) {
	e.update(func(s *State) {
		s.leaveJoin()
		e.choosePurpose(s, id)
	})
}

func (e *Engine) choosePurpose(s *State, id string) {
	{
		if s.PurposeID != id || s.Plan.System == "" {
			s.PurposeID = id
			p, _ := s.Purpose()
			s.Plan = ProposePlan(s.Machine, p)
		}
		if s.Hostname == "" {
			p, _ := s.Purpose()
			s.Hostname = SuggestHostname(p)
		}
	}
}

func (e *Engine) SetPlan(p DiskPlan) {
	e.update(func(s *State) { s.Plan = p })
}

var hostnameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// CheckHostname explains what is wrong with name, or returns "".
func CheckHostname(name string) string {
	switch {
	case name == "":
		return "A name is needed."
	case len(name) > 63:
		return "63 characters at most."
	case !hostnameRE.MatchString(name):
		return "Use a to z, 0 to 9 and inner dashes."
	}
	return ""
}

func (e *Engine) SetHostname(name string) error {
	if msg := CheckHostname(name); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	e.update(func(s *State) { s.Hostname = name })
	return nil
}

func SuggestHostname(p Purpose) string {
	base := strings.ToLower(p.ID)
	if base == "" {
		base = "srv"
	}
	return base + "-01"
}

// Install starts the install once; confirm must equal the chosen hostname.
func (e *Engine) Install(ctx context.Context, confirm string) error {
	e.mu.Lock()
	s := e.st
	e.mu.Unlock()
	if s.Step >= StepInstalling {
		return nil
	}
	if confirm != s.Hostname || s.Hostname == "" {
		return fmt.Errorf("the name does not match")
	}
	p, ok := s.Purpose()
	if !ok {
		return fmt.Errorf("no purpose chosen")
	}
	req := InstallRequest{
		Machine:  s.Machine,
		Purpose:  p,
		Plan:     s.Plan,
		Net:      s.Net.Config,
		Hostname: s.Hostname,
		Keyboard: s.Keyboard,
		SSHKeys:  s.SSHKeys,
		Profile:  s.Profile,
	}
	req.Site, _ = siteFor(s)
	steps := make([]InstallStep, len(InstallSteps))
	for i, t := range InstallSteps {
		steps[i] = InstallStep{Title: t}
	}
	steps[InstData].Title = "Hand data disks to " + p.Name
	e.update(func(s *State) {
		s.Step = StepInstalling
		s.Install = InstallState{Steps: steps, Started: time.Now()}
	})
	go func() {
		err := e.sys.Install(ctx, req, e.onInstall)
		e.update(func(s *State) {
			if err != nil {
				s.Install.Err = err.Error()
				for i := range s.Install.Steps {
					if s.Install.Steps[i].Status == Running {
						s.Install.Steps[i].Status = Failed
					}
				}
				return
			}
			s.Install.Finished = true
			s.Install.Progress = 1
			s.Step = StepReady
		})
	}()
	return nil
}

func (e *Engine) onInstall(ev InstallEvent) {
	e.update(func(s *State) {
		in := &s.Install
		if ev.Log != "" {
			in.Log = append(in.Log, ev.Log)
			if len(in.Log) > 2000 {
				in.Log = in.Log[len(in.Log)-2000:]
			}
		}
		if ev.MOK != "" {
			in.MOKPassword = ev.MOK
		}
		if ev.Step < 0 || ev.Step >= len(in.Steps) {
			return
		}
		st := &in.Steps[ev.Step]
		if ev.Status != Pending {
			st.Status = ev.Status
		}
		if ev.Detail != "" {
			st.Detail = ev.Detail
		}
		done := 0.0
		for i, w := range stepWeights {
			switch {
			case i < ev.Step:
				done += w
			case i == ev.Step:
				p := ev.Progress
				if st.Status == Done || st.Status == Skipped {
					p = 1
				}
				done += w * p
			}
		}
		if done > in.Progress {
			in.Progress = done
		}
	})
}

func (e *Engine) Reboot() error { return e.sys.Reboot() }
