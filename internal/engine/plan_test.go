package engine

import (
	"slices"
	"testing"
)

const tb = 1000 * 1000 * 1000 * 1000

func machine() Machine {
	m := Machine{Disks: []Disk{
		{Name: "nvme0n1", Size: 960e9, SSD: true},
		{Name: "nvme1n1", Size: 960e9, SSD: true},
		{Name: "sda", Size: 8 * tb},
		{Name: "sdb", Size: 8 * tb},
		{Name: "sdc", Size: 8 * tb},
		{Name: "sdd", Size: 8 * tb},
	}}
	for i := range m.Disks {
		m.Disks[i].Path = "/dev/disk/by-id/wwn-" + m.Disks[i].Name
	}
	return m
}

func TestStoragePairsDataDisks(t *testing.T) {
	p := ProposePlan(machine(), Purpose{DataDisks: "pairs"})
	if p.System != "nvme0n1" {
		t.Fatalf("system = %q, want the smallest SSD", p.System)
	}
	if len(p.Pools) != 2 || p.Pools[0].Raid != "raid1" || !slices.Equal(p.Pools[0].Disks, []string{"sda", "sdb"}) {
		t.Fatalf("pools = %+v", p.Pools)
	}
	// The lone SSD has no twin: it stays free rather than unprotected.
	if !slices.Equal(p.Unused, []string{"nvme1n1"}) {
		t.Fatalf("unused = %v", p.Unused)
	}
	if got := len(p.Erased()); got != 5 {
		t.Fatalf("erased %d disks, want 5", got)
	}
}

func TestOtherPurposesLeaveDataDisksAlone(t *testing.T) {
	p := ProposePlan(machine(), Purpose{DataDisks: "none"})
	if len(p.Pools) != 0 || len(p.Unused) != 5 || len(p.Erased()) != 1 {
		t.Fatalf("plan = %+v", p)
	}
}

func TestFailingDiskNeverHoldsTheSystem(t *testing.T) {
	m := machine()
	m.Disks[0].Failing = true
	p := ProposePlan(m, Purpose{DataDisks: "pairs"})
	if p.System != "nvme1n1" || !slices.Contains(p.Unused, "nvme0n1") {
		t.Fatalf("plan = %+v", p)
	}
}

func TestTooSmallForSystem(t *testing.T) {
	m := Machine{Disks: []Disk{{Name: "sda", Size: 16 << 30, SSD: true}}}
	if p := ProposePlan(m, Purpose{}); p.System != "" {
		t.Fatalf("system = %q on a 16 GiB disk", p.System)
	}
}

func TestBestFitPrefersStorageWithPairs(t *testing.T) {
	ps := []Purpose{{ID: "k8s", DataDisks: "none"}, {ID: "storage", DataDisks: "pairs"}}
	if got := BestFit(machine(), ps).ID; got != "storage" {
		t.Fatalf("best fit = %s", got)
	}
	one := Machine{Disks: machine().Disks[:1]}
	if got := BestFit(one, ps).ID; got != "k8s" {
		t.Fatalf("best fit without data disks = %s", got)
	}
}

func TestHostnames(t *testing.T) {
	for name, ok := range map[string]bool{"srv-01": true, "a": true, "-a": false, "a-": false, "Srv": false, "a_b": false, "": false} {
		if got := CheckHostname(name) == ""; got != ok {
			t.Errorf("CheckHostname(%q) ok = %v, want %v", name, got, ok)
		}
	}
}

// thin takes every healthy data disk into one pool, for the distro's
// default r-drbd pool; a failing disk stays out.
func TestThinPoolsEveryDataDisk(t *testing.T) {
	m := machine()
	m.Disks[5].Failing = true
	p := ProposePlan(m, Purpose{DataDisks: DataThin})
	if len(p.Pools) != 1 || p.Pools[0].Raid != "none" || !slices.Equal(p.Pools[0].Disks, []string{"nvme1n1", "sda", "sdb", "sdc"}) {
		t.Fatalf("pools = %+v", p.Pools)
	}
	if !slices.Equal(p.Unused, []string{"sdd"}) {
		t.Fatalf("unused = %v", p.Unused)
	}
	r := PlanFromRoles(m, "nvme0n1", map[string]bool{"sda": true, "sdb": true}, DataThin)
	if len(r.Pools) != 1 || len(r.Pools[0].Disks) != 2 {
		t.Fatalf("by hand: %+v", r)
	}
}

// A disk known only by its kernel name may be another disk at the next
// start: it can hold the system, never data.
func TestUnstableDiskNeverHoldsData(t *testing.T) {
	m := machine()
	m.Disks[2].Path = "/dev/sda"
	p := ProposePlan(m, Purpose{DataDisks: DataThin})
	if slices.Contains(p.Pools[0].Disks, "sda") || !slices.Contains(p.Unused, "sda") {
		t.Fatalf("plan = %+v", p)
	}
	r := PlanFromRoles(m, "nvme0n1", map[string]bool{"sda": true, "sdb": true, "sdc": true}, DataPairs)
	if len(r.Pools) != 1 || slices.Contains(r.Pools[0].Disks, "sda") {
		t.Fatalf("by hand: %+v", r)
	}
}
