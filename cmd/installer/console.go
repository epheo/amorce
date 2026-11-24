package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/epheo/microshift-installer/internal/console"
	"github.com/epheo/microshift-installer/internal/live"
	"github.com/epheo/microshift-installer/internal/report"
	"github.com/epheo/microshift-installer/internal/site"
	"github.com/epheo/microshift-installer/internal/ui"
)

// consoleCmd is the installed system's console: what this machine is, what
// its purpose says, and, on the first machine, every machine of the site.
func consoleCmd(args []string) error {
	fs := flag.NewFlagSet("console", flag.ExitOnError)
	var ttys ttyList
	fs.Var(&ttys, "tty", "console device, repeatable (default /dev/tty1)")
	lookName := fs.String("look", "", "force styled, normal or true")
	sitePath := fs.String("site", "/"+live.SitePath, "this machine's site file")
	statePath := fs.String("state", "/var/lib/microshift-installer/reports.json", "the first machine's board")
	fs.Parse(args)
	if len(ttys) == 0 {
		ttys = ttyList{"/dev/tty1"}
	}
	logf := func(s string) { log.Println(s) }
	ctx := context.Background()

	h := &home{serial: live.DMISerial()}
	if b, err := os.ReadFile(*sitePath); err == nil {
		f, err := site.Parse(b)
		if err != nil {
			logf(err.Error())
		} else {
			h.site = f
		}
	}
	if f := h.site; f != nil && f.Report != nil {
		host, _, err := net.SplitHostPort(f.Report.HostPort(report.Port))
		if err == nil && slices.Contains(ownAddresses(), host) {
			h.store = report.Open(*statePath)
			go func() {
				srv := &http.Server{Addr: fmt.Sprintf(":%d", report.Port), Handler: report.Handler(h.store, f.Report.Token),
					ReadHeaderTimeout: 10 * time.Second}
				logf(fmt.Sprintf("receiving reports on %s: %v", srv.Addr, srv.ListenAndServe()))
			}()
		}
		go h.reportRunning(ctx, logf)
	}

	// Kernel messages go to tty12, alt+F12, as on ESXi; the serial console
	// still gets them.
	if f, err := os.OpenFile("/dev/tty1", os.O_RDWR|syscall.O_NOCTTY, 0); err == nil {
		if err := console.RedirectKernelMessages(f, 12); err != nil {
			logf(fmt.Sprintf("kernel messages stay on tty1: %v", err))
		}
		f.Close()
	}
	go h.gather(ctx)

	cmdline, _ := os.ReadFile("/proc/cmdline")
	model := func(opt ui.Options) *ui.Model {
		opt.Shell, opt.Home = nil, h.info
		return ui.New(ctx, nil, opt)
	}
	serve(ctx, ttys, *lookName, string(cmdline), logf, model, nil)
	return nil
}

type home struct {
	serial string
	site   *site.File
	store  *report.Store // the first machine's board, nil elsewhere

	mu   sync.Mutex
	last ui.HomeInfo
}

// self is this machine's line on the board.
func (h *home) self() report.Report {
	name, _ := os.Hostname()
	r := report.Report{Name: name, Serial: h.serial, State: report.Running}
	if a := ownAddresses(); len(a) > 0 {
		r.Address = a[0]
	}
	if h.site != nil {
		r.Expected = h.site.Names()
	}
	return r
}

// reportRunning says this machine is up, on the first machine's board or
// to the first machine: once it gets through, then every 10 minutes.
func (h *home) reportRunning(ctx context.Context, logf func(string)) {
	to := h.site.Report
	for {
		wait := 10 * time.Minute
		if h.store != nil {
			h.store.Add(h.self())
		} else if err := report.Send(ctx, to.HostPort(report.Port), to.Token, h.self()); err != nil {
			logf(fmt.Sprintf("report to %s: %v", to.Address, err))
			wait = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// info is the latest gathered view; the screen never waits on a command.
func (h *home) info() ui.HomeInfo {
	h.mu.Lock()
	i := h.last
	h.mu.Unlock()
	if h.store != nil {
		i.Board = h.store.Board()
	}
	return i
}

// gather asks systemd every 2 seconds, and bootc until it answers once:
// the image changes only with a restart.
func (h *home) gather(ctx context.Context) {
	image := ""
	var asked time.Time
	for {
		name, _ := os.Hostname()
		i := ui.HomeInfo{Name: name, Address: ownAddresses(), Serial: h.serial, Notes: issueNotes()}
		if h.site != nil {
			i.Site = h.site.Site
		}
		out, _ := exec.Command("systemctl", "is-system-running").Output()
		i.Health = strings.TrimSpace(string(out))
		if i.Health == "degraded" {
			out, _ := exec.Command("systemctl", "list-units", "--state=failed", "--no-legend", "--plain").Output()
			if s := strings.TrimSpace(string(out)); s != "" {
				i.Failed = strings.Count(s, "\n") + 1
			}
		}
		if image == "" && time.Since(asked) > 10*time.Minute {
			asked = time.Now()
			image = bootedImage()
		}
		i.Image = image
		h.mu.Lock()
		h.last = i
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// bootedImage is where updates come from, as bootc sees it.
func bootedImage() string {
	out, err := exec.Command("bootc", "status", "--json", "--booted").Output()
	if err != nil {
		return ""
	}
	var st struct {
		Status struct {
			Booted struct {
				Image struct {
					Image struct {
						Image string `json:"image"`
					} `json:"image"`
				} `json:"image"`
			} `json:"booted"`
		} `json:"status"`
	}
	if json.Unmarshal(out, &st) != nil {
		return ""
	}
	return st.Status.Booted.Image.Image.Image
}

// issueNotes are the login banner texts purposes leave in issue.d, which
// the console shows instead of a login prompt.
func issueNotes() []ui.Note {
	var files []string
	for _, dir := range []string{"/etc/issue.d", "/run/issue.d", "/usr/lib/issue.d"} {
		m, _ := filepath.Glob(filepath.Join(dir, "*.issue"))
		files = append(files, m...)
	}
	slices.SortFunc(files, func(a, b string) int { return strings.Compare(filepath.Base(a), filepath.Base(b)) })
	var out []ui.Note
	seen := map[string]bool{}
	for _, f := range files {
		base := filepath.Base(f)
		if seen[base] {
			continue
		}
		seen[base] = true
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var lines []string
		for _, l := range strings.Split(string(b), "\n") {
			// agetty escapes need a getty to expand them.
			if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, `\`) {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			continue
		}
		// 50-stornas.issue is from stornas.
		from := strings.TrimSuffix(base, ".issue")
		if i := strings.Index(from, "-"); i > 0 {
			if _, err := strconv.Atoi(from[:i]); err == nil {
				from = from[i+1:]
			}
		}
		out = append(out, ui.Note{From: from, Lines: lines})
	}
	return out
}

// ownAddresses are this machine's IPv4 addresses on its networks, without
// MicroShift's own ranges (10.42 to 10.44) and link-local ones.
func ownAddresses() []string {
	addrs, _ := net.InterfaceAddrs()
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || (ip[0] == 10 && ip[1] >= 42 && ip[1] <= 44) {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}
