package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/epheo/amorce/internal/engine"
	"github.com/epheo/amorce/internal/site"
)

// Settings changes the installed system: the console's F2, as on ESXi.
type Settings interface {
	// Network is this machine's network, and why it cannot change, if so.
	Network() (site.Answers, string)
	// SetNetwork keeps a new network for the next start.
	SetNetwork(site.Answers) error
	HasRootPassword() bool
	SetRootPassword(string) error
	Restart() error
	PowerOff() error
}

const (
	setMenu = iota
	setNetwork
	setPassword
	setRestart
	setPowerOff
)

var settingRows = []string{"Network", "Root password", "Restart", "Shut down"}

type settingsView struct {
	page, cur, field int
	net              site.Answers
	fixed            string // why the network cannot change
	hasPassword      bool
	in               [5]input // network: address, gateway, DNS, VLAN, NTP; password: twice
	confirm          bool
	busy             bool
	note             string
}

type settingDoneMsg struct {
	note string
	err  error
}

func (m *Model) openSettings() {
	s := &settingsView{}
	s.net, s.fixed = m.opt.Settings.Network()
	s.hasPassword = m.opt.Settings.HasRootPassword()
	m.set = s
}

func (m *Model) keySettings(key string, k tea.KeyPressMsg) tea.Cmd {
	s := m.set
	if s.busy {
		return nil
	}
	if key == "esc" {
		switch {
		case s.confirm:
			s.confirm = false
		case s.page != setMenu:
			s.page, s.note = setMenu, ""
		default:
			m.set = nil
		}
		return nil
	}
	switch s.page {
	case setMenu:
		switch key {
		case "up":
			s.cur = max(0, s.cur-1)
		case "down":
			s.cur = min(len(settingRows)-1, s.cur+1)
		case "enter":
			s.page, s.field, s.confirm, s.note = s.cur+1, 0, false, ""
			if s.page == setNetwork {
				s.in = networkInputs(s.net)
			}
			if s.page == setPassword {
				s.in = [5]input{}
			}
		}
	case setNetwork:
		if s.fixed != "" {
			return nil
		}
		switch key {
		case "tab", "down":
			s.field = (s.field + 1) % len(s.in)
		case "shift+tab", "up":
			s.field = (s.field + len(s.in) - 1) % len(s.in)
		case "enter":
			a, msg := networkAnswers(s.net, s.in)
			if msg != "" {
				s.note = msg
				return nil
			}
			if !s.confirm {
				s.confirm, s.note = true, ""
				return nil
			}
			s.busy = true
			st := m.opt.Settings
			return func() tea.Msg {
				if err := st.SetNetwork(a); err != nil {
					return settingDoneMsg{err: err}
				}
				return settingDoneMsg{err: st.Restart(), note: "Restarting."}
			}
		default:
			s.confirm = false
			s.in[s.field].handle(key, k)
		}
	case setPassword:
		switch key {
		case "tab", "down", "shift+tab", "up":
			s.field = 1 - s.field
		case "enter":
			switch {
			case s.field == 0:
				s.field = 1
			case len(s.in[0].value) < 8:
				s.note = "Eight characters at least."
			case s.in[0].value != s.in[1].value:
				s.note, s.field = "The two differ; type it again.", 0
				s.in = [5]input{}
			default:
				s.busy = true
				st, pw := m.opt.Settings, s.in[0].value
				return func() tea.Msg {
					return settingDoneMsg{err: st.SetRootPassword(pw), note: "Root password set: enter logs in with it."}
				}
			}
		default:
			s.in[s.field].handle(key, k)
			s.note = ""
		}
	case setRestart, setPowerOff:
		if key == "enter" {
			s.busy = true
			st, page := m.opt.Settings, s.page
			return func() tea.Msg {
				if page == setPowerOff {
					return settingDoneMsg{err: st.PowerOff(), note: "Shutting down."}
				}
				return settingDoneMsg{err: st.Restart(), note: "Restarting."}
			}
		}
	}
	return nil
}

func (m *Model) settingDone(msg settingDoneMsg) {
	s := m.set
	if s == nil {
		return
	}
	s.busy, s.confirm = false, false
	if msg.err != nil {
		s.note = msg.err.Error()
		return
	}
	s.hasPassword = s.hasPassword || s.page == setPassword
	s.page, s.note = setMenu, msg.note
}

func networkInputs(a site.Answers) [5]input {
	var n site.Network
	if a.Network != nil {
		n = *a.Network
	}
	vlan := ""
	if n.VLAN > 0 {
		vlan = strconv.Itoa(n.VLAN)
	}
	return [5]input{newInput(a.Address), newInput(n.Gateway), newInput(strings.Join(n.DNS, ", ")), newInput(vlan),
		newInput(strings.Join(n.NTP, ", "))}
}

// networkAnswers reads the form into answers, checked as the installer
// checks a site file's.
func networkAnswers(cur site.Answers, in [5]input) (site.Answers, string) {
	a := site.Answers{Address: strings.TrimSpace(in[0].value)}
	n := site.Network{Gateway: strings.TrimSpace(in[1].value)}
	if cur.Network != nil {
		n.Interface = cur.Network.Interface
	}
	for _, d := range strings.FieldsFunc(in[2].value, func(r rune) bool { return r == ',' || r == ' ' }) {
		n.DNS = append(n.DNS, d)
	}
	n.NTP = strings.FieldsFunc(strings.ToLower(in[4].value), func(r rune) bool { return r == ',' || r == ' ' })
	if v := strings.TrimSpace(in[3].value); v != "" {
		vlan, err := strconv.Atoi(v)
		if err != nil || vlan < 1 || vlan > 4094 {
			return a, "A VLAN is 1 to 4094, or empty."
		}
		n.VLAN = vlan
	}
	a.Network = &n
	if a.Address == "dhcp" && (n.Gateway != "" || len(n.DNS) > 0) {
		return a, "With dhcp, the gateway and DNS come from DHCP: leave them empty."
	}
	if !strings.Contains(a.Address, "/") && a.Address != "dhcp" {
		return a, "Give the address with its prefix, like 10.20.120.12/24, or dhcp."
	}
	if _, msg := engine.SiteNet(a, engine.Machine{}); msg != "" {
		return a, msg
	}
	return a, ""
}

