package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"time"

	"github.com/epheo/amorce/internal/site"
)

// SiteRun is an install answered by the site file. It checks every answer
// in wizard order, counts down, and installs; the first answer that does
// not work hands the wizard to a person on that screen.
type SiteRun struct {
	Site     string // the file's site name
	Name     string // this machine's name in it
	Checking bool
	// Countdown is the seconds left before the install starts.
	Countdown int
	// Stopped is set once a person has the wizard.
	Stopped bool
	// Problem says why a person is needed, shown on ProblemStep.
	Problem     string
	ProblemStep Step
}

// SiteCountdown is how long a person has to stop an answered install.
var SiteCountdown = 10

// startSite reads the media's site file; it returns true when the file
// names this machine and a run started.
func (e *Engine) startSite(ctx context.Context) bool {
	b, err := e.sys.Site(ctx)
	if b == nil && err == nil {
		return false
	}
	var f *site.File
	if err == nil {
		f, err = site.Parse(b)
	}
	if err != nil {
		e.update(func(s *State) {
			s.Site = &SiteRun{Stopped: true, Problem: err.Error(), ProblemStep: StepHello}
		})
		return false
	}
	e.update(func(s *State) { s.SiteFile, s.ReportTo, s.SiteNames = b, f.Report, f.Names() })
	a, ok := f.Find(e.Snapshot().identity())
	if !ok {
		e.update(func(s *State) {
			s.Site = &SiteRun{Site: f.Site, Stopped: true, ProblemStep: StepHello,
				Problem: "The site file does not name this machine."}
		})
		return false
	}
	e.update(func(s *State) { s.Site = &SiteRun{Site: f.Site, Name: a.Name, Checking: true} })
	go e.runSite(ctx, a)
	return true
}

// StopSite gives the wizard to a person; the answers so far stay filled in.
func (e *Engine) StopSite() {
	e.update(func(s *State) {
		if s.Site != nil && !s.Site.Stopped && s.Step < StepInstalling {
			s.Site.Stopped, s.Site.Checking, s.Site.Countdown = true, false, 0
		}
	})
}

func (e *Engine) siteStopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st.Site == nil || e.st.Site.Stopped
}

