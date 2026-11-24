package ui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/epheo/microshift-installer/internal/engine"
)

// Options describe the terminal one Model draws on.
type Options struct {
	Look Look
	// Where names this terminal on the welcome screen's status bar.
	Where string
	// Shell opens a shell on this terminal; nil disables F12.
	Shell func() tea.ExecCommand
}

// EngineMsg tells the model the engine state changed.
type EngineMsg struct{}

type tickMsg time.Time

type shellDoneMsg struct{ err error }

type Model struct {
	eng *engine.Engine
	ctx context.Context
	opt Options
	t   Theme
	st  engine.State

	w, h  int
	frame int
	ticks int
	shown engine.Step

	helloCur   int
	kb         *keyboardPicker
	netCur     int
	form       *netForm
	purposeCur int
	disksCur   int
	roles      *roleEditor
	name       input
	confirm    input
	confirmTab bool
	readyCur   int
	countdown  int
	showLog    bool
	note       string
}

func New(ctx context.Context, eng *engine.Engine, opt Options) *Model {
	m := &Model{eng: eng, ctx: ctx, opt: opt, t: NewTheme(opt.Look), w: 100, h: 30, countdown: -1}
	m.st = eng.Snapshot()
	m.shown = m.st.Step
	return m
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Init() tea.Cmd { return tick() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case EngineMsg:
		m.st = m.eng.Snapshot()
		if m.st.Step != m.shown {
			m.enter(m.st.Step)
		}
	case tickMsg:
		m.frame++
		m.ticks++
		if m.ticks%10 == 0 && m.countdown > 0 {
			m.countdown--
			if m.countdown == 0 {
				m.countdown = -1
				if err := m.eng.Reboot(); err != nil {
					m.note = err.Error()
				}
			}
		}
		return m, tick()
	case shellDoneMsg:
		if msg.err != nil {
			m.note = "The shell ended: " + msg.err.Error()
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

// enter resets this terminal's cursors when any terminal changed the step.
func (m *Model) enter(s engine.Step) {
	m.shown = s
	m.note = ""
	m.form, m.roles, m.kb = nil, nil, nil
	switch s {
	case engine.StepNetwork:
		m.netCur = 0
	case engine.StepPurpose:
		m.purposeCur = max(0, m.purposeIndex(m.st.PurposeID))
	case engine.StepDisks:
		m.disksCur = 0
	case engine.StepName:
		m.name = newInput(m.st.Hostname)
	case engine.StepConfirm:
		m.confirm = newInput("")
		m.confirmTab = false
	case engine.StepReady:
		m.readyCur = 0
		m.countdown = 10
	}
}

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if m.countdown > 0 {
		m.countdown = -1
	}
	// Any key on any console hands an answered install to a person.
	if r := m.st.Site; r != nil && !r.Stopped && m.st.Step < engine.StepInstalling {
		m.eng.StopSite()
		return nil
	}
	if s == "f12" && m.opt.Shell != nil {
		return tea.Exec(m.opt.Shell(), func(err error) tea.Msg { return shellDoneMsg{err} })
	}
	if m.kb != nil {
		m.keyKeyboard(s, k)
		return nil
	}
	switch m.st.Step {
	case engine.StepHello:
		return m.keyHello(s)
	case engine.StepNetwork:
		m.keyNetwork(s, k)
	case engine.StepPurpose:
		m.keyPurpose(s)
	case engine.StepDisks:
		m.keyDisks(s)
	case engine.StepName:
		m.keyName(s, k)
	case engine.StepConfirm:
		m.keyConfirm(s, k)
	case engine.StepInstalling:
		m.keyInstalling(s)
	case engine.StepReady:
		m.keyReady(s)
	}
	return nil
}

// screen is one rendered step: a left column, a side panel, keys, and extras.
type screen struct {
	left       []string
	focusFrom  int // rows of left to mark with the focus bar; -1 for none
	focusTo    int
	panel      []string
	help       [][2]string
	pill       string
	pillColor  color.Color
	stepNo     int
	right      string
	layers     []*lipgloss.Layer
	fullscreen bool // the screen draws its own body; no left column or panel
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// panelInner is the side panel's text width, inside its border and margin.
const panelInner = 30

// layout is the 100x30 grid the mockups use, or a narrow one for 80x24.
type layout struct {
	W, H         int
	wide         bool
	leftX, leftW int
	panelX       int
	helpY, barY  int
}

func (m *Model) layout() layout {
	if m.w >= 100 && m.h >= 30 {
		return layout{W: 100, H: 30, wide: true, leftX: 5, leftW: 56, panelX: 64, helpY: 27, barY: 29}
	}
	w, h := max(m.w, 60), max(m.h, 20)
	return layout{W: w, H: h, leftX: 3, leftW: w - 6, helpY: h - 3, barY: h - 1}
}

func (m *Model) render() string {
	sc := m.screen()
	l := m.layout()
	t := m.t
	var layers []*lipgloss.Layer
	add := func(x, y int, s string) {
		layers = append(layers, lipgloss.NewLayer(s).X(x).Y(y))
	}

	title := "Installer"
	add(3, 1, t.Gradient(title)+t.S(t.Border).Render(" "+strings.Repeat(t.Slash, l.W-6-len(title))))

	if !sc.fullscreen {
		left := sc.left
		maxRows := l.helpY - 4
		if len(left) > maxRows {
			left = squeeze(left, maxRows)
		}
		for i, line := range left {
			left[i] = ansi.Truncate(line, l.leftW, "…")
		}
		add(l.leftX, 3, strings.Join(left, "\n"))
		if sc.focusFrom >= 0 && sc.focusTo >= sc.focusFrom && sc.focusTo < len(left) {
			bar := strings.TrimSuffix(strings.Repeat(t.S(t.BorderDim).Render(t.Focus)+"\n", sc.focusTo-sc.focusFrom+1), "\n")
			add(l.leftX-2, 3+sc.focusFrom, bar)
		}
		if l.wide && len(sc.panel) > 0 {
			h := max(15, min(len(sc.panel)+2, l.helpY-4))
			panel := sc.panel[:min(len(sc.panel), h-2)]
			for i, line := range panel {
				panel[i] = ansi.Truncate(line, panelInner, "…")
			}
			add(l.panelX, 3, m.box(panelInner+3, h, t.Border))
			add(l.panelX+2, 4, strings.Join(panel, "\n"))
		}
	}
	for _, ly := range sc.layers {
		layers = append(layers, ly)
	}
	note := m.note
	if p := m.siteProblem(); note == "" && p != "" && !sc.fullscreen {
		note = t.Alert + " " + p
	}
	if note != "" {
		add(l.leftX, l.helpY-1, t.S(t.Warn).Render(ansi.Truncate(note, l.W-l.leftX-2, "…")))
	}
	add(3, l.helpY, m.helpLine(sc.help, l.W-4))
	add(0, l.barY, m.statusBar(sc, l.W))

	c := lipgloss.NewCanvas(l.W, l.H)
	c.Compose(lipgloss.NewCompositor(layers...))
	out := c.Render()
	// Center the frame as one block; lipgloss.Place would center each line
	// on its own width, and the canvas trims trailing spaces.
	if m.w > l.W || m.h > l.H {
		pad := strings.Repeat(" ", max(0, (m.w-l.W)/2))
		lines := strings.Split(out, "\n")
		for i := range lines {
			lines[i] = pad + lines[i]
		}
		out = strings.Repeat("\n", max(0, (m.h-l.H)/2)) + strings.Join(lines, "\n")
	}
	return out
}

// squeeze drops blank rows, bottom first, until lines fits n rows.
// siteProblem is why the site file needs a person on this screen, or "".
func (m *Model) siteProblem() string {
	if r := m.st.Site; r != nil && r.Problem != "" && m.st.Step == r.ProblemStep {
		return r.Problem
	}
	return ""
}

func squeeze(lines []string, n int) []string {
	out := append([]string(nil), lines...)
	for i := len(out) - 1; i > 0 && len(out) > n; i-- {
		if strings.TrimSpace(ansi.Strip(out[i])) == "" && strings.TrimSpace(ansi.Strip(out[i-1])) == "" {
			out = append(out[:i], out[i+1:]...)
		}
	}
	for i := len(out) - 1; i >= 0 && len(out) > n; i-- {
		if strings.TrimSpace(ansi.Strip(out[i])) == "" {
			out = append(out[:i], out[i+1:]...)
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (m *Model) box(w, h int, c color.Color) string {
	b := m.t.Box
	st := m.t.S(c)
	var rows []string
	rows = append(rows, st.Render(b.TopLeft+strings.Repeat(b.Top, w-2)+b.TopRight))
	for range h - 2 {
		rows = append(rows, st.Render(b.Left)+strings.Repeat(" ", w-2)+st.Render(b.Right))
	}
	rows = append(rows, st.Render(b.BottomLeft+strings.Repeat(b.Bottom, w-2)+b.BottomRight))
	return strings.Join(rows, "\n")
}

func (m *Model) helpLine(keys [][2]string, width int) string {
	t := m.t
	var parts []string
	for _, k := range keys {
		parts = append(parts, t.S(t.Key).Render(k[0])+t.S(t.Muted).Render(" "+k[1]))
	}
	if m.opt.Shell != nil && m.st.Step != engine.StepConfirm {
		parts = append(parts, t.S(t.Key).Render("F12")+t.S(t.Muted).Render(" shell"))
	}
	return ansi.Truncate(strings.Join(parts, t.S(t.Faint).Render(t.Sep)), width, "")
}

func (m *Model) statusBar(sc screen, width int) string {
	t := m.t
	on := lipgloss.NewStyle().Background(t.Bar)
	left := t.Pill(t.Accent, " SETUP ")
	if sc.pill != "" {
		left += t.Pill(sc.pillColor, " "+sc.pill+" ")
	}
	if sc.stepNo > 0 {
		left += on.Foreground(t.BarText).Render(fmt.Sprintf(" step %d of %d", sc.stepNo, engine.WizardSteps))
	}
	right := on.Foreground(t.BarText).Render(sc.right + " ")
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, width, "")
	}
	return left + on.Render(strings.Repeat(" ", gap)) + right
}

func (m *Model) screen() screen {
	if m.kb != nil {
		return m.viewKeyboard()
	}
	switch m.st.Step {
	case engine.StepNetwork:
		return m.viewNetwork()
	case engine.StepPurpose:
		return m.viewPurpose()
	case engine.StepDisks:
		return m.viewDisks()
	case engine.StepName:
		return m.viewName()
	case engine.StepConfirm:
		return m.viewConfirm()
	case engine.StepInstalling:
		return m.viewInstalling()
	case engine.StepReady:
		return m.viewReady()
	}
	return m.viewHello()
}

// statusRight is the address once known, as the mockups show it.
func (m *Model) statusRight() string {
	parts := []string{}
	if m.st.Step >= engine.StepConfirm && m.st.Hostname != "" {
		parts = append(parts, m.st.Hostname)
	}
	if a := m.address(); a != "" {
		parts = append(parts, a)
	}
	return strings.Join(parts, "  ")
}

func (m *Model) address() string {
	if m.st.Net.Up && m.st.Net.Config.Address.IsValid() {
		return m.st.Net.Config.Address.Addr().String()
	}
	return ""
}

func (m *Model) spinner() string {
	return m.t.S(m.t.Title).Render(m.t.Spin[m.frame%len(m.t.Spin)])
}

// setup is the "Your setup" list every wizard panel starts with.
func (m *Model) setup() []string {
	t := m.t
	st := m.st
	type row struct {
		label, value string
		step         engine.Step
	}
	purpose, _ := st.Purpose()
	rows := []row{
		{"Keyboard", st.Keyboard, engine.StepHello},
		{"Network", m.address(), engine.StepNetwork},
		{"Purpose", purpose.Name, engine.StepPurpose},
		{"Disks", m.planSummary(), engine.StepDisks},
		{"Name", st.Hostname, engine.StepName},
	}
	out := []string{t.B(t.Good).Render("Your setup"), ""}
	for _, r := range rows {
		label := t.S(t.Muted).Width(10).Render(r.label)
		switch {
		case r.step < st.Step || (r.step == engine.StepHello && st.Step == engine.StepHello):
			out = append(out, t.S(t.Good).Render(t.Ok+" ")+label+t.S(t.Fg).Render(r.value))
		case r.step == st.Step && r.step == engine.StepNetwork && m.netProblem():
			out = append(out, t.S(t.Warn).Render(t.Alert+" ")+label+t.S(t.Warn).Render("needs you"))
		case r.step == st.Step:
			out = append(out, t.S(t.Title).Render(t.Cur+" ")+label+t.S(t.Title).Render("choosing"))
		default:
			out = append(out, t.S(t.Faint).Render(t.Pend+" ")+t.S(t.Muted).Render(r.label))
		}
	}
	return out
}

func (m *Model) planSummary() string {
	p := m.st.Plan
	if len(p.Pools) > 0 {
		return humanBytes(m.dataBytes())
	}
	if d, ok := m.st.Disk(p.System); ok {
		return d.Name
	}
	return ""
}

// dataBytes is the usable space of the planned pools.
func (m *Model) dataBytes() uint64 {
	var total uint64
	for _, pool := range m.st.Plan.Pools {
		var small uint64
		for i, n := range pool.Disks {
			d, _ := m.st.Disk(n)
			if i == 0 || d.Size < small {
				small = d.Size
			}
		}
		if pool.Raid == "raid1" {
			total += small
		} else {
			total += small * uint64(len(pool.Disks))
		}
	}
	return total
}

// choice renders one option of a select; the chosen one gets the cursor.
func (m *Model) choice(label string, on bool) string {
	t := m.t
	if on {
		return t.S(t.Accent).Render("> ") + t.S(t.Good).Render(label)
	}
	return "  " + t.S(t.Fg).Render(label)
}

func humanBytes(n uint64) string {
	f := float64(n)
	switch {
	case f >= 1e12:
		s := fmt.Sprintf("%.1f TB", f/1e12)
		return strings.Replace(s, ".0 ", " ", 1)
	case f >= 1e9:
		return fmt.Sprintf("%.0f GB", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("%.0f MB", f/1e6)
	}
	return fmt.Sprintf("%d B", n)
}
