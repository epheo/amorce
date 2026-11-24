package report

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendAndBoard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports.json")
	s := Open(path)
	srv := httptest.NewServer(Handler(s, "tok"))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	ctx := context.Background()

	if err := Send(ctx, addr, "nope", Report{Name: "srv-02", State: Installing}); err == nil {
		t.Fatal("a wrong token was accepted")
	}
	if err := Send(ctx, addr, "tok", Report{Name: "srv-02", Serial: "S2", State: Installing, Progress: 0.4,
		Expected: []string{"srv-01", "srv-02", "srv-03"}}); err != nil {
		t.Fatal(err)
	}
	// A reinstall under another name replaces the machine, matched by serial.
	if err := Send(ctx, addr, "tok", Report{Name: "srv-02", Serial: "s2", State: Running}); err != nil {
		t.Fatal(err)
	}
	b, err := Machines(ctx, addr, "tok")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range b {
		got = append(got, r.Name+"="+r.State)
	}
	if strings.Join(got, " ") != "srv-02=running srv-01= srv-03=" {
		t.Fatalf("board: %v", got)
	}
	// The board survives a restart.
	if n := len(Open(path).Board()); n != 3 {
		t.Fatalf("reopened board has %d machines", n)
	}
}
