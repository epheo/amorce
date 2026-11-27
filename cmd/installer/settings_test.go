package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/epheo/amorce/internal/live"
	"github.com/epheo/amorce/internal/site"
)

const kept = `report: {address: 10.20.120.11, token: t}
defaults:
  network: {interface: eno1, gateway: 10.20.120.1}
machines:
  - match: {serial: S1}
    name: srv-01
    address: 10.20.120.11/24
`

func settingsIn(t *testing.T, file string) (*settings, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{live.KeyfileDir, filepath.Dir(live.NodeConfigPath), filepath.Dir(live.SitePath)} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, live.KeyfileDir, "eno1.nmconnection"), []byte("old"), 0o600)
	os.WriteFile(filepath.Join(root, live.SitePath), []byte(file), 0o600)
	f, err := site.Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	h := &home{id: site.Identity{Serial: "S1"}, site: f}
	h.answers, _ = f.Find(h.id)
	return &settings{h: h, root: root, sitePath: "/" + live.SitePath}, root
}

// A new address lands where the installer put the old one, and the site
// file follows: this machine's entry, and report when it names this machine.
func TestSetNetwork(t *testing.T) {
	s, root := settingsIn(t, kept)
	a, why := s.Network()
	if why != "" || a.Network.Interface != "eno1" {
		t.Fatalf("%q %+v", why, a.Network)
	}
	a.Address = "10.20.120.12/24"
	if err := s.SetNetwork(a); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(root, rel))
		return string(b)
	}
	if k := read(filepath.Join(live.KeyfileDir, "eno1.nmconnection")); !strings.Contains(k, "address1=10.20.120.12/24,10.20.120.1") {
		t.Errorf("keyfile:\n%s", k)
	}
	if n := read(live.NodeConfigPath); !strings.Contains(n, "nodeIP: 10.20.120.12") {
		t.Errorf("node config:\n%s", n)
	}
	f, err := site.Parse([]byte(read(live.SitePath)))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Find(site.Identity{Serial: "S1"}); got.Address != "10.20.120.12/24" || f.Report.Address != "10.20.120.12" {
		t.Errorf("site file:\n%s", read(live.SitePath))
	}
}

func TestWorkerKeepsItsAddress(t *testing.T) {
	s, _ := settingsIn(t, strings.NewReplacer("address: 10.20.120.11,", "address: 10.20.120.10,",
		"    name: srv-01\n", "    name: srv-01\n    profile: worker\n").Replace(kept))
	if _, why := s.Network(); why == "" {
		t.Fatal("a worker may change its address")
	}
}
