package engine

import (
	"fmt"
	"slices"
	"strings"
)

// Data disk layouts, as a purpose's data-disks label names them; the
// purpose builds them at its first start, from the plan.
const (
	DataPairs = "pairs" // a mirror per pair of same-size disks of a kind
	DataThin  = "thin"  // one LVM thin pool over every data disk
)

// UsesDataDisks says whether the purpose takes disks beyond the system disk.
func (p Purpose) UsesDataDisks() bool { return p.DataDisks == DataPairs || p.DataDisks == DataThin }

// minSystemBytes is the smallest disk bootc images fit on with room to update.
const minSystemBytes = 32 << 30

// BestFit picks the purpose the hardware suits: Storage when spare disks pair up.
func BestFit(m Machine, ps []Purpose) Purpose {
	best := ps[0]
	for _, p := range ps {
		if p.DataDisks == DataPairs && len(ProposePlan(m, p).Pools) > 0 {
			return p
		}
	}
	return best
}

// BestFitReason explains BestFit's choice in one line, or returns "".
func BestFitReason(m Machine, p Purpose) string {
	if p.DataDisks != DataPairs {
		return ""
	}
	plan := ProposePlan(m, p)
	n := 0
	for _, pool := range plan.Pools {
		n += len(pool.Disks)
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("Best fit because %d data disks were found.", n)
}

// ProposePlan suggests a system disk and what the purpose does with the rest.
func ProposePlan(m Machine, p Purpose) DiskPlan {
	var plan DiskPlan
	disks := slices.Clone(m.Disks)
	slices.SortStableFunc(disks, func(a, b Disk) int {
		switch {
		case a.Failing != b.Failing:
			return boolCmp(a.Failing, b.Failing)
		case a.SSD != b.SSD:
			return boolCmp(!a.SSD, !b.SSD)
		case a.Size != b.Size:
			return cmpU64(a.Size, b.Size)
		case a.Stable() != b.Stable():
			return boolCmp(!a.Stable(), !b.Stable())
		}
		// Kernel names can swap between starts; the same machine must get
		// the same proposal at every start.
		return strings.Compare(a.Path, b.Path)
	})
	for _, d := range disks {
		if d.Size >= minSystemBytes && !d.Failing {
			plan.System = d.Name
			break
		}
	}
	var rest []Disk
	for _, d := range m.Disks {
		if d.Name != plan.System {
			rest = append(rest, d)
		}
	}
	plan.Pools, plan.Unused = layOut(p.DataDisks, rest)
	return plan
}

func layOut(layout string, disks []Disk) ([]Pool, []string) {
	switch layout {
	case DataPairs:
		return pairUp(disks)
	case DataThin:
		return onePool(disks)
	}
	var unused []string
	for _, d := range disks {
		unused = append(unused, d.Name)
	}
	return nil, unused
}

// dataReady says whether d may hold data: healthy, and found again at the
// purpose's first start by the name the plan gives it.
func dataReady(d Disk) bool { return !d.Failing && d.Stable() }

// onePool puts every data-ready disk in one pool; the others stay unused.
func onePool(disks []Disk) ([]Pool, []string) {
	pool := Pool{Name: "data", Raid: "none"}
	var unused []string
	for _, d := range disks {
		if !dataReady(d) {
			unused = append(unused, d.Name)
			continue
		}
		pool.Disks = append(pool.Disks, d.Name)
	}
	if len(pool.Disks) == 0 {
		return nil, unused
	}
	return []Pool{pool}, unused
}

// pairUp mirrors disks of the same kind and size; the odd ones stay unused.
func pairUp(disks []Disk) ([]Pool, []string) {
	type key struct {
		ssd  bool
		size uint64
	}
	groups := map[key][]Disk{}
	var order []key
	var unused []string
	for _, d := range disks {
		if !dataReady(d) {
			unused = append(unused, d.Name)
			continue
		}
		k := key{d.SSD, d.Size}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], d)
	}
	var pools []Pool
	count := map[string]int{}
	for _, k := range order {
		g := groups[k]
		tier := "hdd"
		if k.ssd {
			tier = "ssd"
		}
		for i := 0; i+1 < len(g); i += 2 {
			count[tier]++
			pools = append(pools, Pool{
				Name:  fmt.Sprintf("%s-%d", tier, count[tier]),
				Raid:  "raid1",
				Disks: []string{g[i].Name, g[i+1].Name},
			})
		}
		if len(g)%2 == 1 {
			unused = append(unused, g[len(g)-1].Name)
		}
	}
	return pools, unused
}

// Erased lists every disk the plan wipes: the system disk and pool members.
func (p DiskPlan) Erased() []string {
	var out []string
	if p.System != "" {
		out = append(out, p.System)
	}
	for _, pool := range p.Pools {
		out = append(out, pool.Disks...)
	}
	return out
}

// PlanFromRoles rebuilds a plan after the user assigned roles by hand.
func PlanFromRoles(m Machine, system string, data map[string]bool, layout string) DiskPlan {
	plan := DiskPlan{System: system}
	var rest []Disk
	for _, d := range m.Disks {
		switch {
		case d.Name == system:
		case data[d.Name]:
			rest = append(rest, d)
		default:
			plan.Unused = append(plan.Unused, d.Name)
		}
	}
	pools, unused := layOut(layout, rest)
	plan.Pools = pools
	plan.Unused = append(plan.Unused, unused...)
	return plan
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func cmpU64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
