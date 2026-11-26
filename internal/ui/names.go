package ui

import (
	"net/netip"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/epheo/amorce/internal/engine"
)

// Rows of the names form; which show depends on the image and the sign-in.
const (
	rowName = iota
	rowDomain
	rowSignIn
	rowUser
	rowDisplay
	rowEmail
)

// namesForm is the Name step for an image that serves names: the machine's
// name, the site's domain, and who signs in first.
type namesForm struct {
	in       [6]input // by row; the sign-in row has no text
	provider bool     // sign in through the image's identity provider
	focus    int      // a row
	err      string
}

func newNamesForm(st engine.State) *namesForm {
	n := st.Names
	f := &namesForm{provider: n.Identity != ""}
	f.in[rowName] = newInput(st.Hostname)
	f.in[rowDomain] = newInput(n.Domain)
	f.in[rowUser] = newInput(n.Admin.User)
	f.in[rowDisplay] = newInput(n.Admin.Display)
	f.in[rowEmail] = newInput(n.Admin.Email)
	return f
}

func (f *namesForm) rows(p engine.Purpose) []int {
	rows := []int{rowName, rowDomain}
	if p.Identity == "" {
		return rows
	}
	rows = append(rows, rowSignIn)
	if f.provider {
		rows = append(rows, rowUser, rowDisplay, rowEmail)
	}
	return rows
}

func (f *namesForm) names(p engine.Purpose) engine.Names {
	n := engine.Names{Domain: strings.TrimSpace(f.in[rowDomain].value)}
	if f.provider && p.Identity != "" {
		n.Identity = p.Identity
		n.Admin = engine.Admin{
			User:    strings.TrimSpace(f.in[rowUser].value),
			Display: strings.TrimSpace(f.in[rowDisplay].value),
			Email:   strings.TrimSpace(f.in[rowEmail].value),
		}
	}
	return n
}

func (m *Model) keyNames(s string, k tea.KeyPressMsg) {
	f := m.names
	p, _ := m.st.Purpose()
	rows := f.rows(p)
	at := max(0, indexOf(rows, f.focus))
	move := func(to int) {
		if f.focus == rowDomain || f.focus == rowSignIn {
			m.eng.CheckDNS(m.ctx, f.names(p))
		}
		f.focus = rows[min(max(0, to), len(rows)-1)]
	}
	switch s {
	case "esc":
		m.eng.Go(engine.StepDisks)
	case "up", "shift+tab":
		move(at - 1)
	case "down", "tab":
		move(at + 1)
	case "enter":
		if at < len(rows)-1 {
			move(at + 1)
			return
		}
		name := f.in[rowName].value
		if msg := engine.CheckHostname(name); msg != "" {
			f.err, f.focus = msg, rowName
			return
		}
		if msg := m.eng.SetNames(m.ctx, f.names(p)); msg != "" {
			f.err = msg
			return
		}
		m.eng.SetHostname(name)
		m.eng.Go(engine.StepConfirm)
	case "left", "right", "space":
		if f.focus == rowSignIn {
			f.provider = !f.provider
			f.err = ""
			return
		}
		fallthrough
	default:
		if f.focus == rowSignIn {
			return
		}
		in := &f.in[f.focus]
		switch f.focus {
		case rowName, rowDomain, rowUser:
			in.handle(strings.ToLower(s), k)
			in.value = strings.ToLower(in.value)
		default:
			in.handle(s, k)
		}
		f.err = ""
	}
}

