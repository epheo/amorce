package ui

import (
	"net/netip"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/link"
	"github.com/epheo/microshift-installer/internal/site"
)

// purposeItem is one choice on Purpose: a new cluster of a purpose, or
// joining a first machine's cluster as a worker.
type purposeItem struct {
	purpose   *engine.Purpose
	cluster   *engine.Cluster
	byAddress bool
}

func (m *Model) purposeItems() []purposeItem {
	st := m.st
	var out []purposeItem
	for i := range st.Purposes {
		out = append(out, purposeItem{purpose: &st.Purposes[i]})
	}
	cs := slices.Clone(st.Clusters)
	if r := st.ReportTo; r != nil && !slices.ContainsFunc(cs, func(c engine.Cluster) bool { return c.Address == r.Host() }) {
		cs = append(cs, engine.Cluster{Address: r.Host()})
	}
	for i := range cs {
		if a := st.Net.Config.Address; a.IsValid() && a.Addr().String() == cs[i].Address {
			continue
		}
		out = append(out, purposeItem{cluster: &cs[i]})
	}
	return append(out, purposeItem{byAddress: true})
}

// purposeCursor is where the cursor starts: on what is chosen.
func (m *Model) purposeCursor() int {
	for i, it := range m.purposeItems() {
		switch {
		case m.st.Profile == site.Worker && it.cluster != nil && m.st.Knows(it.cluster.Address):
			return i
		case m.st.Profile != site.Worker && it.purpose != nil && it.purpose.ID == m.st.PurposeID:
			return i
		}
	}
	return 0
}

func (m *Model) keyPurpose(s string, k tea.KeyPressMsg) {
	if m.join != nil {
		m.keyJoin(s, k)
		return
	}
	items := m.purposeItems()
	switch s {
	case "esc":
		m.eng.Go(engine.StepNetwork)
	case "up":
		m.purposeCur = max(0, m.purposeCur-1)
	case "down":
		m.purposeCur = min(len(items)-1, m.purposeCur+1)
	case "enter":
		if m.purposeCur >= len(items) {
			return
		}
		switch it := items[m.purposeCur]; {
		case it.purpose != nil:
			m.eng.ChoosePurpose(it.purpose.ID)
			m.eng.Go(engine.StepDisks)
		case it.cluster != nil && m.st.Knows(it.cluster.Address):
			m.eng.Join(m.ctx, *it.cluster, nil)
		case it.cluster != nil:
			m.join = &joinForm{cluster: *it.cluster, field: 1}
		default:
			m.join = &joinForm{byAddress: true}
		}
	}
}

func (m *Model) viewPurpose() screen {
	if m.join != nil {
		return m.viewJoin()
	}
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
	items := m.purposeItems()
	cur := min(m.purposeCur, len(items)-1)
	bar := t.S(t.Mauve).Render(t.List + " ")
	pick := func(on bool, label string, width int) string {
		if on {
			return bar + t.B(t.Accent).Width(width).Render(label)
		}
		return "  " + t.S(t.Fg).Width(width).Render(label)
	}
	joinHead := false
	for i, it := range items {
		on := i == cur
		if it.purpose == nil && !joinHead {
			joinHead = true
			left = append(left, t.B(t.Title).Render("Or join a cluster as a worker"))
		}
		switch {
		case it.purpose != nil:
			p := it.purpose
			badge := ""
			if p.ID == best.ID && len(st.Purposes) > 1 {
				badge = "  " + t.Pill(t.Good, " BEST FIT ")
			}
			summary := "  " + t.S(t.Muted).Render(p.Summary)
			if on {
				summary = bar + t.S(t.Mauve).Render(p.Summary)
			}
			left = append(left, pick(on, p.Name, 0)+badge, summary, "")
		case it.cluster != nil:
			c := it.cluster
			label := "Join " + c.Address
			var about []string
			if c.Name != "" {
				label = "Join " + c.Name
				about = append(about, c.Address)
			}
			if p, ok := m.purposeByID(c.Purpose); ok {
				about = append(about, p.Name)
			}
			if st.Knows(c.Address) {
				about = append(about, "from the site file")
			} else if c.Site != "" {
				about = append(about, "site "+c.Site)
			}
			left = append(left, pick(on, label, 22)+t.S(t.Muted).Render(strings.Join(about, ", ")))
		default:
			left = append(left, pick(on, "Join by address", 0))
		}
	}
	if why := engine.BestFitReason(st.Machine, best); why != "" && len(st.Purposes) > 1 {
		left = append(left, "", t.S(t.Muted).Render(why))
	}
	panel := m.setup()
	switch it := items[cur]; {
	case it.purpose != nil:
		if w := m.secureBootWarning(*it.purpose); w != nil {
			left = append(append(left, ""), w...)
		}
		panel = append(panel, "", t.B(t.Good).Render(it.purpose.Name+" includes"))
		for _, inc := range it.purpose.Includes {
			panel = append(panel, t.S(t.Fg).Render(inc))
		}
	default:
		panel = append(panel, "", t.B(t.Good).Render("A worker"),
			t.S(t.Fg).Render("runs what the first machine"),
			t.S(t.Fg).Render("runs, in its cluster; it"),
			t.S(t.Fg).Render("joins at its first start."))
	}
	return screen{
		left: left, focusFrom: -1, panel: panel,
		help: [][2]string{{"up/down", "choose"}, {"enter", "continue"}, {"esc", "back"}},
		pill: "Purpose", pillColor: t.Border, stepNo: 3, right: m.statusRight(),
	}
}

