package ui

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/epheo/microshift-installer/internal/engine"
)

type netAction int

const (
	actKeepFixed netAction = iota
	actKeepDHCP
	actContinue
	actEdit
	actRetest
	actUseVLAN
	actKeepAnyway
)

type netChoice struct {
	label string
	act   netAction
	vlan  int
}

// netProblem is true when the network needs a decision before going on.
func (m *Model) netProblem() bool {
	r := m.st.Net
	return !m.st.NetTesting && (!r.Up || r.Conflict != "" || r.Gateway == engine.CheckFailed)
}

func vlanName(v int) string {
	if v == 0 {
		return "untagged"
	}
	return fmt.Sprintf("VLAN %d", v)
}

// switchVLAN is the port VLAN the switch announces for iface, or 0.
func (m *Model) switchVLAN(iface string) int {
	for _, n := range m.st.Machine.NICs {
		if n.Name == iface && n.Neighbor != nil {
			return n.Neighbor.VLAN
		}
	}
	return 0
}

// netReport renders the test result and the options it leads to.
func (m *Model) netReport() ([]string, []netChoice) {
	t := m.t
	r := m.st.Net
	c := r.Config
	bad := func(s string) string { return t.S(t.Bad).Render(t.Fail+" ") + t.B(t.Fg).Render(s) }
	good := func(s string) string { return t.S(t.Good).Render(t.Ok+" ") + t.B(t.Fg).Render(s) }
	muted := func(s string) string { return t.S(t.Muted).Render("  " + s) }
	ago := muted("Tested just now.")
	if !r.At.IsZero() && time.Since(r.At) > 30*time.Second {
		ago = muted(fmt.Sprintf("Tested %s ago.", time.Since(r.At).Round(time.Second)))
	}

	switch {
	case m.st.NetTesting:
		return []string{m.spinner() + t.S(t.Fg).Render(" Testing the network")}, nil
	case !r.Up && c.Mode == engine.NetDHCP:
		return []string{bad("No DHCP server answered"), muted("Enter the address this machine should use.")},
			[]netChoice{{label: "Enter the settings", act: actEdit}, {label: "Ask DHCP again", act: actRetest}}
	case !r.Up:
		return []string{bad("These settings did not come up"), ago},
			[]netChoice{{label: "Edit the settings by hand", act: actEdit}, {label: "Test again", act: actRetest}}
	case r.Conflict != "":
		return []string{bad("Another machine uses " + c.Address.Addr().String()), muted("It answers from " + r.Conflict + ".")},
			[]netChoice{{label: "Edit the settings by hand", act: actEdit}, {label: "Test again", act: actRetest}}
	case r.Gateway == engine.CheckFailed && len(r.GatewayVLANs) > 0:
		v := r.GatewayVLANs[0]
		lines := []string{bad("The gateway does not answer on " + vlanName(c.VLAN))}
		where := "It answers " + vlanName(v)
		if sv := m.switchVLAN(c.Iface); v == 0 && sv != 0 {
			lines = append(lines, muted(where+"; the switches call it VLAN "+strconv.Itoa(sv)+"."),
				muted("Tested just now. The switches (LLDP) agree."))
		} else {
			lines = append(lines, muted(where+"."), ago)
		}
		use := "Use " + vlanName(v) + ", where the gateway is"
		if sv := m.switchVLAN(c.Iface); v == 0 && sv != 0 {
			use = "Use VLAN " + strconv.Itoa(sv) + " untagged, where the gateway is"
		}
		return lines, []netChoice{
			{label: use, act: actUseVLAN, vlan: v},
			{label: "Keep " + vlanName(c.VLAN) + "; I will fix the switches", act: actKeepAnyway},
			{label: "Edit the settings by hand", act: actEdit},
		}
	case r.Gateway == engine.CheckFailed:
		return []string{bad("The gateway " + c.Gateway.String() + " does not answer"),
				muted("Not on " + vlanName(c.VLAN) + ", and nowhere else we looked."), ago},
			[]netChoice{{label: "Edit the settings by hand", act: actEdit},
				{label: "Test again", act: actRetest},
				{label: "Keep these settings anyway", act: actKeepAnyway}}
	}
	lines := []string{good("The network works")}
	detail := "The gateway answers"
	switch r.DNS {
	case engine.CheckOK:
		detail += ", and so does DNS."
	case engine.CheckFailed:
		detail += "; DNS does not."
	default:
		detail += "."
	}
	lines = append(lines, muted(detail))
	if c.Mode == engine.NetDHCP {
		return lines, []netChoice{
			{label: "Keep " + c.Address.Addr().String() + ", as a fixed address", act: actKeepFixed},
			{label: "Keep using DHCP", act: actKeepDHCP},
			{label: "Edit the settings by hand", act: actEdit},
		}
	}
	return lines, []netChoice{{label: "Continue with these settings", act: actContinue},
		{label: "Edit the settings by hand", act: actEdit}}
}

