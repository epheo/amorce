package ui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/epheo/amorce/internal/demo"
	"github.com/epheo/amorce/internal/engine"
	"github.com/epheo/amorce/internal/site"
)

// Every step renders within the terminal at the mockups' size and at 80x24.
func TestScreensFit(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, look := range []Look{LookStyled, LookNormal, LookTrue} {
			raid := look == LookNormal
			eng := engine.New(&demo.System{Speed: 1000, RAID: raid})
			eng.Start(context.Background())
			for i := 0; i < 200 && !eng.Snapshot().Discovered; i++ {
				time.Sleep(10 * time.Millisecond)
			}
			m := New(context.Background(), eng, Options{Look: look})
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for _, step := range []engine.Step{engine.StepHello, engine.StepNetwork, engine.StepPurpose,
				engine.StepDisks, engine.StepName, engine.StepConfirm, engine.StepName} {
				// The second Name is the names form of an image that offers them.
				if step == engine.StepName && m.shown == engine.StepConfirm {
					eng.ChoosePurpose("virtualization")
				} else {
					eng.ChoosePurpose("storage")
				}
				eng.Go(step)
				m.Update(EngineMsg{})
				out := m.View().Content
				lines := strings.Split(out, "\n")
				if strings.Contains(ansi.Strip(out), "…") {
					t.Errorf("%v %v step %d: a line was cut short", size, look, step)
				}
				if len(lines) > size[1] {
					t.Errorf("%v %v step %d: %d lines", size, look, step, len(lines))
				}
				for _, l := range lines {
					if w := lipgloss.Width(l); w > size[0] {
						t.Errorf("%v %v step %d: line %d wide: %q", size, look, step, w, ansi.Strip(l))
					}
				}
				// The side panel keeps its right border on every row.
				if size[0] == 100 && step != engine.StepConfirm {
					th := NewTheme(look)
					for y := 4; y < 16; y++ {
						row := []rune(ansi.Strip(lines[y]) + strings.Repeat(" ", 100))
						if string(row[96]) != th.Box.Right {
							t.Errorf("%v step %d: row %d lost the panel border: %q", look, step, y, string(row[60:]))
						}
					}
				}
			}
		}
	}
}

// Ready keeps its buttons and their note above the help line, whatever it lists.
func TestReadyFits(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		eng := engine.New(&demo.System{Speed: 1000})
		eng.Start(context.Background())
		for i := 0; i < 300 && (!eng.Snapshot().Discovered || eng.Snapshot().NetTesting); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		cfg := demo.DemoNetwork()
		cfg.VLAN = 110
		eng.TestNetwork(context.Background(), cfg)
		for i := 0; i < 300 && !eng.Snapshot().Net.Up; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		eng.ChooseNetwork(cfg)
		eng.ChoosePurpose("storage")
		if err := eng.Install(context.Background(), eng.Snapshot().Hostname); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 500 && eng.Snapshot().Step != engine.StepReady; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		m := New(context.Background(), eng, Options{Look: LookNormal})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(EngineMsg{})
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if !slices.ContainsFunc(lines[:m.layout().helpY], func(l string) bool {
			return strings.Contains(l, "Remove the install media, then restart.")
		}) {
			t.Errorf("%v: the note under the buttons is lost:\n%s", size, strings.Join(lines, "\n"))
		}
	}
}

// The normal look draws only what any console font has.
func TestNormalLookIsPlain(t *testing.T) {
	th := NewTheme(LookNormal)
	for _, g := range append([]string{th.Ok, th.Fail, th.Cur, th.Pend, th.Alert, th.Cable}, th.Spin...) {
		for _, r := range g {
			if r > 127 {
				t.Errorf("normal look uses %q", r)
			}
		}
	}
}

func TestDoing(t *testing.T) {
	for in, want := range map[string]string{
		"Copy the system image": "Copying the system image", "Wipe the chosen disks": "Wiping the chosen disks",
		"Check the hardware": "Checking the hardware", "Add the Secure Boot key": "Adding the Secure Boot key",
		"Apply network settings": "Applying network settings", "Hand data disks to Storage": "Handing data disks to Storage",
	} {
		if got := doing(in); got != want {
			t.Errorf("doing(%q) = %q", in, got)
		}
	}
}

