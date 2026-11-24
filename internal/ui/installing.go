package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/epheo/microshift-installer/internal/engine"
)

func (m *Model) keyInstalling(s string) {
	if s == "l" {
		m.showLog = !m.showLog
	}
	if s == "esc" {
		m.showLog = false
	}
}

func (m *Model) viewInstalling() screen {
	t := m.t
	l := m.layout()
	in := m.st.Install
	sc := screen{
		fullscreen: true, focusFrom: -1,
		help: [][2]string{{"l", "show log"}},
		pill: "Installing", pillColor: t.Border, right: m.statusRight(),
	}
	if m.showLog {
		sc.help = [][2]string{{"l", "hide log"}}
		sc.layers = m.logLayers(l, in.Log)
		return sc
	}
	cur := 0
	for i, s := range in.Steps {
		if s.Status == engine.Running || s.Status == engine.Failed {
			cur = i
		}
	}
	center := func(y int, s string) *lipgloss.Layer {
		return lipgloss.NewLayer(s).X(max(0, (l.W-lipgloss.Width(s))/2)).Y(y)
	}
	var head string
	if in.Err != "" {
		head = t.S(t.Bad).Render(t.Fail+" ") + t.B(t.Fg).Render("The install stopped")
	} else if len(in.Steps) > 0 {
		head = m.spinner() + " " + t.S(t.Accent).Render(doing(in.Steps[cur].Title)) +
			t.S(t.Muted).Render(fmt.Sprintf("   %d/%d", cur+1, len(in.Steps)+1))
	}
	barW := min(60, l.W-20)
	pct := t.B(t.Fg).Render(fmt.Sprintf("  %d%%", int(in.Progress*100)))
	sc.layers = append(sc.layers,
		center(5, head),
		lipgloss.NewLayer(t.Progress(barW, in.Progress)+pct).X((l.W-barW)/2).Y(7),
		center(8, t.S(t.Muted).Render(eta(in))),
	)
	var steps []string
	for i, s := range in.Steps {
		var g string
		st := t.S(t.Fg)
		switch s.Status {
		case engine.Done:
			g = t.S(t.Good).Render(t.Ok)
		case engine.Skipped:
			g, st = t.S(t.Faint).Render(t.Pend), t.S(t.Muted)
		case engine.Running:
			g, st = m.spinner(), t.B(t.Fg)
		case engine.Failed:
			g, st = t.S(t.Bad).Render(t.Fail), t.S(t.Bad)
		default:
			g, st = t.S(t.Faint).Render(t.Pend), t.S(t.Muted)
		}
		line := g + " " + st.Render(s.Title)
		if s.Detail != "" && (s.Status == engine.Skipped || s.Status == engine.Failed) {
			line += t.S(t.Muted).Render(", " + s.Detail)
		}
		steps = append(steps, line)
		_ = i
	}
	steps = append(steps, t.S(t.Faint).Render(t.Pend)+" "+t.S(t.Muted).Render("Restart"))
	sx := (l.W - 30) / 2
	sc.layers = append(sc.layers, lipgloss.NewLayer(strings.Join(steps, "\n")).X(sx).Y(11))

	by := 11 + len(steps) + 1
	if by+6 < l.helpY {
		var body []string
		bc := t.BorderDim
		if in.Err != "" {
			bc = t.Bad
			body = []string{t.B(t.Bad).Render("What happened"), t.S(t.Fg).Render(ansi.Truncate(in.Err, 53, "…")),
				t.S(t.Muted).Render("Press l for the log, F12 for a shell.")}
		} else {
			body = []string{t.B(t.Good).Render("After the restart"),
				t.S(t.Muted).Render("The machine starts from its own disk and shows"),
				t.S(t.Muted).Render("its web address on this screen.")}
		}
		bw := 59
		sc.layers = append(sc.layers,
			lipgloss.NewLayer(m.box(bw, len(body)+2, bc)).X((l.W-bw)/2).Y(by),
			lipgloss.NewLayer(strings.Join(body, "\n")).X((l.W-bw)/2+2).Y(by+1))
	}
	return sc
}

func (m *Model) logLayers(l layout, log []string) []*lipgloss.Layer {
	t := m.t
	h := l.helpY - 5
	w := l.W - 6
	start := max(0, len(log)-(h-2))
	var lines []string
	for _, s := range log[start:] {
		lines = append(lines, t.S(t.Muted).Render(ansi.Truncate(s, w-4, "…")))
	}
	return []*lipgloss.Layer{
		lipgloss.NewLayer(m.box(w, h, t.BorderDim)).X(3).Y(3),
		lipgloss.NewLayer(t.B(t.Title).Render(" Log ")).X(6).Y(3),
		lipgloss.NewLayer(strings.Join(lines, "\n")).X(5).Y(4),
	}
}

