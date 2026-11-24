// Package report carries install progress from every installer to the first
// machine, one way: the first machine never sends commands back.
package report

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Port is where the first machine listens; the distro opens it.
const Port = 9190

// States a machine reports, in the order an install goes through them.
const (
	Waiting    = "waiting"    // the wizard needs a person; Detail says why
	Installing = "installing" // Step and Progress say how far
	Failed     = "failed"     // Detail is the error
	Installed  = "installed"  // restarting into the installed system
	Running    = "running"    // the installed system is up
)

type Report struct {
	Name     string    `json:"name"`
	Serial   string    `json:"serial,omitempty"`
	MAC      string    `json:"mac,omitempty"`
	Address  string    `json:"address,omitempty"`
	State    string    `json:"state"`
	Step     string    `json:"step,omitempty"`
	Progress float64   `json:"progress,omitempty"`
	Detail   string    `json:"detail,omitempty"`
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
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/v1/reports", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s: %s", address, resp.Status)
	}
	return nil
}

// Machines asks the first machine for its board.
func Machines(ctx context.Context, address, token string) ([]Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/v1/machines", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", address, resp.Status)
	}
	var out []Report
	return out, json.NewDecoder(resp.Body).Decode(&out)
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

// Handler serves the store; every call needs the site's token.
func Handler(s *Store, token string) http.Handler {
	mux := http.NewServeMux()
	auth := func(r *http.Request) bool {
		got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		return token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
	}
	mux.HandleFunc("POST /v1/reports", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
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
		if !auth(r) {
			http.Error(w, "wrong token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Board())
	})
	return mux
}