func TestKeyboardSearchPutsExactFirst(t *testing.T) {
	k := newKeyboardPicker([]string{"ca-fr-dvorak", "ch-fr", "fr", "fr-bepo", "us"}, "us")
	k.filter.value = "fr"
	got := k.matches()
	if len(got) != 4 || got[0] != "fr" || got[1] != "fr-bepo" {
		t.Fatalf("got %v", got)
	}
}

// The installed console keeps the purpose's notes whole and lists the board.
func TestHomeFits(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		for _, look := range []Look{LookStyled, LookNormal, LookTrue} {
			m := New(context.Background(), nil, Options{Look: look, Home: DemoHome})
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			out := ansi.Strip(m.View().Content)
			lines := strings.Split(out, "\n")
			if len(lines) > size[1] {
				t.Errorf("%v %v: %d lines", size, look, len(lines))
			}
			for _, l := range lines {
				if w := lipgloss.Width(l); w > size[0] {
					t.Errorf("%v %v: line %d wide: %q", size, look, w, l)
				}
			}
			for _, want := range []string{"3A:4F:9C", "E9:27", "Machines in site lab", "correct-horse-staple",
				"aardvark gigabyte outhouse reunion uncle zigzagged", "F4 close words"} {
				if !strings.Contains(out, want) {
					t.Errorf("%v %v: %q missing:\n%s", size, look, want, out)
				}
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyF3})
			board := ansi.Strip(m.View().Content)
			for _, name := range []string{"store-01", "store-05", "not seen"} {
				if !strings.Contains(board, name) {
					t.Errorf("%v %v: F3 lacks %q:\n%s", size, look, name, board)
				}
			}
		}
	}
}

