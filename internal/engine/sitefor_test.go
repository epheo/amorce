package engine

import (
	"net/netip"
	"testing"

	"github.com/epheo/amorce/internal/site"
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
	c, msg := SiteNet(a, m)
	want := st.Net.Config
	if msg != "" || c.Iface != want.Iface || c.VLAN != want.VLAN || c.Address != want.Address || c.Gateway != want.Gateway {
		t.Fatalf("%q %+v", msg, c)
	}
	if a.Name != "k8s-01" || a.Purpose != "kubernetes" || a.Keyboard != "fr" || a.Disks.System != "auto" || a.Disks.Data != "none" {
		t.Fatalf("%+v\n%s", a, b)
	}
}

// A machine the site file does not name adds itself to the file it keeps.
func TestSiteForAddsThisMachine(t *testing.T) {
	m := Machine{Serial: "S2", NICs: []NIC{{Name: "eno1", MAC: "3c:ec:ef:00:00:01", Carrier: true}}}
	st := State{
		Machine: m, Hostname: "k8s-02", Profile: site.Worker,
		SiteFile: []byte("report: {address: 10.0.0.1, token: t}\nmachines:\n  - match: {serial: S1}\n    name: k8s-01\n"),
		Net:      NetReport{Config: NetConfig{Mode: NetStatic, Iface: "eno1", Address: netip.MustParsePrefix("10.0.0.2/24")}},
	}
	b, err := siteFor(st)
	if err != nil {
		t.Fatal(err)
	}
	f, err := site.Parse(b)
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	a, ok := f.Find(site.Identity{Serial: "S2"})
	if !ok || a.Name != "k8s-02" || a.Profile != site.Worker || a.Address != "10.0.0.2/24" || f.Report.Token != "t" {
		t.Fatalf("%v %+v\n%s", ok, a, b)
	}
	if _, ok := f.Find(site.Identity{Serial: "S1"}); !ok {
		t.Fatalf("lost the first machine:\n%s", b)
	}
}

// An install that queued a key says on the board what MokManager asks for.
func TestReportCarriesMOKPassword(t *testing.T) {
	r := reportOf(State{Step: StepReady, Hostname: "srv-02", Install: InstallState{MOKPassword: "4831 7720"}})
	if r.State != "installed" || r.Detail != "at its next start, MokManager asks for 4831 7720" {
		t.Fatalf("%+v", r)
	}
}
