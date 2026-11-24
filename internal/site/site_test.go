package site

import (
	"strings"
	"testing"
)

const lab = `
site: lab
defaults:
  purpose: storage
  keyboard: fr
  network: {vlan: 110, gateway: 10.20.120.1, dns: [10.20.0.53]}
  disks: {data: auto}
  ssh_authorized_keys: ["ssh-ed25519 AAAAC3Nza admin@lab"]
machines:
  - match: {serial: CZ2041XY7K}
    name: srv-02
    address: 10.20.120.12/24
  - match: {mac: "3C:EC:EF:00:00:03"}
    name: srv-03
    address: 10.20.120.13/24
    network: {vlan: 120}
    disks: {erase: any}
`

func TestFind(t *testing.T) {
	f, err := Parse([]byte(lab))
	if err != nil {
		t.Fatal(err)
	}
	a, ok := f.Find(Identity{Serial: "cz2041xy7k "})
	if !ok || a.Name != "srv-02" || a.Purpose != "storage" || a.Keyboard != "fr" || a.Network.VLAN != 110 || len(a.SSHKeys) != 1 {
		t.Fatalf("serial: %v %+v", ok, a)
	}
	a, ok = f.Find(Identity{MACs: []string{"3c:ec:ef:00:00:01", "3c:ec:ef:00:00:03"}})
	if !ok || a.Name != "srv-03" || a.Network.VLAN != 120 || a.Network.Gateway != "10.20.120.1" ||
		a.Disks.Data != "auto" || a.Disks.Erase != "any" {
		t.Fatalf("mac: %v %+v %+v", ok, a, a.Disks)
	}
	if _, ok := f.Find(Identity{Serial: "OTHER"}); ok {
		t.Fatal("matched a machine the file does not name")
	}
}

func TestParseRejects(t *testing.T) {
	for _, bad := range []string{
		"machines:\n  - name: srv-02\n",
		"machines:\n  - match: {serial: X}\n    nmae: srv-02\n",
		"machines:\n  - match: {mac: nope}\n",
		"defaults: {disks: {data: raid5}}\nmachines: []\n",
		"defaults: {ssh_authorized_keys: [AAAAC3Nza]}\nmachines: []\n",
		"defaults: {profile: worker}\nmachines: []\n",
		"defaults: {profile: master}\nmachines: []\n",
		"report: {address: 10.0.0.1, token: t}\ndefaults: {profile: worker}\nmachines:\n  - match: {serial: X}\n    address: 10.0.0.1/24\n",
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	f := &File{Machines: []Machine{{Match: Match{Serial: "S1"}, Answers: Answers{Name: "a", Address: "dhcp"}}}}
	b, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# Written by the installer") {
		t.Fatalf("no header: %s", b)
	}
	g, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := g.Find(Identity{Serial: "S1"}); !ok || a.Name != "a" {
		t.Fatalf("%v %+v", ok, a)
	}
}

func TestProfile(t *testing.T) {
	f, err := Parse([]byte(`
report: {address: 10.0.0.1, token: t}
defaults: {profile: worker}
machines:
  - match: {serial: A}
    address: 10.0.0.1/24
    profile: controller
  - match: {serial: B}
    address: 10.0.0.2/24
`))
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := f.Find(Identity{Serial: "A"}); a.Profile != "controller" {
		t.Fatalf("first machine: %q", a.Profile)
	}
	if a, _ := f.Find(Identity{Serial: "B"}); a.Profile != Worker {
		t.Fatalf("second machine: %q", a.Profile)
	}
}
