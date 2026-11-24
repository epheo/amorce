// Command installer is the install media's wizard and its tools.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/epheo/microshift-installer/internal/demo"
	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/ui"
)

const usage = `usage:
  installer run [--tty DEV]...      the wizard on the given consoles (install media)
  installer demo [--look L]         the wizard on a fake machine, in this terminal
  installer screens DIR [W H]       render every demo screen to DIR
  installer iso [flags]             write install media (see installer iso -h)
  installer font DIR                write the console fonts
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "demo":
		err = demoCmd(os.Args[2:])
	case "screens":
		w, h := 100, 30
		if len(os.Args) < 3 {
			err = fmt.Errorf("screens needs a directory")
			break
		}
		if len(os.Args) == 5 {
			fmt.Sscan(os.Args[3], &w)
			fmt.Sscan(os.Args[4], &h)
		}
		err = screens(os.Args[2], w, h)
	case "iso":
		err = isoCmd(os.Args[2:])
	case "font":
		if len(os.Args) < 3 {
			err = fmt.Errorf("font needs a directory")
			break
		}
		err = fontCmd(os.Args[2])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "installer:", err)
		os.Exit(1)
	}
}

func parseLook(s string) (ui.Look, error) {
	switch s {
	case "styled":
		return ui.LookStyled, nil
	case "normal":
		return ui.LookNormal, nil
	case "true", "":
		return ui.LookTrue, nil
	}
	return 0, fmt.Errorf("unknown look %q", s)
}

func demoCmd(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	lookName := fs.String("look", "true", "styled, normal or true")
	fs.Parse(args)
	look, err := parseLook(*lookName)
	if err != nil {
		return err
	}
	ctx := context.Background()
	eng := engine.New(&demo.System{Speed: 4})
	eng.Start(ctx)
	opt := ui.Options{Look: look, Where: "demo", Shell: func() tea.ExecCommand {
		return &ui.Shell{Cmd: exec.Command(os.Getenv("SHELL"))}
	}}
	popts := []tea.ProgramOption{}
	if look != ui.LookTrue {
		popts = append(popts, tea.WithColorProfile(colorprofile.ANSI))
	}
	p := tea.NewProgram(ui.New(ctx, eng, opt), popts...)
	ch, stop := eng.Subscribe()
	defer stop()
	go func() {
		for range ch {
			p.Send(ui.EngineMsg{})
		}
	}()
	_, err = p.Run()
	return err
}