func (m *Model) keyNetwork(s string, k tea.KeyPressMsg) {
	if m.form != nil {
		m.keyForm(s, k)
		return
	}
	_, choices := m.netReport()
	switch s {
	case "esc":
		m.eng.Go(engine.StepHello)
	case "up":
		m.netCur = max(0, m.netCur-1)
	case "down":
		m.netCur = min(len(choices)-1, m.netCur+1)
	case "t":
		m.eng.TestNetwork(m.ctx, m.st.Net.Config)
	case "enter":
		if m.netCur >= len(choices) {
			return
		}
		ch := choices[m.netCur]
		cfg := m.st.Net.Config
		switch ch.act {
		case actKeepFixed:
			cfg.Mode = engine.NetStatic
			m.eng.ChooseNetwork(cfg)
			m.eng.Go(engine.StepPurpose)
		case actKeepDHCP, actContinue, actKeepAnyway:
			m.eng.ChooseNetwork(cfg)
			m.eng.Go(engine.StepPurpose)
		case actEdit:
			m.form = newNetForm(m.st, cfg)
		case actRetest:
			m.eng.TestNetwork(m.ctx, cfg)
		case actUseVLAN:
			cfg.VLAN = ch.vlan
			m.netCur = 0
			m.eng.TestNetwork(m.ctx, cfg)
		}
	}
}

func (m *Model) viewNetwork() screen {
	t := m.t
	l := m.layout()
	sc := screen{
		pill: "Network", pillColor: t.Border, stepNo: 2, right: m.statusRight(),
		help: [][2]string{{"up/down", "choose"}, {"enter", "apply"}, {"t", "test again"}, {"esc", "back"}},
	}
	sc.left = []string{
		t.B(t.Title).Render("Network"),
		t.S(t.Muted).Render("Cables with a link, and what is at the other end."),
		"",
	}
	diagram, rows := m.cableDiagram(l)
	sc.layers = append(sc.layers, diagram...)
	for range rows {
		sc.left = append(sc.left, "")
	}
	sc.left = append(sc.left, "")
	if m.form != nil {
		top := len(sc.left)
		sc.left = append(sc.left, m.formView()...)
		sc.focusFrom, sc.focusTo = top, len(sc.left)-1
		sc.help = [][2]string{{"up/down", "field"}, {"left/right", "interface"}, {"enter", "next, then test"}, {"esc", "cancel"}}
	} else {
		lines, choices := m.netReport()
		sc.left = append(sc.left, lines...)
		sc.focusFrom = -1
		if len(choices) > 0 {
			sc.left = append(sc.left, "")
			q := "What should we do?"
			sub := "We can fix it from here."
			if !m.netProblem() {
				q, sub = "Keep these settings?", "The installed system starts with them."
			}
			sc.focusFrom = len(sc.left)
			sc.left = append(sc.left, t.B(t.Title).Render(q), t.S(t.Muted).Render(sub), "")
			for i, ch := range choices {
				sc.left = append(sc.left, m.choice(ch.label, i == m.netCur))
			}
			sc.focusTo = len(sc.left) - 1
		}
	}
	sc.panel = append(m.setup(), m.netPanel()...)
	return sc
}

func (m *Model) netPanel() []string {
	t := m.t
	r := m.st.Net
	c := r.Config
	kv := func(k, v string, vc ...any) string {
		st := t.S(t.Fg)
		if len(vc) > 0 {
			st = vc[0].(lipgloss.Style)
		}
		return t.S(t.Muted).Width(10).Render(k) + st.Render(v)
	}
	out := []string{"", t.B(t.Good).Render("Network found")}
	if c.Iface != "" {
		name := c.Iface
		if c.VLAN != 0 {
			name += fmt.Sprintf(".%d", c.VLAN)
		}
		out = append(out, kv("Interface", name))
	}
	if !r.Up {
		return append(out, kv("Address", "none", t.S(t.Bad)))
	}
	mode := ""
	if c.Mode == engine.NetDHCP {
		mode = " DHCP"
	}
	out = append(out, kv("Address", c.Address.Addr().String()+t.S(t.Muted).Render(mode)))
	check := func(c engine.Check, okText string) (string, lipgloss.Style) {
		switch c {
		case engine.CheckOK:
			return okText, t.S(t.Fg)
		case engine.CheckFailed:
			return "no reply", t.S(t.Bad)
		}
		return "not set", t.S(t.Muted)
	}
	gw, gst := check(r.Gateway, c.Gateway.String())
	out = append(out, kv("Gateway", gw, gst))
	dns, dst := check(r.DNS, "answers")
	return append(out, kv("DNS", dns, dst))
}

