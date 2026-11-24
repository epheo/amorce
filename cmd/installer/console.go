package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
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
	stateDir := fs.String("state", "/var/lib/microshift-installer", "the first machine's board and key")
	fs.Parse(args)
	if len(ttys) == 0 {
		ttys = ttyList{"/dev/tty1"}
	}
	logf := func(s string) { log.Println(s) }
	ctx := context.Background()

	h := &home{id: live.Identity(), state: report.Running, changed: make(chan struct{}, 1)}
	if b, err := os.ReadFile(*sitePath); err == nil {
		f, err := site.Parse(b)
		if err != nil {
			logf(err.Error())
		} else {
			h.site = f
			h.answers, _ = f.Find(h.id)
		}
	}
	if f := h.site; f != nil && f.Report != nil {
		if h.answers.Profile == site.Worker && !workerJoined() {
			h.state = report.Joining
		}
		go func() {
			if h.isFirst(ctx) {
				fm, err := newFirst(f, h.answers, *sitePath, *stateDir)
				if err != nil {
					logf(err.Error())
				} else {
					h.mu.Lock()
					h.first = fm
					h.mu.Unlock()
					go fm.serve(logf)
					go fm.announce(ctx)
				}
			} else if h.state == report.Joining {
				go h.joinCluster(ctx, logf)
			}
			h.reportRunning(ctx, logf)
		}()
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
	set := &settings{h: h, root: "/", sitePath: *sitePath}
	model := func(opt ui.Options) *ui.Model {
		opt.Shell, opt.Home, opt.Words, opt.Settings = nil, h.info, h.toggleWords, set
		return ui.New(ctx, nil, opt)
	}
	go serveSSH(*stateDir, model, logf)
	serve(ctx, ttys, *lookName, string(cmdline), logf, model, nil)
	return nil
}

type home struct {
	id      site.Identity
	site    *site.File
	answers site.Answers // this machine's, when the site file names it

	mu      sync.Mutex
	first   *first // set on the first machine
	state   string // running, or joining for a worker not joined yet
	detail  string
	changed chan struct{}
	last    ui.HomeInfo
}

// isFirst says whether this is the machine the site file reports to: by its
// own entry, or by its addresses once the network is up.
func (h *home) isFirst(ctx context.Context) bool {
	host := h.site.Report.Host()
	if a := h.answers.Address; a == host || strings.HasPrefix(a, host+"/") {
		return true
	}
	if h.answers.Profile == site.Worker {
		return false
	}
	for range 30 {
		if slices.Contains(ownAddresses(), host) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

func (h *home) set(state, detail string) {
	h.mu.Lock()
	h.state, h.detail = state, detail
	h.mu.Unlock()
	select {
	case h.changed <- struct{}{}:
	default:
	}
}

// self is this machine's line on the board.
func (h *home) self() report.Report {
	name, _ := os.Hostname()
	h.mu.Lock()
	r := report.Report{Name: name, Serial: h.id.Serial, State: h.state, Detail: h.detail}
	worker := h.answers.Profile == site.Worker
	h.mu.Unlock()
	if len(h.id.MACs) > 0 {
		r.MAC = h.id.MACs[0]
	}
	if a := ownAddresses(); len(a) > 0 {
		r.Address = a[0]
	}
	if worker {
		r.Role = report.Worker
	}
	if h.site != nil {
		r.Expected = h.site.Names()
	}
	return r
}

// reportRunning puts this machine on the first machine's board: at every
// change, and every 10 minutes once a report gets through.
func (h *home) reportRunning(ctx context.Context, logf func(string)) {
	to := h.site.Report
	for {
		wait := 10 * time.Minute
		h.mu.Lock()
		fm := h.first
		h.mu.Unlock()
		if fm != nil {
			fm.store.Add(h.self())
		} else if err := report.Send(ctx, to.Address, to.Token, h.self()); err != nil {
			logf(fmt.Sprintf("report to %s: %v", to.Address, err))
			wait = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-h.changed:
		case <-time.After(wait):
		}
	}
}

func (h *home) toggleWords() {
	h.mu.Lock()
	fm := h.first
	h.mu.Unlock()
	if fm != nil {
		fm.toggleWords()
	}
}

// info is the latest gathered view; the screen never waits on a command.
func (h *home) info() ui.HomeInfo {
	h.mu.Lock()
	i, fm, state, detail, worker := h.last, h.first, h.state, h.detail, h.answers.Profile == site.Worker
	h.mu.Unlock()
	switch {
	case fm != nil:
		i.Board = fm.store.Board()
		workers := 0
		for _, r := range i.Board {
			if r.Role == report.Worker && r.State == report.Running {
				workers++
			}
		}
		i.Cluster = fmt.Sprintf("first machine, %d %s joined", workers, plural(workers, "worker", "workers"))
		i.Words, i.WordsUntil, i.WordsNote = fm.wordsShown()
		i.CanAdd = true
	case worker && state == report.Joining:
		i.Cluster, i.ClusterBusy = "worker of "+h.site.Report.Host()+", joining: "+detail, true
	case worker:
		i.Cluster = "worker of " + h.site.Report.Host()
	}
	return i
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// gather asks systemd every 2 seconds, and bootc until it answers once:
// the image changes only with a restart.
func (h *home) gather(ctx context.Context) {
	image := ""
	var asked time.Time
	for {
		name, _ := os.Hostname()
		i := ui.HomeInfo{Name: name, Address: ownAddresses(), Serial: h.id.Serial, Notes: issueNotes()}
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

// microshiftRange is MicroShift's own: 10.42 to 10.44.
func microshiftRange(ip net.IP) bool {
	ip = ip.To4()
	return ip != nil && ip[0] == 10 && ip[1] >= 42 && ip[1] <= 44
}

// ownAddresses are this machine's IPv4 addresses on its networks, without
// MicroShift's own ranges and link-local ones.
func ownAddresses() []string {
	addrs, _ := net.InterfaceAddrs()
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || microshiftRange(ip) {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}
