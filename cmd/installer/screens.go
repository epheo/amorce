package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/epheo/microshift-installer/internal/demo"
	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/ui"
)

// screens renders every screen of the demo machine to dir, one file per
// screen and look, so they can be compared with the mockups.
func screens(dir string, w, h int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, look := range []ui.Look{ui.LookStyled, ui.LookNormal, ui.LookTrue} {
		if err := walk(dir, look, w, h); err != nil {
			return err
		}
	}
	return nil
}

func walk(dir string, look ui.Look, w, h int) error {
	ctx := context.Background()
	eng := engine.New(&demo.System{Speed: 1000})
	eng.Start(ctx)
	waitFor(eng, func(s engine.State) bool { return s.Discovered && !s.NetTesting })
	m := ui.New(ctx, eng, ui.Options{Look: look, Where: "screen and serial console"})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	sync := func() { m.Update(ui.EngineMsg{}) }
	press := func(keys ...string) {
		for _, k := range keys {
			m.Update(keyMsg(k))
		}
	}
	shot := func(name string) error {
		sync()
		p := filepath.Join(dir, fmt.Sprintf("%s-%s.ans", name, look))
		return os.WriteFile(p, []byte(m.View().Content), 0o644)
	}

	if err := shot("1-hello"); err != nil {
		return err
	}
	eng.Go(engine.StepNetwork)
	sync()
	if err := shot("2a-network-nodhcp"); err != nil {
		return err
	}
	eng.TestNetwork(ctx, demo.DemoNetwork())
	waitFor(eng, func(s engine.State) bool { return !s.NetTesting })
	if err := shot("2b-network-vlan"); err != nil {
		return err
	}
	press("enter")
	waitFor(eng, func(s engine.State) bool { return !s.NetTesting && s.Net.Gateway == engine.CheckOK })
	if err := shot("2c-network-ok"); err != nil {
		return err
	}
	press("enter")
	sync()
	if err := shot("3-purpose"); err != nil {
		return err
	}
	press("enter")
	sync()
	if err := shot("4-disks"); err != nil {
		return err
	}
	press("down", "enter")
	sync()
	if err := shot("4b-disks-choose"); err != nil {
		return err
	}
	press("esc", "up", "enter")
	sync()
	press("ctrl+u")
	for _, r := range "srv-01" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if err := shot("5-name"); err != nil {
		return err
	}
	press("enter")
	sync()
	for _, r := range "srv-0" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if err := shot("6-confirm"); err != nil {
		return err
	}
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	press("enter")
	waitFor(eng, func(s engine.State) bool { return s.Install.Progress > 0.5 })
	if err := shot("7-installing"); err != nil {
		return err
	}
	waitFor(eng, func(s engine.State) bool { return s.Step == engine.StepReady })
	return shot("8-ready")
}

func waitFor(eng *engine.Engine, ok func(engine.State) bool) {
	for range 600 {
		if ok(eng.Snapshot()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "tab": tea.KeyTab, "space": tea.KeySpace,
	}
	if s == "ctrl+u" {
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: codes[s]}
}
