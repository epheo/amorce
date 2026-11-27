package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/epheo/amorce/internal/engine"
)

type helloOption struct {
	label string
	run   func(m *Model) tea.Cmd
}

func (m *Model) helloOptions() []helloOption {
	opts := []helloOption{{"Install on this machine", func(m *Model) tea.Cmd {
		if m.st.Problem != "" || !m.st.Discovered {
			return nil
		}
		m.eng.Go(engine.StepNetwork)
		return nil
	}}}
	if m.opt.Shell != nil {
		opts = append(opts, helloOption{"Open a rescue shell", func(m *Model) tea.Cmd {
			return tea.Exec(m.opt.Shell(), func(err error) tea.Msg { return shellDoneMsg{err} })
		}})
	}
	return opts
}

func (m *Model) keyHello(s string) tea.Cmd {
	opts := m.helloOptions()
	switch s {
	case "up":
		m.helloCur = max(0, m.helloCur-1)
	case "down":
		m.helloCur = min(len(opts)-1, m.helloCur+1)
	case "k":
		m.kb = newKeyboardPicker(m.st.Keymaps, m.st.Keyboard)
	case "enter":
		return opts[m.helloCur].run(m)
	}
	return nil
}

func (m *Model) viewHello() screen {
	t := m.t
	st := m.st
	left := []string{
		t.B(t.Title).Render("Let's set up this machine"),
		t.S(t.Muted).Render("Nothing is erased until you confirm, on the last step."),
		"",
		t.B(t.Title).Render("What would you like to do?"),
		t.S(t.Muted).Render("Most people start a new installation."),
		"",
	}
	if r := st.Site; r != nil && r.Name != "" {
		where := "The site file"
		if r.Site != "" {
			where = "Site " + r.Site
		}
		left[1] = t.S(t.Muted).Render(where + " answers every screen.")
		left[3] = t.B(t.Title).Render("This is " + r.Name)
		switch {
		case r.Checking:
			left[4] = m.spinner() + t.S(t.Muted).Render(" Checking its answers")
		case r.Countdown > 0:
			left[4] = t.S(t.Fg).Render(fmt.Sprintf("Installing in %d s. ", r.Countdown)) + t.S(t.Muted).Render("Any key stops it.")
		default:
			left[4] = t.S(t.Muted).Render("Stopped; its answers are filled in.")
		}
	}
	for i, o := range m.helloOptions() {
		left = append(left, m.choice(o.label, i == m.helloCur))
	}
	focusTo := len(left) - 1
	left = append(left, "", "",
		t.B(t.Title).Render("Keyboard"),
		t.S(t.Fg).Render(st.Keyboard)+t.S(t.Muted).Render("   k to change, type to search"),
		"", "")
	switch {
	case st.Problem != "":
		left = append(left, t.S(t.Bad).Render(t.Fail+" ")+t.S(t.Fg).Render(st.Problem))
	case !st.Discovered:
		left = append(left, m.spinner()+t.S(t.Muted).Render(" Looking at this machine"))
	default:
		var names []string
		for _, p := range st.Purposes {
			names = append(names, p.Name)
		}
		left = append(left, t.S(t.Muted).Render("On this media: "+strings.Join(names, ", ")+"."))
	}

	panel := []string{t.B(t.Good).Render("This machine"), ""}
	if st.Discovered {
		mc := st.Machine
		linked := 0
		for _, n := range mc.NICs {
			if n.Carrier {
				linked++
			}
		}
		fw, sb := "BIOS", ""
		if mc.UEFI {
			fw = "UEFI"
			sb = "Secure Boot off"
			if mc.SecureBoot {
				sb = "Secure Boot on"
			}
		}
		kv := func(k, v string) string { return t.S(t.Muted).Width(10).Render(k) + t.S(t.Fg).Render(v) }
		if mc.Serial != "" {
			panel = append(panel, kv("Serial", mc.Serial))
		}
		panel = append(panel,
			kv("CPU", fmt.Sprintf("%d cores", mc.CPUs)),
			kv("Memory", humanBytes(roundGiB(mc.MemoryBytes))),
			kv("Disks", fmt.Sprintf("%d found", len(mc.Disks))),
			kv("Network", fmt.Sprintf("%d of %d linked", linked, len(mc.NICs))),
			kv("Firmware", fw),
		)
		if sb != "" {
			panel = append(panel, kv("", sb))
		}
	} else {
		panel = append(panel, m.spinner()+t.S(t.Muted).Render(" looking"))
	}
	panel = append(panel, "", t.B(t.Good).Render("Your setup"), t.S(t.Muted).Render("Fills in as you answer."))

	return screen{
		left: left, focusFrom: 3, focusTo: focusTo, panel: panel,
		help: [][2]string{{"up/down", "choose"}, {"enter", "continue"}, {"k", "keyboard"}},
		pill: "Welcome", pillColor: t.Border, stepNo: 1, right: m.opt.Where,
	}
}

// roundGiB reports memory the way it is sold: 64 GB, not 67.
func roundGiB(n uint64) uint64 {
	g := (n + (1<<30)/2) >> 30
	return g * 1e9
}

type keyboardPicker struct {
	all    []string
	filter input
	cur    int
}

func newKeyboardPicker(all []string, current string) *keyboardPicker {
	k := &keyboardPicker{all: all}
	for i, v := range all {
		if v == current {
			k.cur = i
		}
	}
	return k
}

// matches lists layouts containing the filter: the exact name first, then
// names starting with it, then the rest.
func (k *keyboardPicker) matches() []string {
	f := strings.ToLower(k.filter.value)
	if f == "" {
		return k.all
	}
	var exact, prefix, other []string
	for _, v := range k.all {
		lv := strings.ToLower(v)
		switch {
		case lv == f:
			exact = append(exact, v)
		case strings.HasPrefix(lv, f):
			prefix = append(prefix, v)
		case strings.Contains(lv, f):
			other = append(other, v)
		}
	}
	return append(append(exact, prefix...), other...)
}

func (m *Model) keyKeyboard(s string, key tea.KeyPressMsg) {
	k := m.kb
	list := k.matches()
	switch s {
	case "esc":
		m.kb = nil
	case "up":
		k.cur = max(0, k.cur-1)
	case "down":
		k.cur = min(len(list)-1, k.cur+1)
	case "enter":
		if k.cur < len(list) {
			if err := m.eng.SetKeyboard(list[k.cur]); err != nil {
				m.note = err.Error()
			}
		}
		m.kb = nil
	default:
		if k.filter.handle(s, key) {
			k.cur = 0
		}
	}
}

func (m *Model) viewKeyboard() screen {
	t := m.t
	k := m.kb
	list := k.matches()
	left := []string{
		t.B(t.Title).Render("Keyboard"),
		t.S(t.Muted).Render("Type to search, then press enter."),
		"",
		m.inputView(k.filter, true),
		"",
	}
	const rows = 14
	start := max(0, min(k.cur-rows/2, len(list)-rows))
	for i := start; i < len(list) && i < start+rows; i++ {
		left = append(left, m.choice(list[i], i == k.cur))
	}
	if len(list) == 0 {
		left = append(left, t.S(t.Muted).Render("  No layout matches."))
	}
	return screen{
		left: left, focusFrom: 3, focusTo: len(left) - 1, panel: m.setup(),
		help: [][2]string{{"type", "search"}, {"up/down", "choose"}, {"enter", "use"}, {"esc", "back"}},
		pill: "Keyboard", pillColor: t.Border, stepNo: 1, right: m.opt.Where,
	}
}