// cableDiagram draws the machine, each NIC's cable, and the switch port behind it.
func (m *Model) cableDiagram(l layout) ([]*lipgloss.Layer, int) {
	t := m.t
	nics := m.st.Machine.NICs
	if len(nics) == 0 {
		return nil, 0
	}
	// Ports with a switch get a box; spread them so boxes never touch.
	var order []engine.NIC
	for _, n := range nics {
		if n.Neighbor != nil && n.Carrier {
			order = append(order, n)
		}
	}
	var rest []engine.NIC
	for _, n := range nics {
		if n.Neighbor == nil || !n.Carrier {
			rest = append(rest, n)
		}
	}
	if len(order) > 1 && len(rest) > 0 {
		order = append([]engine.NIC{order[0]}, append(rest, order[1:]...)...)
	} else {
		order = append(order, rest...)
	}
	if len(order) > 5 {
		order = order[:5]
	}
	top := 3 + 3 // below the title rows
	x0 := l.leftX - 2
	var layers []*lipgloss.Layer
	y := top + 1
	prevBoxed := false
	var ys []int
	for i, n := range order {
		boxed := n.Neighbor != nil && n.Carrier
		if i > 0 {
			if boxed && prevBoxed {
				y += 4
			} else {
				y += 2
			}
		}
		ys = append(ys, y)
		prevBoxed = boxed
	}
	bottom := ys[len(ys)-1] + 1
	layers = append(layers, lipgloss.NewLayer(m.box(14, bottom-top+1, t.Border)).X(x0).Y(top))
	mid := top + (bottom-top)/2
	layers = append(layers, lipgloss.NewLayer(t.B(t.Fg).Render("this machine")).X(x0+1).Y(mid))
	lineX := x0 + 16
	const lineW = 25
	for i, n := range order {
		y := ys[i]
		name := n.Name + " "
		if !n.Carrier {
			layers = append(layers, lipgloss.NewLayer(t.S(t.Muted).Render(n.Name+"  no cable")).X(lineX).Y(y))
			continue
		}
		label := ""
		if n.SpeedMbps > 0 {
			label = " " + speed(n.SpeedMbps) + " "
		}
		fill := lineW - len(name) - len(label)
		if n.Neighbor == nil {
			fill = lineW - len(name) - len(label) - 2
		}
		a := fill / 2
		cable := t.S(t.Good).Render(strings.Repeat(t.Cable, a)) + t.S(t.Muted).Render(label) +
			t.S(t.Good).Render(strings.Repeat(t.Cable, fill-a))
		layers = append(layers, lipgloss.NewLayer(t.S(t.Fg).Render(name)+cable).X(lineX).Y(y))
		if n.Neighbor == nil {
			layers = append(layers, lipgloss.NewLayer(t.S(t.Muted).Render("no switch info")).X(lineX+lineW).Y(y))
			continue
		}
		bx := lineX + lineW + 1
		layers = append(layers, lipgloss.NewLayer(m.box(16, 3, t.BorderDim)).X(bx).Y(y-1))
		sw := clip(n.Neighbor.Switch, 7)
		port := clip(n.Neighbor.Port, 13-len(sw))
		layers = append(layers, lipgloss.NewLayer(t.B(t.Fg).Render(sw)+t.S(t.Muted).Render(" "+port)).X(bx+2).Y(y))
	}
	return layers, bottom - top + 1
}

func clip(s string, n int) string {
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	if len(r) > n {
		return string(r[:n-1]) + "~"
	}
	return s
}

func speed(mbps int) string {
	if mbps >= 1000 && mbps%1000 == 0 {
		return fmt.Sprintf("%dG", mbps/1000)
	}
	if mbps >= 1000 {
		return fmt.Sprintf("%.1fG", float64(mbps)/1000)
	}
	return fmt.Sprintf("%dM", mbps)
}

// netForm edits the settings by hand; fields hold text until applied.
type netForm struct {
	ifaces []string
	iface  int
	fields [4]input // VLAN, address, gateway, DNS
	focus  int      // 0 is the interface, then the fields
	err    string
}

