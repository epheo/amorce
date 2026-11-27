// Package report carries install progress from every installer to the first
// machine, one way: the first machine never sends commands back.
package report

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/epheo/amorce/internal/link"
)

// States a machine reports, in the order an install goes through them.
const (
	Waiting    = "waiting"    // the wizard needs a person; Detail says why
	Installing = "installing" // Step and Progress say how far
	Failed     = "failed"     // Detail is the error
	Installed  = "installed"  // restarting into the installed system
	Joining    = "joining"    // a worker joining the first machine's cluster; Detail says how
	Running    = "running"    // the installed system is up
)

// Worker is the role of a machine that joins the first machine's cluster.
const Worker = "worker"

type Report struct {
	Name     string    `json:"name"`
	Serial   string    `json:"serial,omitempty"`
	MAC      string    `json:"mac,omitempty"`
	Address  string    `json:"address,omitempty"`
	State    string    `json:"state"`
	Step     string    `json:"step,omitempty"`
	Progress float64   `json:"progress,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	Role     string    `json:"role,omitempty"`
	At       time.Time `json:"at"`
	// Expected names every machine of the site file the sender booted
	// with, so the board lists machines not seen yet.
	Expected []string `json:"expected,omitempty"`
}

// key identifies a machine across reinstalls and renames.
func (r Report) key() string {
	switch {
	case r.Serial != "":
		return "serial:" + strings.ToUpper(r.Serial)
	case r.MAC != "":
		return "mac:" + strings.ToLower(r.MAC)
	}
	return "name:" + r.Name
}

// Send posts r to the first machine; callers never wait on it for long.
func Send(ctx context.Context, address, token string, r Report) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return link.Call(ctx, address, token, http.MethodPost, "/v1/reports", r, nil)
}

// Machines asks the first machine for its board.
func Machines(ctx context.Context, address, token string) ([]Report, error) {
	var out []Report
	return out, link.Call(ctx, address, token, http.MethodGet, "/v1/machines", nil, &out)
}

// Admission is the first machine's answer to an installer that typed the
// words from its screen: the site file, which holds the site's token.
type Admission struct {
	Site    string   `json:"site"`
	Purpose string   `json:"purpose"`
	Names   []string `json:"names,omitempty"`
}

// Admit trades the words on the first machine's screen for its site file;
// me is the asking machine's line on its board.
func Admit(ctx context.Context, address, words string, me Report) (Admission, error) {
	var a Admission
	return a, link.Call(ctx, address, words, http.MethodPost, "/v1/admit", me, &a)
}

// Join asks the first machine for what a worker joins its cluster with: a
// MicroShift join kubeconfig, valid minutes.
func Join(ctx context.Context, address, token string, me Report) (string, error) {
	var out struct {
		Kubeconfig string `json:"kubeconfig"`
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	err := link.Call(ctx, address, token, http.MethodPost, "/v1/join", me, &out)
	return out.Kubeconfig, err
}

// Store is the first machine's board, kept on disk so a restart keeps it.
type Store struct {
	path string

	mu       sync.Mutex
	machines map[string]Report
	expected []string
	changed  chan struct{}
}

func Open(path string) *Store {
	s := &Store{path: path, machines: map[string]Report{}, changed: make(chan struct{}, 1)}
	if b, err := os.ReadFile(path); err == nil {
		var saved struct {
			Machines []Report `json:"machines"`
			Expected []string `json:"expected"`
		}
		if json.Unmarshal(b, &saved) == nil {
			for _, r := range saved.Machines {
				s.machines[r.key()] = r
			}
			s.expected = saved.Expected
		}
	}
	return s
}

// Changed receives after every report.
func (s *Store) Changed() <-chan struct{} { return s.changed }

// Add keeps the latest report per machine.
func (s *Store) Add(r Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.At.IsZero() {
		r.At = time.Now()
	}
	s.machines[r.key()] = r
	for _, n := range r.Expected {
		if !slices.Contains(s.expected, n) {
			s.expected = append(s.expected, n)
		}
	}
	s.saveLocked()
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	b, _ := json.Marshal(struct {
		Machines []Report `json:"machines"`
		Expected []string `json:"expected"`
	}{s.boardLocked(), s.expected})
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}

// Board lists every machine seen, then every expected one not seen yet,
// each group by name.
func (s *Store) Board() []Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.boardLocked()
}

func (s *Store) boardLocked() []Report {
	var out []Report
	seen := map[string]bool{}
	for _, r := range s.machines {
		out = append(out, r)
		seen[r.Name] = true
	}
	slices.SortFunc(out, func(a, b Report) int { return strings.Compare(a.Name, b.Name) })
	for _, n := range s.expected {
		if !seen[n] {
			out = append(out, Report{Name: n})
		}
	}
	return out
}

// Handle adds the store's calls to mux; proven says whether a call holds
// the site's token.
func Handle(mux *http.ServeMux, s *Store, proven func(http.ResponseWriter, *http.Request) bool) {
	mux.HandleFunc("POST /v1/reports", func(w http.ResponseWriter, r *http.Request) {
		if !proven(w, r) {
			http.Error(w, "wrong token", http.StatusUnauthorized)
			return
		}
		var rep Report
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&rep); err != nil || rep.Name == "" {
			http.Error(w, "a report needs a name", http.StatusBadRequest)
			return
		}
		rep.At = time.Now()
		s.Add(rep)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/machines", func(w http.ResponseWriter, r *http.Request) {
		if !proven(w, r) {
			http.Error(w, "wrong token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Board())
	})
}
