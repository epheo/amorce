package engine

import (
	"net/netip"
	"testing"

	"github.com/epheo/microshift-installer/internal/site"
)

func TestSiteForRoundTrips(t *testing.T) {
	m := Machine{
		Serial: "S1",
		Disks:  []Disk{{Name: "sda", Path: "/dev/disk/by-id/wwn-1", Size: 960e9, SSD: true}},
		NICs:   []NIC{{Name: "eno1", MAC: "3c:ec:ef:00:00:01", Carrier: true}},
	}
	p := Purpose{ID: "kubernetes", DataDisks: "none"}
	st := State{
		Machine: m, Purposes: []Purpose{p}, PurposeID: p.ID, Keyboard: "fr", Hostname: "k8s-01",
		Plan: ProposePlan(m, p),
		Net: NetReport{Config: NetConfig{Mode: NetStatic, Iface: "eno1", VLAN: 110,
			Address: netip.MustParsePrefix("10.20.120.11/24"), Gateway: netip.MustParseAddr("10.20.120.1")}},
	}
	b, err := siteFor(st)
	if err != nil {
		t.Fatal(err)
	}
	f, err := site.Parse(b)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	a, ok := f.Find(site.Identity{Serial: "S1"})
	if !ok {
		t.Fatalf("no match:\n%s", b)
	}
	c, msg := siteNet(a, m)
	want := st.Net.Config
	if msg != "" || c.Iface != want.Iface || c.VLAN != want.VLAN || c.Address != want.Address || c.Gateway != want.Gateway {
		t.Fatalf("%q %+v", msg, c)
	}
	if a.Name != "k8s-01" || a.Purpose != "kubernetes" || a.Keyboard != "fr" || a.Disks.System != "auto" || a.Disks.Data != "none" {
		t.Fatalf("%+v\n%s", a, b)
	}
}
