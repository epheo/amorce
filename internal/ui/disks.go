package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/epheo/amorce/internal/engine"
)

func diskKind(d engine.Disk) string {
	if d.Controller != "" {
		return "RAID"
	}
	if d.SSD {
		return "SSD"
	}
	return "HDD"
}

// diskRow is one tree leaf: name, kind and size, then what needs attention.
func (m *Model) diskRow(prefix string, d engine.Disk) string {
	t := m.t
	row := t.S(t.Faint).Render(prefix) + t.S(t.Fg).Width(9).Render(d.Name)
	size := diskKind(d) + " " + humanBytes(d.Size)
	note := ""
	switch {
	case d.Failing:
		note = t.S(t.Bad).Render(d.Health)
	case d.Holds != "":
		note = t.S(t.Warn).Render("has " + d.Holds)
	case d.Health != "":
		note = t.S(t.Warn).Render(d.Health)
	}
	if note != "" {
		return row + t.S(t.Muted).Width(12).Render(size) + note
	}
	return row + t.S(t.Muted).Render(size)
}

// group is a tree root with its size flush right.
func (m *Model) group(name, desc, size string, width int) string {
	t := m.t
	l := t.B(t.Fg).Render(name) + t.S(t.Muted).Render("  "+desc)
	r := t.B(t.Fg).Render(size)
	gap := width - lipgloss.Width(l) - lipgloss.Width(r)
	return l + strings.Repeat(" ", max(1, gap)) + r
}

func (m *Model) planTree(width int) []string {
	t := m.t
	st := m.st
	plan := st.Plan
	p, _ := st.Purpose()
	var out []string
	if d, ok := st.Disk(plan.System); ok {
		out = append(out, m.group("System", "one disk", humanBytes(d.Size), width), m.diskRow("└── ", d), "")
	}
	if len(plan.Pools) > 0 {
		out = append(out, m.group("Data", m.dataLayout(plan)+", built by "+p.Name, humanBytes(m.dataBytes()), width))
		for i, pool := range plan.Pools {
			last := i == len(plan.Pools)-1
			branch, stem := "├── ", "│   "
			if last {
				branch, stem = "└── ", "    "
			}
			kind := "mirror"
			if pool.Raid != "raid1" {
				kind = "thin pool, no copy"
			}
			out = append(out, t.S(t.Faint).Render(branch)+t.S(t.Fg).Width(9).Render(pool.Name)+t.S(t.Muted).Render(kind))
			for j, n := range pool.Disks {
				d, _ := st.Disk(n)
				leaf := "├── "
				if j == len(pool.Disks)-1 {
					leaf = "└── "
				}
				out = append(out, m.diskRow(stem+leaf, d))
			}
		}
		out = append(out, "")
	}
	if len(plan.Unused) > 0 {
		desc := "left untouched"
		if p.DataDisks == engine.DataPairs {
			desc = "free for a pool later, in the web UI"
		}
		out = append(out, t.B(t.Fg).Render("Not used")+t.S(t.Muted).Render("  "+desc))
		for i, n := range plan.Unused {
			d, _ := st.Disk(n)
			leaf := "├── "
			if i == len(plan.Unused)-1 {
				leaf = "└── "
			}
			out = append(out, m.diskRow(leaf, d))
		}
		out = append(out, "")
	}
	return out
}

func (m *Model) keyDisks(s string) {
	if m.roles != nil {
		m.keyRoles(s)
		return
	}
	switch s {
	case "esc":
		m.eng.Go(engine.StepPurpose)
	case "up":
		m.disksCur = 0
	case "down":
		m.disksCur = 1
	case "enter":
		if m.disksCur == 1 {
			m.roles = newRoleEditor(m.st)
			return
		}
		if m.st.Plan.System != "" {
			m.eng.Go(engine.StepName)
		}
	}
}

func (m *Model) viewDisks() screen {
	t := m.t
	l := m.layout()
	if m.roles != nil {
		return m.viewRoles()
	}
	st := m.st
	left := []string{
		t.B(t.Title).Render("Disks"),
		t.S(t.Muted).Render(fmt.Sprintf("Our suggestion for the %d disks found.", len(st.Machine.Disks))),
		"",
	}
	if st.Plan.System == "" {
		left = append(left, t.S(t.Bad).Render(t.Fail+" ")+t.S(t.Fg).Render("No disk can hold the system: 32 GB is the minimum."))
	}
	left = append(left, m.planTree(min(l.leftW, 56))...)
	left = append(left, m.raidWarning()...)
	focus := len(left)
	left = append(left,
		t.B(t.Title).Render("Use this layout?"),
		t.S(t.Muted).Render("You can pick the disks yourself instead."),
		"",
		m.choice("Yes, use this layout", m.disksCur == 0),
		m.choice("Let me choose the disks", m.disksCur == 1),
	)
	panel := m.setup()
	if d, ok := st.Disk(st.Plan.System); ok {
		kv := func(k, v string) string { return t.S(t.Muted).Width(12).Render(k) + t.S(t.Fg).Render(v) }
		panel = append(panel, "", t.B(t.Good).Render("Space"), kv("System", humanBytes(d.Size)))
		if len(st.Plan.Pools) > 0 {
			survives := "no disk loss"
			if st.Plan.Pools[0].Raid == "raid1" {
				survives = "1 disk per pair"
			}
			panel = append(panel, kv("Your data", humanBytes(m.dataBytes())), kv("Survives", survives))
		}
	}
	return screen{
		left: left, focusFrom: focus, focusTo: len(left) - 1, panel: panel,
		help: [][2]string{{"up/down", "choose"}, {"enter", "continue"}, {"esc", "back"}},
		pill: "Disks", pillColor: t.Border, stepNo: 4, right: m.statusRight(),
	}
}