func (m *Model) purposeByID(id string) (engine.Purpose, bool) {
	for _, p := range m.st.Purposes {
		if p.ID == id {
			return p, true
		}
	}
	return engine.Purpose{}, false
}

// joinForm asks for the words on the first machine's screen, and for its
// address when it was not heard on the network.
type joinForm struct {
	cluster   engine.Cluster
	byAddress bool
	address   input
	words     input
	field     int // 0 address, 1 words
	note      string
}

const addressHint = "Type the first machine's address, like 10.20.120.11."

func (m *Model) keyJoin(s string, k tea.KeyPressMsg) {
	j := m.join
	if js := m.st.Join; js != nil && js.Busy {
		return
	}
	switch s {
	case "esc":
		m.join = nil
		m.eng.CancelJoin()
		return
	case "tab", "shift+tab", "up", "down":
		if j.byAddress {
			j.field = 1 - j.field
		}
		return
	case "enter":
		if j.byAddress {
			a, err := netip.ParseAddr(strings.TrimSpace(j.address.value))
			if err != nil {
				j.field, j.note = 0, addressHint
				return
			}
			j.cluster = engine.Cluster{Address: a.String()}
			if j.field == 0 {
				j.field, j.note = 1, ""
				return
			}
		}
		words, bad := link.Expand(j.words.value)
		switch {
		case bad != "":
			j.note = "\"" + bad + "\" is not one of the words."
		case len(words) != link.JoinWords:
			j.note = "Type all six words."
		default:
			j.note = ""
			m.eng.Join(m.ctx, j.cluster, words)
		}
		return
	}
	in := &j.words
	if j.byAddress && j.field == 0 {
		in = &j.address
	}
	if in.handle(strings.ToLower(s), k) {
		in.value = strings.ToLower(in.value)
		j.note = ""
	}
}

func (m *Model) viewJoin() screen {
	t := m.t
	j := m.join
	name := j.cluster.Name
	if name == "" {
		name = "the first machine"
	}
	title := "Join a cluster"
	if j.cluster.Name != "" {
		title = "Join " + j.cluster.Name
	}
	left := []string{
		t.B(t.Title).Render(title),
		t.S(t.Muted).Render("This machine becomes a worker of its cluster."),
		"",
	}
	if j.byAddress {
		left = append(left, t.B(t.Title).Render("The first machine's address"), m.inputView(j.address, j.field == 0), "")
	} else {
		about := j.cluster.Address
		if p, ok := m.purposeByID(j.cluster.Purpose); ok {
			about += " and runs " + p.Name
		}
		left = append(left, t.S(t.Fg).Render(name+" is at "+about+"."), "")
	}
	left = append(left, t.B(t.Title).Render("The six words on its screen"), m.inputView(j.words, j.field == 1))
	words, _ := link.Expand(j.words.value)
	left = append(left, "  "+t.S(t.Muted).Render(strings.Join(words, " ")), "")
	if j.cluster.Open {
		left = append(left, t.S(t.Muted).Render(name+" shows its words now."))
	} else {
		left = append(left, t.S(t.Muted).Render("Press F4 on "+name+"'s screen to show them."))
	}
	left = append(left, t.S(t.Muted).Render("Three letters of each word are enough."), "")
	switch js := m.st.Join; {
	case js != nil && js.Busy:
		left = append(left, m.spinner()+" "+t.S(t.Fg).Render("Checking the words with "+name+"..."))
	case j.note != "":
		left = append(left, t.S(t.Warn).Render(t.Alert+" ")+t.S(t.Fg).Render(j.note))
	case js != nil && js.Err != "":
		left = append(left, t.S(t.Bad).Render(t.Fail+" ")+t.S(t.Fg).Render(js.Err))
	}
	help := [][2]string{{"type", "the words"}, {"enter", "join"}, {"esc", "back"}}
	if j.byAddress {
		help = [][2]string{{"tab", "next field"}, {"enter", "join"}, {"esc", "back"}}
	}
	return screen{
		left: left, focusFrom: -1, panel: m.setup(), help: help,
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