func (m *Model) viewSettings() screen {
	t := m.t
	l := m.layout()
	s := m.set
	kv := func(k, v string, st lipgloss.Style) string { return t.S(t.Muted).Width(16).Render(k) + st.Render(v) }
	lines := []string{t.B(t.Title).Render("Settings"), ""}
	help := [][2]string{{"up/down", "choose"}, {"enter", "change"}, {"esc", "back"}}
	switch s.page {
	case setMenu:
		values := []string{networkSummary(s.net), "not set: enter cannot log in", "", ""}
		if s.hasPassword {
			values[1] = "set"
		}
		for i, row := range settingRows {
			mark, st := "  ", t.S(t.Fg)
			if i == s.cur {
				mark, st = t.S(t.Mauve).Render(t.List+" "), t.B(t.Accent)
			}
			lines = append(lines, mark+st.Width(16).Render(row)+t.S(t.Muted).Render(values[i]))
		}
		if a := s.net; a.Domain != "" || (a.Identity != "" && a.Identity != "none") {
			signIn := "a token"
			if a.Identity != "" && a.Identity != "none" {
				signIn = engine.ProviderName(a.Identity)
			}
			lines = append(lines, "", "  "+t.S(t.Muted).Width(16).Render("Domain")+t.S(t.Fg).Render(a.Domain),
				"  "+t.S(t.Muted).Width(16).Render("Sign-in")+t.S(t.Fg).Render(signIn),
				"  "+t.S(t.Muted).Render("Set at install: passkeys and accounts are bound to them."))
		}
	case setNetwork:
		lines[0] = t.B(t.Title).Render("Network")
		iface := ""
		if s.net.Network != nil {
			iface = s.net.Network.Interface
		}
		lines = append(lines, kv("Interface", iface, t.S(t.Fg)))
		if s.fixed != "" {
			lines = append(lines, kv("Address", networkSummary(s.net), t.S(t.Fg)), "",
				t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(s.fixed))
			help = [][2]string{{"esc", "back"}}
			break
		}
		for i, label := range []string{"Address", "Gateway", "DNS", "VLAN", "NTP"} {
			lines = append(lines, t.S(t.Muted).Width(14).Render(label)+m.inputView(s.in[i], i == s.field))
		}
		lines = append(lines, "", t.S(t.Muted).Render("An address with its prefix, or dhcp. The rest may stay empty."))
		if s.confirm {
			lines = append(lines, "", t.S(t.Warn).Render(t.Alert+" ")+
				t.S(t.Fg).Render(m.home.Name+" restarts to use it; MicroShift keeps its data."),
				t.S(t.Muted).Render("  enter restarts now, any other key keeps editing"))
		}
		help = [][2]string{{"tab", "next field"}, {"enter", "apply"}, {"esc", "back"}}
	case setPassword:
		lines[0] = t.B(t.Title).Render("Root password")
		lines = append(lines,
			t.S(t.Muted).Width(14).Render("New")+m.secretView(s.in[0], s.field == 0),
			t.S(t.Muted).Width(14).Render("Again")+m.secretView(s.in[1], s.field == 1), "",
			t.S(t.Muted).Render("Enter on this screen, and alt+F2, log in as root with it."))
		help = [][2]string{{"tab", "next field"}, {"enter", "set"}, {"esc", "back"}}
	case setRestart, setPowerOff:
		what := "Restart"
		if s.page == setPowerOff {
			what = "Shut down"
		}
		lines[0] = t.B(t.Title).Render(what)
		lines = append(lines, t.S(t.Fg).Render(what+" "+m.home.Name+"? Its workloads stop until it is back."))
		if s.page == setPowerOff {
			lines = append(lines, t.S(t.Muted).Render("It starts again from its power button or BMC only."))
		}
		help = [][2]string{{"enter", strings.ToLower(what)}, {"esc", "back"}}
	}
	if s.busy {
		lines = append(lines, "", m.spinner()+" "+t.S(t.Fg).Render("Working..."))
	} else if s.note != "" {
		lines = append(lines, "", t.S(t.Fg).Render(s.note))
	}
	return screen{
		fullscreen: true, focusFrom: -1, title: m.home.Name, bar: "SETTINGS", help: help,
		right:  strings.Join(append([]string{m.home.Name}, m.home.Address...), "  "),
		layers: []*lipgloss.Layer{lipgloss.NewLayer(strings.Join(squeeze(lines, l.helpY-4), "\n")).X(l.leftX).Y(3)},
	}
}

func networkSummary(a site.Answers) string {
	out := a.Address
	if a.Network != nil {
		if a.Network.VLAN > 0 {
			out += ", VLAN " + strconv.Itoa(a.Network.VLAN)
		}
		if a.Network.Gateway != "" {
			out += ", gateway " + a.Network.Gateway
		}
	}
	return out
}

// secretView draws a field as stars.
func (m *Model) secretView(in input, focused bool) string {
	return m.inputView(input{value: strings.Repeat("*", len([]rune(in.value)))}, focused)
}