func (m *Model) viewNames() screen {
	t := m.t
	f := m.names
	p, _ := m.st.Purpose()
	provider := engine.ProviderName(p.Identity)
	label := func(s string) string { return t.S(t.Muted).Width(13).Render(s) }
	left := []string{
		t.B(t.Title).Render("Names and sign-in"),
		t.S(t.Muted).Render("How people reach this machine, and who signs in first."),
		"",
	}
	focusFrom, focusTo := -1, -1
	for _, r := range f.rows(p) {
		if r == f.focus {
			focusFrom = len(left)
		}
		switch r {
		case rowName:
			left = append(left, label("Name")+m.inputView(f.in[r], f.focus == r))
		case rowDomain:
			left = append(left, label("Domain")+m.inputView(f.in[r], f.focus == r))
			left = append(left, m.dnsLines(p, f.names(p))...)
			if engine.CheckDomain(f.names(p).Domain, p.Names) == "" {
				left = append(left, m.dhcpWarning(f.names(p).Domain)...)
			}
		case rowSignIn:
			v := "A token"
			if f.provider {
				v = provider
			}
			val := "  " + t.S(t.Fg).Render(v)
			if f.focus == r {
				val = t.S(t.Accent).Render("< ") + t.S(t.Good).Render(v) + t.S(t.Accent).Render(" >")
			}
			left = append(left, label("Sign-in")+val)
		case rowUser:
			left = append(left, label("First admin")+m.inputView(f.in[r], f.focus == r))
		case rowDisplay:
			left = append(left, label("Full name")+m.inputView(f.in[r], f.focus == r))
		case rowEmail:
			left = append(left, label("Email")+m.inputView(f.in[r], f.focus == r))
		}
		if r == f.focus {
			focusTo = len(left) - 1
		}
	}
	hints := map[int]string{
		rowName:    "Lowercase letters, digits and dashes.",
		rowDomain:  "Optional; DNS sends *.domain here. Empty: by address.",
		rowSignIn:  "A token from the console, or " + provider + " accounts.",
		rowUser:    "2 to 32 of a to z and 0 to 9, a letter first.",
		rowDisplay: "Optional.",
		rowEmail:   "Required by " + provider + " and what signs in through it.",
	}
	left = append(left, "", t.S(t.Muted).Render(hints[f.focus]))
	if f.err != "" {
		left = append(left, t.S(t.Bad).Render(t.Fail+" ")+t.S(t.Fg).Render(f.err))
	}
	help := [][2]string{{"type", "the answer"}, {"tab", "next"}, {"enter", "continue"}, {"esc", "back"}}
	if f.focus == rowSignIn {
		help[0] = [2]string{"space", "change"}
	}
	return screen{
		left: left, focusFrom: focusFrom, focusTo: focusTo, panel: m.setup(), help: help,
		pill: "Name", pillColor: t.Border, stepNo: 5, right: m.statusRight(),
	}
}

// dnsLines shows whether DNS points each name at this machine; a mismatch
// warns and never blocks: the site may set DNS later.
func (m *Model) dnsLines(p engine.Purpose, n engine.Names) []string {
	t := m.t
	st := m.st
	names := engine.CheckedNames(p, n)
	if len(names) == 0 || engine.CheckDomain(n.Domain, p.Names) != "" {
		return nil
	}
	pad := strings.Repeat(" ", 13)
	if strings.Join(names, " ") != st.NameChecksFor {
		return []string{pad + t.S(t.Muted).Render("  tab checks DNS")}
	}
	if st.NamesChecking {
		return []string{pad + m.spinner() + t.S(t.Muted).Render(" Asking DNS")}
	}
	addr, _ := netip.ParseAddr(m.address())
	var out []string
	for _, c := range st.NameChecks {
		switch {
		case c.Points(addr):
			out = append(out, pad+t.S(t.Good).Render(t.Ok+" ")+t.S(t.Fg).Render(c.Name))
		case c.Err != "":
			out = append(out, pad+t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(c.Name)+t.S(t.Muted).Render("  "+c.Err))
		default:
			var a []string
			for _, x := range c.Addrs {
				a = append(a, x.String())
			}
			out = append(out, pad+t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(c.Name)+
				t.S(t.Muted).Render("  is "+strings.Join(a, ", ")))
		}
	}
	return out
}

// namesSummary is the Confirm screen's answer for the domain and sign-in.
func (m *Model) namesSummary() (domain, signIn string) {
	n := m.st.Names
	domain, signIn = n.Domain, "a token from the console"
	if domain == "" {
		domain = "none: by its address"
	}
	if n.Identity != "" {
		signIn = engine.ProviderName(n.Identity) + ", first admin " + n.Admin.User
	}
	return domain, signIn
}

// dnsWarnings are the Confirm screen's lines for names DNS does not point here.
func (m *Model) dnsWarnings(p engine.Purpose) []string {
	t := m.t
	st := m.st
	if strings.Join(engine.CheckedNames(p, st.Names), " ") != st.NameChecksFor || st.NamesChecking {
		return nil
	}
	addr, _ := netip.ParseAddr(m.address())
	var out []string
	for _, c := range st.NameChecks {
		if !c.Points(addr) {
			out = append(out, t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(c.Name+" does not point here yet."))
		}
	}
	return out
}

// dhcpWarning: names point at an address a new lease can move.
func (m *Model) dhcpWarning(domain string) []string {
	if domain == "" || m.st.Net.Config.Mode != engine.NetDHCP {
		return nil
	}
	t := m.t
	return []string{t.S(t.Warn).Render(t.Alert+" ") + t.S(t.Fg).Render("A DHCP address: a new lease breaks these names.")}
}

func indexOf(rows []int, r int) int {
	for i, x := range rows {
		if x == r {
			return i
		}
	}
	return -1
}
