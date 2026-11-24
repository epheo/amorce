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
		if err := walkSite(dir, look, w, h); err != nil {
			return err
		}
		if err := walkHome(dir, look, w, h); err != nil {
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

// demoSite answers every screen for the demo machine; its sdc holds data,
// so the run stops at Confirm unless told erase: any.
const demoSite = `site: lab
defaults:
  keyboard: fr
  network: {interface: eno1, vlan: 110, gateway: 10.20.120.1, dns: [10.20.0.53]}
machines:
  - match: {serial: CZ2041XY7J}
    name: srv-02
    purpose: %s
    address: 10.20.120.12/24
`

// walkSite shows an answered install counting down, and one a person must finish.
func walkSite(dir string, look ui.Look, w, h int) error {
	for _, c := range []struct{ name, purpose string }{{"9a-site-countdown", "kubernetes"}, {"9b-site-stopped", "storage"}} {
		ctx, cancel := context.WithCancel(context.Background())
		eng := engine.New(&demo.System{Speed: 1000, SiteFile: []byte(fmt.Sprintf(demoSite, c.purpose))})
		eng.Start(ctx)
		waitFor(eng, func(s engine.State) bool {
			return s.Site != nil && (s.Site.Countdown > 0 || s.Site.Problem != "")
		})
		m := ui.New(ctx, eng, ui.Options{Look: look, Where: "screen and serial console"})
		m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		m.Update(ui.EngineMsg{})
		err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%s.ans", c.name, look)), []byte(m.View().Content), 0o644)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// walkHome shows the installed console of a first machine, and its board.
func walkHome(dir string, look ui.Look, w, h int) error {
	m := ui.New(context.Background(), nil, ui.Options{Look: look, Home: ui.DemoHome})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("10a-home-%s.ans", look)), []byte(m.View().Content), 0o644); err != nil {
		return err
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF3})
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("10b-home-board-%s.ans", look)), []byte(m.View().Content), 0o644)
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
