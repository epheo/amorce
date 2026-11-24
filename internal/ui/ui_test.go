package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/epheo/microshift-installer/internal/demo"
	"github.com/epheo/microshift-installer/internal/engine"
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
				engine.StepDisks, engine.StepName, engine.StepConfirm} {
				eng.ChoosePurpose("storage")
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
