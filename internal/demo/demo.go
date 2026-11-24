// Package demo is a fake machine for working on the screens away from hardware.
// It is the mockups' srv-01: two SSDs, four HDDs, a switch on VLAN 110.
package demo

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/report"
)

type System struct {
	// Speed scales the fake install; 1 takes about a minute.
	Speed float64
	// RAID hides the hard disks behind one hardware RAID volume.
	RAID bool
	// SiteFile is the site file the media carries, if any.
	SiteFile []byte
}

func (s *System) Site(ctx context.Context) ([]byte, error) { return s.SiteFile, nil }

const tb = 1000 * 1000 * 1000 * 1000

func (s *System) Discover(ctx context.Context) (engine.Machine, error) {
	time.Sleep(300 * time.Millisecond)
	sw := func(name string) *engine.Neighbor {
		return &engine.Neighbor{Switch: name, Port: "port 12", VLAN: 110, Seen: time.Now()}
	}
	m := engine.Machine{
		Serial: "CZ2041XY7J", CPUs: 16, MemoryBytes: 64 << 30, UEFI: true, SecureBoot: true,
		Disks: []engine.Disk{
			{Name: "nvme0n1", Path: "/dev/disk/by-id/nvme-SSD0001", Size: 960e9, SSD: true},
			{Name: "nvme1n1", Path: "/dev/disk/by-id/nvme-SSD0002", Size: 960e9, SSD: true},
			{Name: "sda", Path: "/dev/disk/by-id/wwn-HDD0001", Size: 8 * tb},
			{Name: "sdb", Path: "/dev/disk/by-id/wwn-HDD0002", Size: 8 * tb},
			{Name: "sdc", Path: "/dev/disk/by-id/wwn-HDD0003", Size: 8 * tb, Holds: "2.1 TB of files"},
			{Name: "sdd", Path: "/dev/disk/by-id/wwn-HDD0004", Size: 8 * tb, Health: "early wear, still fine"},
		},
		NICs: []engine.NIC{
			{Name: "eno1", MAC: "3c:ec:ef:00:00:01", Carrier: true, SpeedMbps: 25000, Neighbor: sw("tor-a")},
			{Name: "eno2", MAC: "3c:ec:ef:00:00:02", Carrier: true, SpeedMbps: 25000, Neighbor: sw("tor-b")},
			{Name: "eno3", MAC: "3c:ec:ef:00:00:03"},
		},
	}
	if s.RAID {
		m.Disks = append(m.Disks[:2:2], engine.Disk{Name: "sda", Path: "/dev/disk/by-id/wwn-PERC0001",
			Model: "PERC H740P Mini", Size: 24 * tb, Controller: "PERC H740P Mini"})
	}
	return m, nil
}

func (s *System) Purposes(ctx context.Context) ([]engine.Purpose, error) {
	return []engine.Purpose{
		{ID: "kubernetes", Name: "Kubernetes", Summary: "Run your own apps on MicroShift",
			Includes: []string{"MicroShift", "Replicated volumes", "Gateway API"}, DataDisks: "none"},
		{ID: "storage", Name: "Storage", Summary: "File shares, iSCSI disks, replication",
			Includes:  []string{"Web UI on port 30080", "SMB and NFS shares", "iSCSI disks", "Snapshots", "Replication, 3+ machines"},
			DataDisks: "pairs", NeedsSecureBootOff: true},
		{ID: "virtualization", Name: "Virtualization", Summary: "Virtual machines, identity, git forge",
			Includes: []string{"Virtual machines", "Single sign-on", "Git forge"}, DataDisks: "none"},
	}, nil
}

func (s *System) Keymaps() []string {
	return []string{"be", "ch", "de", "dk", "es", "fi", "fr", "fr-bepo", "gb", "it", "jp106", "no", "pl", "pt", "se", "us"}
}

func (s *System) SetKeymap(name string) error { return nil }

var site = netip.MustParsePrefix("10.20.120.11/24")

// Network is the first look: no DHCP on this rack.
func (s *System) Network(ctx context.Context) engine.NetReport {
	time.Sleep(800 * time.Millisecond)
	return engine.NetReport{Config: engine.NetConfig{Mode: engine.NetDHCP, Iface: "eno1"}, At: time.Now()}
}

// TestNetwork fails on VLAN 120, where the site file says the rack is.
func (s *System) TestNetwork(ctx context.Context, cfg engine.NetConfig) engine.NetReport {
	time.Sleep(1200 * time.Millisecond)
	r := engine.NetReport{Config: cfg, Up: cfg.Address.IsValid(), At: time.Now()}
	if !r.Up {
		return r
	}
	if cfg.VLAN == 120 {
		r.Gateway = engine.CheckFailed
		r.GatewayVLANs = []int{0}
		return r
	}
	r.DNS = engine.CheckOK
	if cfg.Gateway.IsValid() {
		r.Gateway = engine.CheckOK
	}
	return r
}

// DemoNetwork is the settings the demo's form starts from.
func DemoNetwork() engine.NetConfig {
	return engine.NetConfig{
		Mode: engine.NetStatic, Iface: "eno1", VLAN: 120, Address: site,
		Gateway: netip.MustParseAddr("10.20.120.1"), DNS: []netip.Addr{netip.MustParseAddr("10.20.0.53")},
	}
}

// Neighbors has nothing to add: Discover already knows the switches.
func (s *System) Neighbors(ctx context.Context, found func(string, engine.Neighbor)) {}

func (s *System) Install(ctx context.Context, req engine.InstallRequest, ev func(engine.InstallEvent)) error {
	speed := s.Speed
	if speed <= 0 {
		speed = 1
	}
	wait := func(d time.Duration) { time.Sleep(time.Duration(float64(d) / speed)) }
	for i := range engine.InstallSteps {
		ev(engine.InstallEvent{Step: i, Status: engine.Running, Log: "start: " + engine.InstallSteps[i]})
		switch i {
		case engine.InstCopy:
			for p := 0.0; p <= 1; p += 0.02 {
				ev(engine.InstallEvent{Step: i, Status: engine.Running, Progress: p,
					Log: fmt.Sprintf("copying layer %d of 50", int(p*50))})
				wait(800 * time.Millisecond)
			}
		case engine.InstSecureBoot:
			wait(time.Second)
			if !req.Purpose.NeedsSecureBootOff {
				ev(engine.InstallEvent{Step: i, Status: engine.Done, MOK: "4831 7720"})
				continue
			}
		default:
			wait(1500 * time.Millisecond)
		}
		ev(engine.InstallEvent{Step: i, Status: engine.Done, Progress: 1})
	}
	return nil
}

func (s *System) Reboot() error { return fmt.Errorf("demo: no restart") }

// Clusters hears one first machine, the mockups' srv-01.
func (s *System) Clusters(ctx context.Context, found func(engine.Cluster)) {
	time.Sleep(time.Second)
	found(engine.Cluster{Site: "lab", Name: "srv-01", Address: "10.20.120.11", Purpose: "storage", Open: true})
}

// Admit takes any six words.
func (s *System) Admit(ctx context.Context, address string, words []string, me report.Report) (engine.Admission, error) {
	time.Sleep(time.Second)
	if len(words) != 6 {
		return engine.Admission{}, fmt.Errorf("Those are not the words on its screen.")
	}
	f := "site: lab\nreport: {address: " + address + ", token: demo}\ndefaults: {purpose: storage}\nmachines:\n  - match: {serial: OTHER}\n    name: srv-01\n"
	return engine.Admission{Site: []byte(f), Purpose: "storage", Names: []string{"srv-01", "srv-02"}}, nil
}
