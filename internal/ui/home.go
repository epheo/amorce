package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/epheo/microshift-installer/internal/report"
)

// HomeInfo is what the installed console shows; Options.Home refreshes it.
type HomeInfo struct {
	Name    string
	Address []string
	// Health is systemd's view: running, degraded, starting.
	Health string
	Failed int // units, when degraded
	Serial string
	Image  string
	// Notes are what the purpose tells its admins, by source, in order.
	Notes []Note
	// Site names the site file; Board is set on the first machine only.
	Site  string
	Board []report.Report
}

type Note struct {
	From  string
	Lines []string
}

func (m *Model) keyHome(s string) tea.Cmd {
	switch {
	case s == "f3" && len(m.home.Board) > 0:
		m.homeBoard = !m.homeBoard
	case s == "esc":
		m.homeBoard = false
	case s == "enter" && m.opt.Login != nil:
		return tea.Exec(m.opt.Login(), func(err error) tea.Msg { return shellDoneMsg{err} })
	}
	return nil
}

func (m *Model) viewHome() screen {
	t := m.t
	l := m.layout()
	h := m.home
	width := l.W - 2*l.leftX
	kv := func(k, v string, st lipgloss.Style) string {
		return t.S(t.Muted).Width(10).Render(k) + st.Render(v)
	}
	var lines []string
	lines = append(lines, t.B(t.Title).Render("This machine"))
	if len(h.Address) > 0 {
		lines = append(lines, kv("Address", strings.Join(h.Address, ", "), t.S(t.Fg)))
	} else {
		lines = append(lines, kv("Address", "none yet", t.S(t.Warn)))
	}
	health, hs := h.Health, t.S(t.Muted)
	switch h.Health {
	case "running":
		hs = t.S(t.Good)
	case "degraded":
		health, hs = fmt.Sprintf("degraded: %d %s failed", h.Failed, plural(h.Failed, "unit", "units")), t.S(t.Warn)
	}
	lines = append(lines, kv("Health", health, hs))
	if h.Serial != "" {
		lines = append(lines, kv("Serial", h.Serial, t.S(t.Fg)))
	}
	if h.Image != "" {
		lines = append(lines, kv("Image", h.Image, t.S(t.Fg)))
	}
	for _, n := range h.Notes {
		lines = append(lines, "", t.B(t.Title).Render(n.From))
		for _, line := range n.Lines {
			for _, w := range wrapLine(line, width) {
				lines = append(lines, t.S(t.Fg).Render(w))
			}
		}
	}
	rows := l.helpY - 4
	if len(h.Board) > 0 {
		board := m.board(h, len(h.Board)+1)
		if m.homeBoard {
			lines = m.board(h, rows)
		} else if len(squeeze(lines, rows))+1+len(board) <= rows {
			lines = append(append(lines, ""), board...)
		} else {
			lines = append(lines, "", board[0])
		}
	}
	sc := screen{
		fullscreen: true, focusFrom: -1, title: h.Name, bar: "HOME",
		right: strings.Join(append([]string{h.Name}, h.Address...), "  "),
	}
	switch {
	case m.homeBoard:
		sc.help = append(sc.help, [2]string{"esc", "back"})
	case len(h.Board) > 0:
		sc.help = append(sc.help, [2]string{"F3", "machines"})
	}
	if m.opt.Login != nil {
		sc.help = append(sc.help, [2]string{"enter", "log in"})
	}
	sc.help = append(sc.help, [2]string{"alt+F2", "another console"})
	lines = squeeze(lines, rows)
	sc.layers = append(sc.layers, lipgloss.NewLayer(strings.Join(lines, "\n")).X(l.leftX).Y(3))
	return sc
}

// wrapLine breaks s at spaces to fit width, and a long word, such as a
// fingerprint, after a colon, so byte pairs stay whole.
func wrapLine(s string, width int) []string {
	var out []string
	cur := ""
	put := func(w string) {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= width:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = w
		}
	}
	for _, word := range strings.Fields(s) {
		if len(word) <= width {
			put(word)
			continue
		}
		if cur != "" {
			out = append(out, cur)
			cur = ""
		}
		for _, part := range strings.SplitAfter(word, ":") {
			if len(cur)+len(part) > width && cur != "" {
				out = append(out, cur)
				cur = ""
			}
			cur += part
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// board lists the site's machines in at most rows lines.
func (m *Model) board(h HomeInfo, rows int) []string {
	t := m.t
	running := 0
	for _, r := range h.Board {
		if r.State == report.Running {
			running++
		}
	}
	head := "Machines"
	if h.Site != "" {
		head += " in site " + h.Site
	}
	out := []string{t.B(t.Title).Render(head) + t.S(t.Muted).Render(fmt.Sprintf("   %d of %d running", running, len(h.Board)))}
	for i, r := range h.Board {
		if len(out) == rows-1 && i < len(h.Board)-1 {
			out = append(out, t.S(t.Muted).Render(fmt.Sprintf("  and %d more", len(h.Board)-i)))
			break
		}
		sym, st, state, detail := t.Pend, t.S(t.Faint), "not seen", ""
		switch r.State {
		case report.Running:
			sym, st, state, detail = t.Ok, t.S(t.Good), "running", r.Address
		case report.Installed:
			sym, st, state, detail = t.Ok, t.S(t.Good), "installed", "restarting"
		case report.Installing:
			sym, st, state = t.Cur, t.S(t.Accent), "installing"
			detail = fmt.Sprintf("%s  %d%%", r.Step, int(r.Progress*100))
		case report.Waiting:
			sym, st, state, detail = t.Alert, t.S(t.Warn), "waiting", r.Detail
		case report.Failed:
			sym, st, state, detail = t.Fail, t.S(t.Bad), "failed", r.Detail
		}
		row := st.Render(sym+" ") + t.S(t.Fg).Width(14).Render(r.Name) + st.Width(12).Render(state) + t.S(t.Muted).Render(detail)
		out = append(out, ansi.Truncate(row, m.layout().W-2*m.layout().leftX, "…"))
	}
	return out
}