func eta(in engine.InstallState) string {
	if in.Err != "" || in.Progress < 0.1 {
		return ""
	}
	spent := time.Since(in.Started)
	left := time.Duration(float64(spent) * (1 - in.Progress) / in.Progress)
	switch {
	case left < time.Minute:
		return "less than a minute left"
	case left < 2*time.Minute:
		return "about a minute left"
	}
	return fmt.Sprintf("about %d minutes left", int(left.Minutes()+0.5))
}

// doing turns a step title into what is happening now: Copy becomes Copying.
func doing(title string) string {
	verb, rest, _ := strings.Cut(title, " ")
	switch {
	case strings.HasSuffix(verb, "e") && verb != "See":
		verb = verb[:len(verb)-1] + "ing"
	case verb == "Copy" || verb == "Apply":
		verb = verb[:len(verb)-1] + "ying"
	default:
		verb += "ing"
	}
	return verb + " " + rest
}

func (m *Model) keyReady(s string) {
	switch s {
	case "left":
		m.readyCur = 0
	case "right":
		m.readyCur = 1
	case "enter":
		if m.readyCur == 0 {
			if err := m.eng.Reboot(); err != nil {
				m.note = err.Error()
			}
		}
	}
}

func (m *Model) viewReady() screen {
	t := m.t
	l := m.layout()
	st := m.st
	p, _ := st.Purpose()
	lines := []string{t.B(t.Good).Render(t.Ok + " " + st.Hostname + " is installed"), "", t.B(t.Title).Render("At the next start")}
	n := 1
	if m.needsSecureBootOff(p) {
		lines = append(lines,
			t.S(t.Fg).Render(fmt.Sprintf("%d  Open the firmware setup and turn off Secure Boot.", n)),
			t.S(t.Muted).Render("   "+p.Name+" needs it off for now."), "")
		n++
	}
	if st.Install.MOKPassword != "" {
		lines = append(lines,
			t.S(t.Fg).Render(fmt.Sprintf("%d  A blue screen asks you to trust our signing key.", n)),
			t.S(t.Muted).Render("   Choose Enroll MOK, then type this password:"), "",
			"   "+t.Pill(t.Border, "  "+st.Install.MOKPassword+"  "), "")
		n++
	}
	if len(st.Plan.Pools) > 0 {
		lines = append(lines, t.S(t.Fg).Render(fmt.Sprintf("%d  %s starts and %s builds its %d %s.", n, st.Hostname, p.Name,
			len(st.Plan.Pools), plural(len(st.Plan.Pools), "pool", "pools"))))
	} else {
		lines = append(lines, t.S(t.Fg).Render(fmt.Sprintf("%d  %s starts from its own disk.", n, st.Hostname)))
	}
	lines = append(lines, t.S(t.Muted).Render("   Then its console shows the web address."))
	if a := m.address(); a != "" {
		lines = append(lines, "", t.B(t.Title).Render("Address"), t.S(t.Fg).Render(m.netSummary()))
	}
	lines = append(lines, "", t.S(t.Muted).Render("Its answers, to install more like it:"),
		t.S(t.Fg).Render("/etc/microshift-installer/site.yaml"))
	// The box, a blank row, the buttons and their note sit above the help.
	lines = squeeze(lines, l.helpY-8)
	bw := 69
	bx := (l.W - bw) / 2
	sc := screen{
		fullscreen: true, focusFrom: -1,
		help: [][2]string{{"left/right", "choose"}, {"enter", "press"}},
		pill: "Done", pillColor: t.Good, right: m.statusRight(),
	}
	sc.layers = append(sc.layers,
		lipgloss.NewLayer(m.box(bw, len(lines)+2, t.Border)).X(bx).Y(3),
		lipgloss.NewLayer(strings.Join(lines, "\n")).X(bx+3).Y(4))
	restart := t.Button("  Restart now  ", m.readyCur == 0, true, t.Border)
	stay := t.Button("  Stay here  ", m.readyCur == 1, true, t.Border)
	buttons := restart + "  " + stay
	note := "Remove the install media, then restart."
	if m.countdown > 0 {
		note = fmt.Sprintf("Restarting in %d s. Any key stops it.", m.countdown)
	}
	by := 3 + len(lines) + 3
	sc.layers = append(sc.layers,
		lipgloss.NewLayer(buttons).X((l.W-lipgloss.Width(buttons))/2).Y(by),
		lipgloss.NewLayer(t.S(t.Muted).Render(note)).X((l.W-lipgloss.Width(note))/2).Y(by+1))
	return sc
}
