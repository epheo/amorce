package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/epheo/microshift-installer/internal/console"
	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/live"
	"github.com/epheo/microshift-installer/internal/ui"
)

// fontDir holds the console fonts the image ships, by height.
const fontDir = "/usr/share/microshift-installer"

type ttyList []string

func (t *ttyList) String() string     { return strings.Join(*t, ",") }
func (t *ttyList) Set(s string) error { *t = append(*t, s); return nil }

// run starts the engine and one wizard per console; consoles share every
// answer, and each keeps its own cursor.
func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var ttys ttyList
	fs.Var(&ttys, "tty", "console device, repeatable (default: the kernel's consoles)")
	lookName := fs.String("look", "", "force styled, normal or true on every console")
	logPath := fs.String("log", live.LogPath, "log file")
	fs.Parse(args)

	os.MkdirAll(filepath.Dir(*logPath), 0o755)
	lf, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	logger := log.New(io.MultiWriter(lf, os.Stderr), "", log.LstdFlags)
	logf := func(s string) { logger.Println(s) }

	cmdline, _ := os.ReadFile("/proc/cmdline")
	if *lookName == "" {
		*lookName = argValue(string(cmdline), "inst.look")
	}
	if len(ttys) == 0 {
		ttys = kernelConsoles(string(cmdline))
	}
	console.QuietKernel()

	ctx := context.Background()
	sys := live.New(logf)
	eng := engine.New(sys)
	eng.Start(ctx)

	var wg sync.WaitGroup
	for _, dev := range ttys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if err := frontend(ctx, eng, dev, *lookName, string(cmdline), logf); err != nil {
					logf(fmt.Sprintf("%s: %v", dev, err))
				}
				time.Sleep(time.Second)
			}
		}()
	}
	wg.Wait()
	return nil
}

// kernelConsoles maps console= arguments to devices; tty0 is the screen.
func kernelConsoles(cmdline string) []string {
	var out []string
	for _, f := range strings.Fields(cmdline) {
		v, ok := strings.CutPrefix(f, "console=")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(v, ",")
		if name == "tty0" {
			name = "tty1"
		}
		out = append(out, "/dev/"+name)
	}
	if len(out) == 0 {
		out = []string{"/dev/tty1"}
	}
	return out
}

func argValue(cmdline, key string) string {
	for _, f := range strings.Fields(cmdline) {
		if v, ok := strings.CutPrefix(f, key+"="); ok {
			return v
		}
	}
	return ""
}

// glyphs are the symbols the styled look draws; our font must have them all.
var glyphs = []rune("✓×●•⣾⣽⣻⢿⡿⣟⣯⣷━┃│─╭╮╰╯├└█░")

func frontend(ctx context.Context, eng *engine.Engine, dev, lookName, cmdline string, logf func(string)) error {
	f, err := os.OpenFile(dev, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	opts := []tea.ProgramOption{tea.WithInput(f), tea.WithoutSignalHandler(), tea.WithColorProfile(colorprofile.ANSI)}
	look := ui.LookNormal
	where := "screen"
	// Only the basic cursor moves every VT220 descendant has: the renderer's
	// shortcuts for TERM=linux misplace lines on the Linux console, and a
	// serial line can end in any terminal.
	term := "TERM=vt220"
	var before, after func(io.Writer)
	if console.IsVT(f) {
		lf := &console.LinuxFile{File: f}
		if strings.Contains(cmdline, "inst.trace") {
			lf.Trace, _ = os.Create("/run/install/trace-" + filepath.Base(dev))
		}
		opts = append(opts, tea.WithOutput(lf))
		if stock, ok := styleVT(f, dev, lookName, logf); ok {
			look = ui.LookStyled
			// The shell gets the stock colors back, and the wizard ours after.
			before = func(w io.Writer) { console.SetColormap(f, stock); io.WriteString(w, "\x1b[2J\x1b[H") }
			after = func(w io.Writer) { console.SetColormap(f, console.ColormapOf(ui.Palette)) }
		}
	} else {
		opts = append(opts, tea.WithOutput(f))
		where = "serial console"
		baud := 115200
		for _, c := range strings.Fields(cmdline) {
			if v, ok := strings.CutPrefix(c, "console="+filepath.Base(dev)+","); ok {
				if n, err := strconv.Atoi(strings.TrimRight(v, "n8")); err == nil {
					baud = n
				}
			}
		}
		console.SetSpeed(f, baud)
		cols, rows, ok := console.ProbeSize(f)
		if !ok {
			cols, rows = 80, 24
		}
		console.SetSize(f, cols, rows)
		opts = append(opts, tea.WithWindowSize(cols, rows))
		logf(fmt.Sprintf("%s: %dx%d (answered: %v)", dev, cols, rows, ok))
	}
	switch lookName {
	case "true":
		look = ui.LookTrue
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	case "normal":
		look = ui.LookNormal
	}
	opts = append(opts, tea.WithEnvironment([]string{term}))
	logf(fmt.Sprintf("%s: %s look", dev, look))

	opt := ui.Options{Look: look, Where: where, Shell: func() tea.ExecCommand {
		c := exec.Command("/bin/bash", "-l")
		c.Env = append(os.Environ(), "TERM=linux", "PS1=[installer \\W]# ")
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		return &ui.Shell{Cmd: c, Before: before, After: after}
	}}
	p := tea.NewProgram(ui.New(ctx, eng, opt), opts...)
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

// styleVT loads our font and colors, then reads both back; any doubt
// leaves the console as it was, for the normal look. It returns the stock
// colors so a shell can have them back.
func styleVT(f *os.File, dev, lookName string, logf func(string)) (console.Colormap, bool) {
	stock, err := console.GetColormap(f)
	if lookName == "normal" || err != nil {
		return stock, false
	}
	font := filepath.Join(fontDir, fmt.Sprintf("console-%d.psf", console.FontFor(100, 30)))
	if err := console.LoadFont(dev, font); err != nil {
		logf(fmt.Sprintf("%s: %v", dev, err))
		return stock, false
	}
	if !console.HasGlyphs(f, glyphs) {
		logf(dev + ": the loaded font lacks symbols")
		return stock, false
	}
	if err := console.SetColormap(f, console.ColormapOf(ui.Palette)); err != nil {
		console.SetColormap(f, stock)
		logf(fmt.Sprintf("%s: %v", dev, err))
		return stock, false
	}
	io.WriteString(f, "\x1b[2J\x1b[H")
	return stock, true
}
