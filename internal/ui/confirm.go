package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/epheo/microshift-installer/internal/engine"
)

func (m *Model) keyName(s string, k tea.KeyPressMsg) {
	switch s {
	case "esc":
		m.eng.Go(engine.StepDisks)
	case "enter":
		if err := m.eng.SetHostname(m.name.value); err != nil {
			return
		}
		m.eng.Go(engine.StepConfirm)
	default:
		m.name.handle(strings.ToLower(s), k)
		m.name.value = strings.ToLower(m.name.value)
	}
}

func (m *Model) viewName() screen {
	t := m.t
	left := []string{
		t.B(t.Title).Render("Name"),
		t.S(t.Muted).Render("How this machine shows up on the network."),
		"",
		t.B(t.Title).Render("What should this machine be called?"),
		t.S(t.Muted).Render("Lowercase letters, digits and dashes."),
		"",
		m.inputView(m.name, true),
	}
	if msg := engine.CheckHostname(m.name.value); msg != "" && m.name.value != "" {
		left = append(left, "", t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(msg))
	}
	return screen{
		left: left, focusFrom: 3, focusTo: 6, panel: m.setup(),
		help: [][2]string{{"type", "the name"}, {"enter", "continue"}, {"esc", "back"}},
		pill: "Name", pillColor: t.Border, stepNo: 5, right: m.statusRight(),
	}
}

func (m *Model) keyConfirm(s string, k tea.KeyPressMsg) {
	switch s {
	case "esc":
		m.eng.Go(engine.StepName)
	case "tab", "shift+tab":
		m.confirmTab = !m.confirmTab
	case "enter":
		if m.confirmTab {
			m.eng.Go(engine.StepName)
			return
		}
		if err := m.eng.Install(m.ctx, m.confirm.value); err != nil {
			m.note = ""
		}
	default:
		if !m.confirmTab {
			m.confirm.handle(s, k)
		}
	}
}

func (m *Model) viewConfirm() screen {
	t := m.t
	st := m.st
	l := m.layout()
	p, _ := st.Purpose()
	plan := st.Plan
	erased := plan.Erased()

	kv := func(k, v string) string { return t.S(t.Muted).Width(10).Render(k) + t.S(t.Fg).Render(v) }
	lines := []string{
		t.B(t.Bad).Render(fmt.Sprintf("Erase %d %s and install?", len(erased), plural(len(erased), "disk", "disks"))),
		t.S(t.Muted).Render("This cannot be undone."),
		"",
		kv("Name", st.Hostname),
		kv("Purpose", p.Name),
		kv("Network", m.netSummary()),
	}
	if d, ok := st.Disk(plan.System); ok {
		lines = append(lines, kv("System", d.Name+", "+diskKind(d)+" "+humanBytes(d.Size)))
	}
	if len(plan.Pools) > 0 {
		n := 0
		for _, pool := range plan.Pools {
			n += len(pool.Disks)
		}
		lines = append(lines, kv("Data", fmt.Sprintf("%d disks, %d mirrored %s, %s", n, len(plan.Pools),
			plural(len(plan.Pools), "pair", "pairs"), humanBytes(m.dataBytes()))))
	}
	if len(plan.Unused) > 0 {
		lines = append(lines, kv("Kept", strings.Join(plan.Unused, ", ")+", not touched"))
	}
	var warns []string
	for _, n := range erased {
		if d, ok := st.Disk(n); ok && d.Holds != "" {
			warns = append(warns, t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(fmt.Sprintf("%s holds %s; it goes too.", d.Name, d.Holds)))
		}
	}
	if w := m.secureBootWarning(p); w != "" {
		warns = append(warns, w)
	}
	if len(warns) > 0 {
		lines = append(lines, "")
		lines = append(lines, warns...)
	}
	match := m.confirm.value == st.Hostname
	lines = append(lines, "",
		t.B(t.Title).Render("Type "+st.Hostname+" to confirm"),
		m.inputView(m.confirm, !m.confirmTab),
		"")

	back := t.Button("  Go back  ", m.confirmTab, true, t.Border)
	erase := t.Button("  Erase and install  ", !m.confirmTab, match, t.Bad)
	note := "Erase unlocks when the name matches."
	if match {
		note = "Press enter to start."
	}
	const dw = 62
	buttons := back + "  " + erase
	center := func(s string) string {
		pad := (dw - 6 - lipgloss.Width(s)) / 2
		return strings.Repeat(" ", max(0, pad)) + s
	}
	lines = append(lines, center(buttons), center(t.S(t.Muted).Render(note)))

	dh := len(lines) + 4
	dx := (l.W - dw) / 2
	dy := max(3, (l.helpY-dh)/2+1)
	var layers []*lipgloss.Layer
	pattern := strings.TrimSuffix(strings.Repeat(t.S(t.Bar).Render(strings.Repeat(t.Slash, l.W))+"\n", l.helpY-4), "\n")
	if t.Look == LookNormal {
		pattern = strings.TrimSuffix(strings.Repeat(t.S(t.Faint).Render(strings.Repeat(t.Slash, l.W))+"\n", l.helpY-4), "\n")
	}
	layers = append(layers, lipgloss.NewLayer(pattern).X(0).Y(3).Z(0))
	blank := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", dw+2)+"\n", dh+2), "\n")
	layers = append(layers, lipgloss.NewLayer(blank).X(dx-1).Y(dy-1).Z(1))
	layers = append(layers, lipgloss.NewLayer(m.box(dw, dh, t.Bad)).X(dx).Y(dy).Z(2))
	layers = append(layers, lipgloss.NewLayer(strings.Join(lines, "\n")).X(dx+3).Y(dy+2).Z(3))
	return screen{
		fullscreen: true, layers: layers, focusFrom: -1,
		help: [][2]string{{"type", "the name"}, {"tab", "buttons"}, {"enter", "confirm"}, {"esc", "go back"}},
		pill: "Confirm", pillColor: t.Bad, stepNo: 6, right: m.statusRight(),
	}
}

func (m *Model) netSummary() string {
	c := m.st.Net.Config
	if !c.Address.IsValid() {
		return "DHCP"
	}
	s := c.Address.Addr().String()
	if c.Mode == engine.NetDHCP {
		s += " from DHCP"
	}
	if c.VLAN != 0 {
		s += fmt.Sprintf(" on VLAN %d", c.VLAN)
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
