package link

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func serve(t *testing.T, secret string) (string, *Server) {
	t.Helper()
	s, err := NewServer(filepath.Join(t.TempDir(), "tls.pem"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go s.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.Proven(w, r, secret) {
			http.Error(w, "wrong secret", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	return l.Addr().String(), s
}

func TestCall(t *testing.T) {
	addr, _ := serve(t, "site secret")
	var out struct{ OK bool }
	if err := Call(context.Background(), addr, "site secret", "POST", "/x", map[string]string{"a": "b"}, &out); err != nil || !out.OK {
		t.Fatalf("right secret: %v %v", err, out)
	}
	err := Call(context.Background(), addr, "guess", "GET", "/x", nil, nil)
	if e, ok := err.(*Error); !ok || e.Status != http.StatusUnauthorized || e.Msg != "wrong secret" {
		t.Fatalf("wrong secret: %v", err)
	}
}

// A server that accepts any proof but cannot make its own is refused.
func TestCallChecksTheServer(t *testing.T) {
	s, err := NewServer(filepath.Join(t.TempDir(), "tls.pem"))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go s.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Proof", "made up")
		w.Write([]byte(`{}`))
	}))
	err = Call(context.Background(), l.Addr().String(), "site secret", "GET", "/x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "does not hold") {
		t.Fatalf("got %v", err)
	}
}

// A proof made for one certificate is refused with another.
func TestProofBindsTheCertificate(t *testing.T) {
	_, a := serve(t, "s")
	_, b := serve(t, "s")
	pa := proof("s", a.cert.Certificate[0], clientSide)
	if pa == proof("s", b.cert.Certificate[0], clientSide) || pa == proof("s", a.cert.Certificate[0], serverSide) {
		t.Fatal("proofs must differ by certificate and side")
	}
}

func TestServerKeepsItsCertificate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tls.pem")
	a, _ := NewServer(p)
	b, _ := NewServer(p)
	if string(a.cert.Certificate[0]) != string(b.cert.Certificate[0]) {
		t.Fatal("a restart made a new certificate")
	}
}

func TestExpand(t *testing.T) {
	got, bad := Expand("AARD  abandoned abb zig")
	if bad != "" || strings.Join(got, " ") != "aardvark abandoned abbreviate zigzagged" {
		t.Fatalf("got %v, %q", got, bad)
	}
	if _, bad := Expand("aardvark zz"); bad != "zz" {
		t.Fatalf("unknown: %q", bad)
	}
	if len(wordList) != 1295 {
		t.Fatalf("%d words", len(wordList))
	}
	seen := map[string]bool{}
	for _, w := range wordList {
		if seen[w[:3]] {
			t.Fatalf("%s shares its first three letters", w)
		}
		seen[w[:3]] = true
	}
	w := NewWords()
	if back, bad := Expand(Secret(w)); bad != "" || Secret(back) != Secret(w) {
		t.Fatal("new words do not read back")
	}
}

func TestBroadcasts(t *testing.T) {
	for _, b := range broadcasts(nil) {
		if b.To4() == nil || b.IsLoopback() {
			t.Fatalf("bad broadcast %v", b)
		}
	}
}
