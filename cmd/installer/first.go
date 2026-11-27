package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/epheo/amorce/internal/link"
	"github.com/epheo/amorce/internal/report"
	"github.com/epheo/amorce/internal/site"
)

// Join words last an hour, or five wrong tries.
const (
	wordsLast  = time.Hour
	wordsTries = 5
)

// first is the first machine's side of the site: its board, the words that
// admit a new machine, and the join kubeconfigs workers start from.
type first struct {
	site     *site.File
	sitePath string
	name     string
	purpose  string
	store    *report.Store
	tls      *link.Server

	mu     sync.Mutex
	words  []string
	until  time.Time
	tries  int
	closed string // why the words closed early

	joining sync.Mutex
}

func newFirst(f *site.File, a site.Answers, sitePath, stateDir string) (*first, error) {
	ls, err := link.NewServer(filepath.Join(stateDir, "tls.pem"))
	if err != nil {
		return nil, fmt.Errorf("the site's key: %w", err)
	}
	name, _ := os.Hostname()
	purpose := a.Purpose
	if purpose == "" {
		purpose = f.Defaults.Purpose
	}
	return &first{site: f, sitePath: sitePath, name: name, purpose: purpose,
		store: report.Open(filepath.Join(stateDir, "reports.json")), tls: ls}, nil
}

func (fm *first) serve(logf func(string)) {
	mux := http.NewServeMux()
	token := func(w http.ResponseWriter, r *http.Request) bool { return fm.tls.Proven(w, r, fm.site.Report.Token) }
	report.Handle(mux, fm.store, token)
	mux.HandleFunc("POST /v1/admit", fm.admit)
	mux.HandleFunc("POST /v1/join", func(w http.ResponseWriter, r *http.Request) {
		if !token(w, r) {
			http.Error(w, "wrong token", http.StatusUnauthorized)
			return
		}
		fm.join(w, r, logf)
	})
	logf(fmt.Sprintf("serving the site on port %d: %v", link.Port, fm.tls.ListenAndServe(mux)))
}

func (fm *first) announce(ctx context.Context) {
	link.Announce(ctx, 5*time.Second, func() link.Beacon {
		fm.mu.Lock()
		open := fm.openLocked()
		fm.mu.Unlock()
		return link.Beacon{Site: fm.site.Site, Name: fm.name, Address: fm.site.Report.Host(), Purpose: fm.purpose, Open: open}
	}, microshiftRange)
}

func (fm *first) openLocked() bool { return fm.words != nil && time.Now().Before(fm.until) }

// toggleWords shows new words, or closes the shown ones.
func (fm *first) toggleWords() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.openLocked() {
		fm.words, fm.closed = nil, ""
		return
	}
	fm.words, fm.until, fm.tries, fm.closed = link.NewWords(), time.Now().Add(wordsLast), 0, ""
}

func (fm *first) wordsShown() ([]string, time.Time, string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if !fm.openLocked() {
		return nil, time.Time{}, fm.closed
	}
	return fm.words, fm.until, ""
}

// admit trades the words on this screen for the site file, which holds the
// site's token: the new machine reports and joins with it from then on.
func (fm *first) admit(w http.ResponseWriter, r *http.Request) {
	fm.mu.Lock()
	words, open := fm.words, fm.openLocked()
	fm.mu.Unlock()
	if !open {
		http.Error(w, "No join words are open: press F4 on "+fm.name+"'s screen.", http.StatusForbidden)
		return
	}
	if !fm.tls.Proven(w, r, link.Secret(words)) {
		fm.mu.Lock()
		if fm.tries++; fm.tries >= wordsTries {
			fm.words, fm.closed = nil, "The words closed after five wrong tries."
		}
		fm.mu.Unlock()
		http.Error(w, "wrong words", http.StatusUnauthorized)
		return
	}
	var me report.Report
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&me); err != nil || me.Name == "" {
		http.Error(w, "a machine needs a name", http.StatusBadRequest)
		return
	}
	b, err := os.ReadFile(fm.sitePath)
	if err != nil {
		http.Error(w, "no site file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	me.State, me.Detail, me.At = report.Waiting, "Admitted with the words on "+fm.name+"'s screen.", time.Time{}
	fm.store.Add(me)
	a := report.Admission{Site: string(b), Purpose: fm.purpose}
	for _, m := range fm.store.Board() {
		a.Names = append(a.Names, m.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

// join makes a join kubeconfig for a worker, valid ten minutes: MicroShift's
// shortest, and plenty since the worker uses it at once.
func (fm *first) join(w http.ResponseWriter, r *http.Request, logf func(string)) {
	var me report.Report
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&me); err != nil ||
		me.Name == "" || net.ParseIP(me.Address) == nil {
		http.Error(w, "a worker needs a name and an address", http.StatusBadRequest)
		return
	}
	if me.Name == fm.name {
		http.Error(w, me.Name+" is the first machine's name", http.StatusConflict)
		return
	}
	fm.joining.Lock()
	defer fm.joining.Unlock()
	if err := addHost("/etc/hosts", me.Address, me.Name); err != nil {
		logf("hosts: " + err.Error())
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "microshift", "worker-join-kubeconfig",
		"--address", fm.site.Report.Host(), "--duration", "10m").Output()
	if err != nil {
		msg := err.Error()
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			msg = lastLine(string(ee.Stderr))
		}
		logf("join kubeconfig for " + me.Name + ": " + msg)
		http.Error(w, "MicroShift on "+fm.name+" cannot let workers join yet: "+msg, http.StatusServiceUnavailable)
		return
	}
	logf("join kubeconfig for " + me.Name + " at " + me.Address)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"kubeconfig": string(out)})
}

// hostsTag marks the /etc/hosts lines the console keeps: the apiserver
// reaches kubelets by node name, and a site may have no DNS.
const hostsTag = "# microshift-installer worker"

func addHost(path, addr, name string) error {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if f := strings.Fields(l); strings.HasSuffix(l, hostsTag) && len(f) > 1 && (f[0] == addr || f[1] == name) {
			continue
		}
		if l != "" || len(out) > 0 {
			out = append(out, l)
		}
	}
	out = append(out, addr+" "+name+" "+hostsTag)
	// Rewritten in place, so the file keeps its SELinux label.
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