// Purpose lists heard first machines, and the join form fits with an answer.
func TestJoinFits(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		eng := engine.New(&demo.System{Speed: 1000})
		eng.Start(context.Background())
		for i := 0; i < 300 && len(eng.Snapshot().Clusters) == 0; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		m := New(context.Background(), eng, Options{Look: LookNormal})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		eng.Go(engine.StepPurpose)
		m.Update(EngineMsg{})
		check := func(what string, want ...string) {
			t.Helper()
			out := ansi.Strip(m.View().Content)
			lines := strings.Split(out, "\n")
			if len(lines) > size[1] || strings.Contains(out, "…") {
				t.Errorf("%v %s: does not fit:\n%s", size, what, out)
			}
			for _, w := range want {
				if !strings.Contains(out, w) {
					t.Errorf("%v %s: %q missing:\n%s", size, what, w, out)
				}
			}
		}
		check("purpose", "Join srv-01", "Join by address")
		for range 5 {
			m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		}
		for range 3 {
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		for _, r := range "aar gig out reu" {
			m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		check("join", "Join srv-01", "aardvark gigabyte outhouse reunion", "srv-01 shows its words now")
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		check("join, too few", "Type all six words.")
		for _, r := range " unc zig" {
			m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		for i := 0; i < 300 && eng.Snapshot().Step != engine.StepDisks; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		if st := eng.Snapshot(); st.Profile != "worker" || st.Hostname != "srv-03" {
			t.Errorf("%v: not joined: %q %q", size, st.Profile, st.Hostname)
		}
	}
}

type fakeSettings struct{ set site.Answers }

func (f *fakeSettings) Network() (site.Answers, string) {
	return site.Answers{Address: "10.20.120.11/24", Network: &site.Network{Interface: "eno1", VLAN: 110, Gateway: "10.20.120.1"}}, ""
}
func (f *fakeSettings) SetNetwork(a site.Answers) error { f.set = a; return nil }
func (f *fakeSettings) HasRootPassword() bool           { return false }
func (f *fakeSettings) SetRootPassword(string) error    { return nil }
func (f *fakeSettings) Restart() error                  { return nil }
func (f *fakeSettings) PowerOff() error                 { return nil }

// F2 settings fit, and the network form hands checked answers over.
func TestSettings(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		fs := &fakeSettings{}
		m := New(context.Background(), nil, Options{Look: LookNormal, Home: DemoHome, Settings: fs})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		check := func(what string, want ...string) {
			t.Helper()
			out := ansi.Strip(m.View().Content)
			if len(strings.Split(out, "\n")) > size[1] || strings.Contains(out, "…") {
				t.Errorf("%v %s: does not fit:\n%s", size, what, out)
			}
			for _, w := range want {
				if !strings.Contains(out, w) {
					t.Errorf("%v %s: %q missing:\n%s", size, what, w, out)
				}
			}
		}
		press := func(keys ...rune) {
			for _, k := range keys {
				m.Update(tea.KeyPressMsg{Code: k})
			}
		}
		typeText := func(s string) {
			for _, r := range s {
				m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
		check("home", "F2 settings")
		press(tea.KeyF2)
		check("menu", "Settings", "10.20.120.11/24, VLAN 110, gateway 10.20.120.1", "not set")
		press(tea.KeyEnter)
		check("network", "eno1", "Gateway")
		m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		typeText("10.20.120.300/24")
		press(tea.KeyEnter)
		check("bad address", "is not an address")
		m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		typeText("10.20.120.12/24")
		press(tea.KeyEnter)
		check("confirm", "restarts to use it")
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("%v: no apply", size)
		}
		m.Update(cmd())
		if fs.set.Address != "10.20.120.12/24" || fs.set.Network.VLAN != 110 || fs.set.Network.Interface != "eno1" {
			t.Errorf("%v: applied %+v %+v", size, fs.set, fs.set.Network)
		}
		check("after", "Restarting.")
		press(tea.KeyDown, tea.KeyEnter)
		typeText("short")
		press(tea.KeyEnter, tea.KeyEnter)
		check("password", "Eight characters at least.")
	}
}

// The names form needs a domain, fits both sizes at its fullest, shows what
// DNS answers, and hands checked names to the engine.
func TestNamesForm(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		ctx := context.Background()
		eng := engine.New(&demo.System{Speed: 1000})
		eng.Start(ctx)
		for i := 0; i < 200 && !eng.Snapshot().Discovered; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		cfg := demo.DemoNetwork()
		cfg.VLAN = 110
		cfg.Mode = engine.NetDHCP
		eng.TestNetwork(ctx, cfg)
		for i := 0; i < 300 && !eng.Snapshot().Net.Up; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		eng.ChooseNetwork(eng.Snapshot().Net.Config)
		eng.ChoosePurpose("virtualization")
		eng.Go(engine.StepName)
		m := New(ctx, eng, Options{Look: LookNormal})
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(EngineMsg{})
		key := func(s string) {
			for _, r := range s {
				m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
		tab := func() { m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }
		tab()
		tab()
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if f := m.names; !strings.Contains(f.err, "needs a domain") || f.focus != rowDomain {
			t.Fatalf("%v: no domain: %q, row %d", size, f.err, f.focus)
		}
		key("lab.example.com")
		tab()
		m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		tab()
		key("alice")
		tab()
		key("Alice Martin")
		tab()
		key("alice@example.com")
		for i := 0; i < 300 && eng.Snapshot().NamesChecking; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		m.Update(EngineMsg{})
		out := m.View().Content
		plain := ansi.Strip(out)
		if len(strings.Split(out, "\n")) > size[1] || strings.Contains(plain, "…") {
			t.Errorf("%v: does not fit:\n%s", size, plain)
		}
		for _, l := range strings.Split(out, "\n") {
			if lipgloss.Width(l) > size[0] {
				t.Errorf("%v: too wide: %q", size, ansi.Strip(l))
			}
		}
		for _, want := range []string{"Kanidm", "+ console.lab.example.com", "idm.lab.example.com  not found", "Alice Martin", "a new lease breaks"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%v: %q missing:\n%s", size, want, plain)
			}
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		for i := 0; i < 100 && eng.Snapshot().Step != engine.StepConfirm; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		st := eng.Snapshot()
		want := engine.Names{Domain: "lab.example.com", Identity: "kanidm",
			Admin: engine.Admin{User: "alice", Display: "Alice Martin", Email: "alice@example.com"}}
		if st.Step != engine.StepConfirm || st.Names != want {
			t.Fatalf("%v: step %d, names %+v", size, st.Step, st.Names)
		}
		m.Update(EngineMsg{})
		if c := ansi.Strip(m.View().Content); !strings.Contains(c, "Kanidm, first admin alice") || !strings.Contains(c, "idm.lab.example.com does not point here") || !strings.Contains(c, "a new lease breaks") {
			t.Errorf("%v: confirm:\n%s", size, c)
		}
	}
}