// raidWarning says when a purpose that mirrors drives itself only sees a
// RAID controller's volumes, and how to show it the drives.
func (m *Model) raidWarning() []string {
	t := m.t
	p, _ := m.st.Purpose()
	if p.DataDisks != engine.DataPairs {
		return nil
	}
	var names []string
	ctl := ""
	for _, d := range m.st.Machine.Disks {
		if d.Controller != "" {
			names = append(names, d.Name)
			ctl = d.Controller
		}
	}
	if len(names) == 0 {
		return nil
	}
	what := names[0] + " is a volume"
	if len(names) > 1 {
		what = strings.Join(names, ", ") + " are volumes"
	}
	return []string{
		t.S(t.Warn).Render(t.Alert+" ") + t.S(t.Fg).Render(what+" of the "+ctl+" controller."),
		t.S(t.Muted).Render("  " + p.Name + " mirrors drives itself; it cannot see them."),
		t.S(t.Muted).Render("  Set the controller to HBA mode, then start again."),
		"",
	}
}

// roleEditor lets the user say, disk by disk, what each one is for.
type roleEditor struct {
	names  []string
	roles  map[string]int // 0 system, 1 data, 2 not used
	cur    int
	layout string // the purpose's data-disks
}

var roleNames = [...]string{"System", "Data", "Not used"}

func newRoleEditor(st engine.State) *roleEditor {
	p, _ := st.Purpose()
	r := &roleEditor{roles: map[string]int{}, layout: p.DataDisks}
	for _, d := range st.Machine.Disks {
		r.names = append(r.names, d.Name)
		r.roles[d.Name] = 2
	}
	r.roles[st.Plan.System] = 0
	for _, pool := range st.Plan.Pools {
		for _, n := range pool.Disks {
			r.roles[n] = 1
		}
	}
	return r
}

func (r *roleEditor) plan(m engine.Machine) (engine.DiskPlan, string) {
	system := ""
	data := map[string]bool{}
	for _, n := range r.names {
		switch r.roles[n] {
		case 0:
			if system != "" {
				return engine.DiskPlan{}, "Choose one system disk."
			}
			system = n
		case 1:
			data[n] = true
		}
	}
	if system == "" {
		return engine.DiskPlan{}, "Choose a system disk."
	}
	for _, d := range m.Disks {
		if d.Name == system && d.Size < 32<<30 {
			return engine.DiskPlan{}, "The system disk needs 32 GB or more."
		}
	}
	return engine.PlanFromRoles(m, system, data, r.layout), ""
}

func (m *Model) keyRoles(s string) {
	r := m.roles
	switch s {
	case "esc":
		m.roles = nil
	case "up":
		r.cur = max(0, r.cur-1)
	case "down":
		r.cur = min(len(r.names)-1, r.cur+1)
	case "space", "right":
		n := r.names[r.cur]
		next := (r.roles[n] + 1) % 3
		if next == 1 && r.layout != engine.DataPairs && r.layout != engine.DataThin {
			next = 2
		}
		r.roles[n] = next
	case "enter":
		plan, msg := r.plan(m.st.Machine)
		if msg != "" {
			m.note = msg
			return
		}
		m.note = ""
		m.eng.SetPlan(plan)
		m.roles = nil
	}
}

func (m *Model) viewRoles() screen {
	t := m.t
	r := m.roles
	left := []string{
		t.B(t.Title).Render("Choose the disks"),
		t.S(t.Muted).Render("Space changes what a disk is for."),
		"",
	}
	top := len(left)
	for i, n := range r.names {
		d, _ := m.st.Disk(n)
		role := roleNames[r.roles[n]]
		rc := t.Muted
		switch r.roles[n] {
		case 0:
			rc = t.Accent
		case 1:
			rc = t.Good
		}
		cursor := "  "
		if i == r.cur {
			cursor = t.S(t.Accent).Render("> ")
		}
		left = append(left, cursor+t.S(rc).Width(10).Render(role)+m.diskRow("", d))
	}
	plan, msg := r.plan(m.st.Machine)
	left = append(left, "")
	if msg != "" {
		left = append(left, t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(msg))
	} else if len(plan.Pools) > 0 {
		left = append(left, t.S(t.Muted).Render("Data: "+m.dataLayout(plan)+"."))
	}
	return screen{
		left: left, focusFrom: top, focusTo: top + len(r.names) - 1, panel: m.setup(),
		help: [][2]string{{"up/down", "disk"}, {"space", "change"}, {"enter", "done"}, {"esc", "cancel"}},
		pill: "Disks", pillColor: t.Border, stepNo: 4, right: m.statusRight(),
	}
}

// dataLayout says what the plan builds from the data disks.
func (m *Model) dataLayout(plan engine.DiskPlan) string {
	if len(plan.Pools) == 1 && plan.Pools[0].Raid != "raid1" {
		n := len(plan.Pools[0].Disks)
		return fmt.Sprintf("one pool of %d %s", n, plural(n, "disk", "disks"))
	}
	if len(plan.Pools) == 1 {
		return "1 mirrored pair"
	}
	return fmt.Sprintf("%d mirrored pairs", len(plan.Pools))
}
