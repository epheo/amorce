package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/epheo/microshift-installer/internal/site"
)

// JoinState is an attended join: the words from the first machine's
// screen, typed here, buy its site file.
type JoinState struct {
	Name    string // the first machine
	Address string
	Busy    bool
	Err     string
	Done    bool
}

func (e *Engine) heard(c Cluster) {
	c.Seen = time.Now()
	e.update(func(s *State) {
		i := slices.IndexFunc(s.Clusters, func(o Cluster) bool { return o.Address == c.Address })
		if i < 0 {
			s.Clusters = append(s.Clusters, c)
			return
		}
		s.Clusters[i] = c
	})
}

// Join makes this machine a worker of the first machine at c.Address. The
// words are those on its screen; none are needed when the site file names
// that machine already, since the file holds the site's token.
func (e *Engine) Join(ctx context.Context, c Cluster, words []string) {
	st := e.Snapshot()
	if st.Join != nil && st.Join.Busy {
		return
	}
	if st.Knows(c.Address) {
		e.update(func(s *State) {
			s.Profile = site.Worker
			if id := siteAnswers(*s).Purpose; slices.ContainsFunc(s.Purposes, func(p Purpose) bool { return p.ID == id }) {
				e.choosePurpose(s, id)
			}
			s.Step = StepDisks
		})
		return
	}
	name := c.Name
	if name == "" {
		name = c.Address
	}
	e.update(func(s *State) { s.Join = &JoinState{Name: name, Address: c.Address, Busy: true} })
	go func() {
		me := reportOf(st)
		me.Detail = "Joining with words from " + name + "'s screen."
		a, err := e.sys.Admit(ctx, c.Address, words, me)
		var f *site.File
		if err == nil {
			f, err = site.Parse(a.Site)
		}
		if err == nil && f.Report == nil {
			err = fmt.Errorf("%s's site file names no first machine", name)
		}
		e.update(func(s *State) {
			if s.Join == nil || s.Join.Address != c.Address {
				return
			}
			s.Join.Busy = false
			if err != nil {
				s.Join.Err = err.Error()
				return
			}
			if !slices.ContainsFunc(s.Purposes, func(p Purpose) bool { return p.ID == a.Purpose }) {
				s.Join.Err = fmt.Sprintf("%s runs %q; this media has no such image.", name, a.Purpose)
				return
			}
			s.Join.Done = true
			s.SiteFile, s.ReportTo, s.SiteNames = a.Site, f.Report, f.Names()
			s.SSHKeys = f.Defaults.SSHKeys
			s.Profile = site.Worker
			s.Hostname = ""
			e.choosePurpose(s, a.Purpose)
			s.Hostname = NextName(name, append(a.Names, s.SiteNames...))
			s.Step = StepDisks
		})
	}()
}

// siteAnswers are this machine's answers in the site file it holds, or the
// file's defaults when it does not name this machine.
func siteAnswers(s State) site.Answers {
	f, err := site.Parse(s.SiteFile)
	if err != nil {
		return site.Answers{}
	}
	if a, ok := f.Find(s.identity()); ok {
		return a
	}
	return f.Defaults
}

func (s State) identity() site.Identity {
	id := site.Identity{Serial: s.Machine.Serial}
	for _, n := range s.Machine.NICs {
		id.MACs = append(id.MACs, n.MAC)
	}
	return id
}

// Knows says whether this machine holds the site token of the first
// machine at address already, so joining it needs no words.
func (s State) Knows(address string) bool {
	return s.ReportTo != nil && s.ReportTo.Host() == address && (s.Join == nil || s.Join.Done)
}

// CancelJoin forgets a join attempt that failed, so another can start.
func (e *Engine) CancelJoin() {
	e.update(func(s *State) {
		if s.Join != nil && !s.Join.Busy && !s.Join.Done {
			s.Join = nil
		}
	})
}

// leaveJoin makes this machine a controller again, forgetting the site
// file an attended join brought.
func (s *State) leaveJoin() {
	s.Profile = ""
	if s.Join == nil {
		return
	}
	if s.Join.Done {
		s.SiteFile, s.ReportTo, s.SiteNames, s.SSHKeys = nil, nil, nil, nil
		s.Hostname = ""
	}
	s.Join = nil
}

var trailingNumber = regexp.MustCompile(`^(.*?)(\d+)$`)

// NextName is the first name after first's number that none of taken has:
// store-01 gives store-02.
func NextName(first string, taken []string) string {
	base, num, width := first+"-", 1, 2
	if m := trailingNumber.FindStringSubmatch(first); m != nil {
		base, width = m[1], len(m[2])
		num, _ = strconv.Atoi(m[2])
	}
	for range 1000 {
		num++
		name := fmt.Sprintf("%s%0*d", base, width, num)
		if name != first && !slices.Contains(taken, name) && CheckHostname(name) == "" {
			return name
		}
	}
	return ""
}