var formLabels = [...]string{"Interface", "VLAN", "Address", "Gateway", "DNS"}

func newNetForm(st engine.State, c engine.NetConfig) *netForm {
	f := &netForm{}
	for _, n := range st.Machine.NICs {
		f.ifaces = append(f.ifaces, n.Name)
		if n.Name == c.Iface {
			f.iface = len(f.ifaces) - 1
		}
	}
	if c.VLAN != 0 {
		f.fields[0] = newInput(strconv.Itoa(c.VLAN))
	}
	if c.Address.IsValid() {
		f.fields[1] = newInput(c.Address.String())
	}
	if c.Gateway.IsValid() {
		f.fields[2] = newInput(c.Gateway.String())
	}
	var dns []string
	for _, d := range c.DNS {
		dns = append(dns, d.String())
	}
	f.fields[3] = newInput(strings.Join(dns, ", "))
	if c.Iface == "" {
		// Pick the first NIC with a link.
		for i, n := range st.Machine.NICs {
			if n.Carrier {
				f.iface = i
				break
			}
		}
	}
	return f
}

func (f *netForm) config() (engine.NetConfig, string) {
	c := engine.NetConfig{Mode: engine.NetStatic}
	if len(f.ifaces) == 0 {
		return c, "No network interface was found."
	}
	c.Iface = f.ifaces[f.iface]
	if v := strings.TrimSpace(f.fields[0].value); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 4094 {
			return c, "VLAN is a number from 1 to 4094, or empty."
		}
		c.VLAN = n
	}
	a := strings.TrimSpace(f.fields[1].value)
	if !strings.Contains(a, "/") {
		a += "/24"
	}
	p, err := netip.ParsePrefix(a)
	if err != nil || !p.Addr().Is4() {
		return c, "Address looks like 10.20.120.11/24."
	}
	c.Address = p
	g, err := netip.ParseAddr(strings.TrimSpace(f.fields[2].value))
	if err != nil {
		return c, "Gateway looks like 10.20.120.1."
	}
	if !p.Masked().Contains(g) {
		return c, "The gateway is outside " + p.Masked().String() + "."
	}
	c.Gateway = g
	for _, s := range strings.FieldsFunc(f.fields[3].value, func(r rune) bool { return r == ',' || r == ' ' }) {
		d, err := netip.ParseAddr(s)
		if err != nil {
			return c, "DNS is a list of addresses, or empty."
		}
		c.DNS = append(c.DNS, d)
	}
	return c, ""
}

func (m *Model) keyForm(s string, k tea.KeyPressMsg) {
	f := m.form
	switch s {
	case "esc":
		m.form = nil
	case "up", "shift+tab":
		f.focus = max(0, f.focus-1)
	case "down", "tab":
		f.focus = min(len(formLabels)-1, f.focus+1)
	case "left":
		if f.focus == 0 && len(f.ifaces) > 0 {
			f.iface = (f.iface + len(f.ifaces) - 1) % len(f.ifaces)
		}
	case "right", "space":
		if f.focus == 0 && len(f.ifaces) > 0 {
			f.iface = (f.iface + 1) % len(f.ifaces)
		}
	case "enter":
		if f.focus < len(formLabels)-1 {
			f.focus++
			return
		}
		c, msg := f.config()
		if msg != "" {
			f.err = msg
			return
		}
		m.form = nil
		m.netCur = 0
		m.eng.TestNetwork(m.ctx, c)
	default:
		if f.focus > 0 && f.fields[f.focus-1].handle(s, k) {
			f.err = ""
		}
	}
}

func (m *Model) formView() []string {
	t := m.t
	f := m.form
	out := []string{t.B(t.Title).Render("Settings for the installed system"), t.S(t.Muted).Render("VLAN empty means untagged."), ""}
	for i, label := range formLabels {
		lab := t.S(t.Muted).Width(11).Render(label)
		var val string
		if i == 0 {
			name := "none"
			if len(f.ifaces) > 0 {
				name = f.ifaces[f.iface]
			}
			val = t.S(t.Fg).Render(name)
			if f.focus == 0 {
				val = t.S(t.Accent).Render("< ") + t.S(t.Good).Render(name) + t.S(t.Accent).Render(" >")
			} else {
				val = "  " + val
			}
		} else {
			val = m.inputView(f.fields[i-1], f.focus == i)
		}
		out = append(out, lab+val)
	}
	if f.err != "" {
		out = append(out, "", t.S(t.Bad).Render(t.Fail+" ")+t.S(t.Fg).Render(f.err))
	}
	return out
}
