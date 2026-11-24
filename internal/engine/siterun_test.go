package engine_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/epheo/microshift-installer/internal/demo"
	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/link"
	"github.com/epheo/microshift-installer/internal/report"
	"github.com/epheo/microshift-installer/internal/site"
)

// firstMachine serves a board the way the first machine's console does.
func firstMachine(t *testing.T, token string) (*report.Store, string) {
	t.Helper()
	store := report.Open("")
	ls, err := link.NewServer(filepath.Join(t.TempDir(), "tls.pem"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	mux := http.NewServeMux()
	report.Handle(mux, store, func(w http.ResponseWriter, r *http.Request) bool { return ls.Proven(w, r, token) })
	go ls.Serve(l, mux)
	return store, l.Addr().String()
}

func TestMain(m *testing.M) {
	engine.SiteCountdown = 1
	os.Exit(m.Run())
}

// run boots the demo machine with a site file and waits for want.
func run(t *testing.T, siteFile string, want func(engine.State) bool) engine.State {
	t.Helper()
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := engine.New(&demo.System{Speed: 1000, SiteFile: []byte(siteFile)})
	e.Start(ctx)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st := e.Snapshot(); want(st) {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := e.Snapshot()
	t.Fatalf("step %d, site %+v", st.Step, st.Site)
	return st
}

const header = `
site: lab
defaults:
  keyboard: fr
  network: {interface: eno1, vlan: 110, gateway: 10.20.120.1, dns: [10.20.0.53]}
machines:
  - match: {serial: CZ2041XY7J}
`

func TestSiteInstallsUnattended(t *testing.T) {
	st := run(t, header+"    name: k8s-01\n    purpose: kubernetes\n    address: 10.20.120.11/24\n",
		func(s engine.State) bool { return s.Step == engine.StepReady })
	if st.Hostname != "k8s-01" || st.Keyboard != "fr" || st.Net.Config.VLAN != 110 || st.Site.Stopped {
		t.Fatalf("%+v %+v", st.Site, st.Net.Config)
	}
}

func TestSiteAsksForWhatIsMissing(t *testing.T) {
	st := run(t, header+"    purpose: kubernetes\n    address: 10.20.120.11/24\n",
		func(s engine.State) bool { return s.Site != nil && s.Site.Problem != "" })
	if st.Step != engine.StepName || !strings.Contains(st.Site.Problem, "no name") {
		t.Fatalf("step %d: %q", st.Step, st.Site.Problem)
	}
	// Everything answered before the name stays filled in.
	if p, _ := st.Purpose(); p.ID != "kubernetes" || st.Net.Config.Address.String() != "10.20.120.11/24" {
		t.Fatalf("lost answers: %+v", st.Net.Config)
	}
}

func TestSiteStopsBeforeErasingData(t *testing.T) {
	st := run(t, header+"    name: srv-02\n    purpose: storage\n    address: 10.20.120.12/24\n",
		func(s engine.State) bool { return s.Site != nil && s.Site.Problem != "" })
	if st.Step != engine.StepConfirm || !strings.Contains(st.Site.Problem, "blank disks only") {
		t.Fatalf("step %d: %q", st.Step, st.Site.Problem)
	}
}

func TestSiteWrongNetwork(t *testing.T) {
	st := run(t, header+"    name: k8s-01\n    purpose: kubernetes\n    address: 10.20.120.11/24\n    network: {vlan: 120}\n",
		func(s engine.State) bool { return s.Site != nil && s.Site.Problem != "" })
	if st.Step != engine.StepNetwork || len(st.Net.GatewayVLANs) == 0 {
		t.Fatalf("step %d: %q %+v", st.Step, st.Site.Problem, st.Net)
	}
}

func TestSiteNamesOtherMachines(t *testing.T) {
	st := run(t, strings.Replace(header, "CZ2041XY7J", "OTHER", 1)+"    name: x\n",
		func(s engine.State) bool { return s.Site != nil && s.Net.At != (time.Time{}) })
	if st.Step != engine.StepHello || !st.Site.Stopped || !strings.Contains(st.Site.Problem, "does not name") {
		t.Fatalf("step %d: %+v", st.Step, st.Site)
	}
}

func TestSiteReportsToFirstMachine(t *testing.T) {
	store, addr := firstMachine(t, "tok")
	file := strings.Replace(header, "site: lab\n", "site: lab\nreport: {address: \""+addr+"\", token: tok}\n", 1) +
		"    name: k8s-01\n    purpose: kubernetes\n    address: 10.20.120.11/24\n" +
		"  - match: {serial: OTHER}\n    name: k8s-02\n"
	if _, err := site.Parse([]byte(file)); err != nil {
		t.Fatal(err)
	}
	run(t, file, func(s engine.State) bool { return s.Step == engine.StepReady })
	deadline := time.Now().Add(10 * time.Second)
	var board []report.Report
	for time.Now().Before(deadline) {
		board = store.Board()
		if len(board) == 2 && board[0].Name == "k8s-01" && board[0].State == report.Installed {
			if board[1].Name != "k8s-02" || board[1].State != "" {
				t.Fatalf("the unseen machine: %+v", board[1])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("board: %+v", board)
}

func TestSiteWorker(t *testing.T) {
	file := strings.Replace(header, "site: lab\n", "site: lab\nreport: {address: 10.20.120.10, token: tok}\n", 1) +
		"    name: k8s-02\n    purpose: kubernetes\n    address: 10.20.120.12/24\n    profile: worker\n"
	st := run(t, file, func(s engine.State) bool { return s.Step == engine.StepReady })
	if st.Profile != site.Worker {
		t.Fatalf("profile %q", st.Profile)
	}
}
