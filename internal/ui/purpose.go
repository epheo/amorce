package ui

import (
	"github.com/epheo/microshift-installer/internal/engine"
)

func (m *Model) purposeIndex(id string) int {
	for i, p := range m.st.Purposes {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func (m *Model) keyPurpose(s string) {
	ps := m.st.Purposes
	switch s {
	case "esc":
		m.eng.Go(engine.StepNetwork)
	case "up":
		m.purposeCur = max(0, m.purposeCur-1)
	case "down":
		m.purposeCur = min(len(ps)-1, m.purposeCur+1)
	case "enter":
		if m.purposeCur < len(ps) {
			m.eng.ChoosePurpose(ps[m.purposeCur].ID)
			m.eng.Go(engine.StepDisks)
		}
	}
}

func (m *Model) viewPurpose() screen {
	t := m.t
	st := m.st
	best := engine.Purpose{}
	if len(st.Purposes) > 0 {
		best = engine.BestFit(st.Machine, st.Purposes)
	}
	left := []string{
		t.B(t.Title).Render("Purpose"),
		t.S(t.Muted).Render("Everything is on this media; nothing is downloaded."),
		"",
		t.B(t.Title).Render("What will this machine do?"),
		t.S(t.Muted).Render("You can add more machines later."),
		"",
	}
	for i, p := range st.Purposes {
		badge := ""
		if p.ID == best.ID && len(st.Purposes) > 1 {
			badge = "  " + t.Pill(t.Good, " BEST FIT ")
		}
		if i == m.purposeCur {
			bar := t.S(t.Mauve).Render(t.List + " ")
			left = append(left, bar+t.B(t.Accent).Render(p.Name)+badge, bar+t.S(t.Mauve).Render(p.Summary))
		} else {
			left = append(left, "  "+t.S(t.Fg).Render(p.Name)+badge, "  "+t.S(t.Muted).Render(p.Summary))
		}
		left = append(left, "")
	}
	if why := engine.BestFitReason(st.Machine, best); why != "" && len(st.Purposes) > 1 {
		left = append(left, "", t.S(t.Muted).Render(why))
	}
	if m.purposeCur < len(st.Purposes) {
		if w := m.secureBootWarning(st.Purposes[m.purposeCur]); w != nil {
			left = append(append(left, ""), w...)
		}
	}
	panel := m.setup()
	if m.purposeCur < len(st.Purposes) {
		p := st.Purposes[m.purposeCur]
		panel = append(panel, "", t.B(t.Good).Render(p.Name+" includes"))
		for _, inc := range p.Includes {
			panel = append(panel, t.S(t.Fg).Render(inc))
		}
	}
	return screen{
		left: left, focusFrom: -1, panel: panel,
		help: [][2]string{{"up/down", "choose"}, {"enter", "continue"}, {"esc", "back"}},
		pill: "Purpose", pillColor: t.Border, stepNo: 3, right: m.statusRight(),
	}
}

// secureBootWarning says, before anything is erased, that p will not fully
// run on this machine as its firmware is set, and what to do about it.
func (m *Model) secureBootWarning(p engine.Purpose) []string {
	if !m.needsSecureBootOff(p) {
		return nil
	}
	t := m.t
	return []string{
		t.S(t.Warn).Render(t.Alert+" ") + t.S(t.Fg).Render("Secure Boot is on; "+p.Name+" needs it off for now."),
		t.S(t.Muted).Render("  Install, then turn it off in the firmware setup."),
	}
}

func (m *Model) needsSecureBootOff(p engine.Purpose) bool {
	return p.NeedsSecureBootOff && m.st.Machine.SecureBoot
}