func (e *Engine) runSite(ctx context.Context, a site.Answers) {
	netDone := false
	defer func() {
		if !netDone {
			e.RefreshNetwork(ctx)
		}
	}()
	fail := func(step Step, msg string) {
		e.update(func(s *State) {
			if s.Site.Stopped {
				return
			}
			s.Site.Stopped, s.Site.Checking = true, false
			s.Site.Problem, s.Site.ProblemStep = msg, step
			s.Step = step
		})
	}

	e.update(func(s *State) { s.SSHKeys, s.Profile = a.SSHKeys, a.Profile })
	if a.Keyboard != "" {
		if err := e.SetKeyboard(a.Keyboard); err != nil {
			fail(StepHello, "Keyboard "+a.Keyboard+": "+err.Error())
			return
		}
	}

	cfg, msg := SiteNet(a, e.Snapshot().Machine)
	if msg != "" {
		fail(StepNetwork, msg)
		return
	}
	if e.siteStopped() {
		return
	}
	e.update(func(s *State) { s.NetTesting = true })
	var r NetReport
	if cfg.Mode == NetDHCP {
		r = e.sys.Network(ctx)
		r.Config.NTP = cfg.NTP
	} else {
		r = e.sys.TestNetwork(ctx, cfg)
	}
	netDone = true
	e.update(func(s *State) { s.Net, s.NetTesting = r, false })
	if why := netTrouble(r); why != "" {
		fail(StepNetwork, why)
		return
	}
	e.ChooseNetwork(r.Config)

	st := e.Snapshot()
	pid := a.Purpose
	if pid == "" && len(st.Purposes) == 1 {
		pid = st.Purposes[0].ID
	}
	var p Purpose
	var ids []string
	for _, q := range st.Purposes {
		ids = append(ids, q.ID)
		if q.ID == pid {
			p = q
		}
	}
	if p.ID == "" {
		fail(StepPurpose, fmt.Sprintf("Purpose %q is not on this media: %s.", pid, strings.Join(ids, ", ")))
		return
	}
	e.update(func(s *State) { e.choosePurpose(s, p.ID) })

	plan, msg := sitePlan(st.Machine, p, a.Disks)
	if msg != "" {
		fail(StepDisks, msg)
		return
	}
	e.SetPlan(plan)

	if a.Name == "" {
		fail(StepName, "The site file gives this machine no name.")
		return
	}
	if err := e.SetHostname(a.Name); err != nil {
		fail(StepName, "Name "+a.Name+": "+err.Error())
		return
	}
	// A worker serves the first machine's names, not its own.
	if a.Profile != site.Worker {
		n := Names{Domain: a.Domain, Identity: a.Identity}
		if n.Identity == "none" {
			n.Identity = ""
		}
		if a.Admin != nil {
			n.Admin = Admin{User: a.Admin.User, Display: a.Admin.Display, Email: a.Admin.Email}
		}
		if msg := e.SetNames(ctx, n); msg != "" {
			fail(StepName, msg)
			return
		}
	}

	erase := "blank"
	if a.Disks != nil && a.Disks.Erase != "" {
		erase = a.Disks.Erase
	}
	for _, n := range plan.Erased() {
		if d, ok := st.Disk(n); ok && d.Holds != "" && erase != "any" {
			fail(StepConfirm, "Stopped: the site file erases blank disks only.")
			return
		}
	}
	if p.NeedsSecureBootOff && st.Machine.SecureBoot {
		fail(StepConfirm, "Stopped: Secure Boot needs a person to decide.")
		return
	}

	e.update(func(s *State) { s.Site.Checking = false })
	for n := SiteCountdown; n > 0; n-- {
		e.update(func(s *State) {
			if !s.Site.Stopped {
				s.Site.Countdown = n
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if e.siteStopped() {
			return
		}
	}
	if e.siteStopped() {
		return
	}
	e.update(func(s *State) { s.Site.Countdown = 0 })
	if err := e.Install(ctx, a.Name); err != nil {
		fail(StepConfirm, err.Error())
	}
}

// SiteNet turns the answers into settings, or says what is missing.
func SiteNet(a site.Answers, m Machine) (NetConfig, string) {
	var n site.Network
	if a.Network != nil {
		n = *a.Network
	}
	c := NetConfig{Iface: n.Interface, VLAN: n.VLAN, NTP: n.NTP}
	if msg := CheckNTP(n.NTP); msg != "" {
		return c, msg
	}
	if c.Iface == "" {
		for _, nic := range m.NICs {
			if nic.Carrier {
				c.Iface = nic.Name
				break
			}
		}
	}
	if c.Iface == "" {
		return c, "No network interface has a link."
	}
	switch a.Address {
	case "":
		return c, "The site file gives this machine no address; dhcp is an answer too."
	case "dhcp":
		c.Mode = NetDHCP
		return c, ""
	}
	c.Mode = NetStatic
	addr := a.Address
	if !strings.Contains(addr, "/") {
		addr += "/24"
	}
	pfx, err := netip.ParsePrefix(addr)
	if err != nil {
		return c, "Address " + a.Address + " is not an address like 10.20.120.12/24."
	}
	c.Address = pfx
	if n.Gateway != "" {
		g, err := netip.ParseAddr(n.Gateway)
		if err != nil || !pfx.Masked().Contains(g) {
			return c, "Gateway " + n.Gateway + " is not in " + pfx.Masked().String() + "."
		}
		c.Gateway = g
	}
	for _, d := range n.DNS {
		ip, err := netip.ParseAddr(d)
		if err != nil {
			return c, "DNS " + d + " is not an address."
		}
		c.DNS = append(c.DNS, ip)
	}
	return c, ""
}

// netTrouble says why a tested network cannot be used unattended, or "".
func netTrouble(r NetReport) string {
	c := r.Config
	switch {
	case !r.Up && c.Mode == NetDHCP:
		return "The site file says dhcp; no DHCP server answered."
	case !r.Up:
		return "The site file's network did not come up on " + c.Iface + "."
	case r.Conflict != "":
		return "Another machine uses " + c.Address.Addr().String() + "."
	case r.Gateway == CheckFailed:
		return "The gateway " + c.Gateway.String() + " does not answer."
	}
	return ""
}

// sitePlan applies the disk answers to the purpose's plan.
func sitePlan(m Machine, p Purpose, d *site.Disks) (DiskPlan, string) {
	var spec site.Disks
	if d != nil {
		spec = *d
	}
	plan := ProposePlan(m, p)
	if spec.System != "" && spec.System != "auto" {
		sys, ok := findDisk(m, spec.System)
		if !ok {
			return plan, "The site file's system disk " + spec.System + " is not in this machine."
		}
		data := map[string]bool{}
		if p.UsesDataDisks() {
			for _, x := range m.Disks {
				data[x.Name] = x.Name != sys.Name
			}
		}
		plan = PlanFromRoles(m, sys.Name, data, p.DataDisks)
	}
	if spec.Data == "none" {
		for _, pool := range plan.Pools {
			plan.Unused = append(plan.Unused, pool.Disks...)
		}
		plan.Pools = nil
	}
	if plan.System == "" {
		return plan, "No disk can hold the system: 32 GB is the minimum."
	}
	return plan, ""
}

// findDisk matches a kernel name, serial, or by-id name.
func findDisk(m Machine, want string) (Disk, bool) {
	for _, d := range m.Disks {
		if d.Name == want || d.Serial == want || d.Path == want || filepath.Base(d.Path) == want {
			return d, true
		}
	}
	return Disk{}, false
}

// siteFor is the site file the installed system keeps: the one it was
// installed from, naming this machine, or one written from this machine's
// answers, shared settings in defaults, naming this machine as the first
// one that the others report to.
func siteFor(s State) ([]byte, error) {
	id := s.identity()
	f := &site.File{}
	if s.SiteFile != nil {
		var err error
		if f, err = site.Parse(s.SiteFile); err != nil {
			return nil, err
		}
		if _, ok := f.Find(id); ok {
			return s.SiteFile, nil
		}
	} else {
		f.Defaults = siteDefaults(s)
		if c := s.Net.Config; c.Address.IsValid() {
			f.Report = &site.Report{Address: c.Address.Addr().String(), Token: rand.Text()}
		}
	}
	f.Machines = append(f.Machines, siteMachine(s))
	return site.Marshal(f)
}

// siteDefaults are this machine's answers that the next machines share.
func siteDefaults(s State) site.Answers {
	p, _ := s.Purpose()
	c := s.Net.Config
	data := "none"
	if len(s.Plan.Pools) > 0 {
		data = "auto"
	}
	d := site.Answers{Purpose: p.ID, Keyboard: s.Keyboard, Disks: &site.Disks{System: "auto", Data: data},
		SSHKeys: s.SSHKeys}
	n := site.Network{Interface: c.Iface, VLAN: c.VLAN}
	if c.Gateway.IsValid() {
		n.Gateway = c.Gateway.String()
	}
	for _, a := range c.DNS {
		n.DNS = append(n.DNS, a.String())
	}
	n.NTP = c.NTP
	d.Network = &n
	d.Domain, d.Identity = s.Names.Domain, s.Names.Identity
	if a := s.Names.Admin; a != (Admin{}) {
		d.Admin = &site.Admin{User: a.User, Display: a.Display, Email: a.Email}
	}
	return d
}

// siteMachine is this machine's own entry.
func siteMachine(s State) site.Machine {
	p, _ := s.Purpose()
	c := s.Net.Config
	mt := site.Machine{Answers: site.Answers{Name: s.Hostname, Address: "dhcp", Profile: s.Profile}}
	if c.Mode == NetStatic {
		mt.Address = c.Address.String()
	}
	if sys := s.Plan.System; sys != ProposePlan(s.Machine, p).System {
		if d, ok := s.Disk(sys); ok {
			mt.Disks = &site.Disks{System: filepath.Base(d.Path)}
		}
	}
	switch {
	case s.Machine.Serial != "":
		mt.Match.Serial = s.Machine.Serial
	default:
		for _, nic := range s.Machine.NICs {
			if nic.Name == c.Iface || mt.Match.MAC == "" {
				mt.Match.MAC = nic.MAC
			}
		}
	}
	return mt
}
