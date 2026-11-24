package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/live"
	"github.com/epheo/microshift-installer/internal/site"
)

// settings is the console's F2 on this machine. root is "/" but in tests.
type settings struct {
	h        *home
	root     string
	sitePath string
	mu       sync.Mutex
}

func (s *settings) path(rel string) string { return filepath.Join(s.root, rel) }

// Network is this machine's network, from the site file the install kept.
// Its address cannot change while other machines reach it there.
func (s *settings) Network() (site.Answers, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.network()
}

func (s *settings) network() (site.Answers, string) {
	h := s.h
	if h.site == nil {
		return site.Answers{}, "No site file: this machine was not installed by the installer."
	}
	h.mu.Lock()
	a, fm := h.answers, h.first
	h.mu.Unlock()
	n := site.Network{}
	if a.Network != nil {
		n = *a.Network
	}
	if n.Interface == "" {
		n.Interface = live.LinkedNIC()
	}
	a.Network = &n
	switch {
	case a.Address == "":
		return a, "The site file does not name this machine."
	case a.Profile == site.Worker:
		return a, "A worker's cluster knows it by this address; it cannot change here."
	case fm != nil:
		for _, r := range fm.store.Board() {
			if r.State != "" && r.Serial != h.id.Serial {
				return a, "Other machines of the site reach this one here; it cannot change while they do."
			}
		}
	}
	return a, ""
}

// SetNetwork rewrites what the installer wrote for the network, for the
// next start: its NetworkManager profiles, MicroShift's node IP, and this
// machine's entry in the site file. configure-ovs builds br-ex from the
// profiles at every start.
func (s *settings) SetNetwork(a site.Answers) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, why := s.network()
	if why != "" {
		return fmt.Errorf("%s", why)
	}
	cfg, msg := engine.SiteNet(a, engine.Machine{})
	if msg != "" {
		return fmt.Errorf("%s", msg)
	}
	old, _ := engine.SiteNet(cur, engine.Machine{})
	var written []string
	for name := range live.Keyfiles(old) {
		os.Remove(s.path(filepath.Join(live.KeyfileDir, name+".nmconnection")))
	}
	for name, body := range live.Keyfiles(cfg) {
		p := s.path(filepath.Join(live.KeyfileDir, name+".nmconnection"))
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return err
		}
		written = append(written, p)
	}
	name, _ := os.Hostname()
	p := s.path(live.NodeConfigPath)
	if err := os.WriteFile(p, []byte(live.NodeConfig(name, cfg)), 0o644); err != nil {
		return err
	}
	written = append(written, p)

	// A copy: the console reads the file it started with until the restart.
	p = s.path(s.sitePath)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	f, err := site.Parse(b)
	if err != nil {
		return err
	}
	for i, m := range f.Machines {
		if m.Matches(s.h.id) {
			f.Machines[i].Address, f.Machines[i].Network = a.Address, a.Network
		}
	}
	if f.Report != nil && f.Report.Host() == addrHost(cur.Address) && a.Address != "dhcp" {
		f.Report.Address = addrHost(a.Address)
	}
	if b, err = site.Marshal(f); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	written = append(written, p)
	s.h.mu.Lock()
	s.h.answers, _ = f.Find(s.h.id)
	s.h.mu.Unlock()
	if s.root == "/" {
		exec.Command("restorecon", written...).Run()
	}
	return nil
}

func addrHost(a string) string {
	h, _, _ := strings.Cut(a, "/")
	return h
}

func (s *settings) HasRootPassword() bool {
	b, _ := os.ReadFile(s.path("etc/shadow"))
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Split(l, ":"); len(f) > 1 && f[0] == "root" {
			return strings.HasPrefix(f[1], "$")
		}
	}
	return false
}

func (s *settings) SetRootPassword(pw string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "chpasswd")
	c.Stdin = strings.NewReader("root:" + pw + "\n")
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("chpasswd: %v: %s", err, lastLine(string(out)))
	}
	return nil
}

func (s *settings) Restart() error  { return exec.Command("systemctl", "reboot").Run() }
func (s *settings) PowerOff() error { return exec.Command("systemctl", "poweroff").Run() }
