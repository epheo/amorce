package ui

import (
	"fmt"
	"strings"
	"time"

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
	// Cluster says how this machine belongs to the site's cluster.
	Cluster     string
	ClusterBusy bool
	// CanAdd is set on the first machine: F4 shows Words, which admit a new
	// machine until WordsUntil; WordsNote says why they closed early.
	CanAdd     bool
	Words      []string
	WordsUntil time.Time
	WordsNote  string
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
	case s == "f4" && m.home.CanAdd && m.opt.Words != nil:
		m.opt.Words()
		m.home = m.opt.Home()
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
	// Serial and image give way first on a small screen: the purpose's
	// notes are never cut, and open words must show.
	var lines []string
	var spare []int
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
	if h.Cluster != "" {
		st := t.S(t.Fg)
		if h.ClusterBusy {
			st = t.S(t.Warn)
		}
		lines = append(lines, kv("Cluster", h.Cluster, st))
	}
	if h.Image != "" {
		spare = append(spare, len(lines))
		lines = append(lines, kv("Image", h.Image, t.S(t.Fg)))
	}
	if h.Serial != "" {
		spare = append(spare, len(lines))
		lines = append(lines, kv("Serial", h.Serial, t.S(t.Fg)))
	}
	if len(h.Words) > 0 {
		lines = append(lines, "",
			t.B(t.Title).Render("Add machines")+t.S(t.Muted).Render("   until "+h.WordsUntil.Format("15:04")),
			t.S(t.Fg).Render("In the new machine's installer, choose Join "+h.Name+", and type:"),
			"  "+t.B(t.Accent).Render(strings.Join(h.Words, " ")))
	} else if h.WordsNote != "" {
		lines = append(lines, "", t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(h.WordsNote))
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
	var board []string
	if len(h.Board) > 0 {
		board = m.board(h, len(h.Board)+1)
		if len(squeeze(lines, rows))+1+len(board) > rows {
			board = board[:1]
		}
	}
	for len(spare) > 0 && len(squeeze(lines, rows))+len(board) > rows {
		i := spare[len(spare)-1]
		spare = spare[:len(spare)-1]
		lines = append(lines[:i], lines[i+1:]...)
	}
	if len(board) > 0 {
		lines = append(append(squeeze(lines, rows-len(board)), ""), board...)
	}
	if m.homeBoard {
		lines = m.board(h, rows)
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
	switch {
	case h.CanAdd && len(h.Words) > 0:
		sc.help = append(sc.help, [2]string{"F4", "close the words"})
	case h.CanAdd:
		sc.help = append(sc.help, [2]string{"F4", "add machines"})
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
			if r.Role == report.Worker {
				detail += "  worker"
			}
		case report.Joining:
			sym, st, state, detail = t.Cur, t.S(t.Accent), "joining", r.Detail
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
