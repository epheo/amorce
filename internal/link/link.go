// Package link is how a site's machines reach its first machine: TLS on
// one port, where each end proves it holds the site's secret without
// sending it.
package link

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Port is where the first machine listens; the distro's firewall opens it.
const Port = 9190

// HostPort is address with Port added when it has none.
func HostPort(address string) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(address, fmt.Sprint(Port))
}

// A proof is an HMAC of the first machine's certificate under the secret.
// A machine in the middle shows its own certificate: a proof made for it
// is worthless at the first machine, and it cannot make the first
// machine's. So no certificate has to be known in advance.
func proof(secret string, cert []byte, side string) string {
	sum := sha256.Sum256(cert)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(side))
	m.Write(sum[:])
	return base64.RawStdEncoding.EncodeToString(m.Sum(nil))
}

const (
	clientSide = "amorce/1 client\x00"
	serverSide = "amorce/1 server\x00"
)

// Error is a refusal, in the first machine's words.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// Call sends in as JSON to the first machine and decodes its answer into
// out, when out is not nil.
func Call(ctx context.Context, address, secret, method, path string, in, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	addr := HostPort(address)
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	dl, _ := ctx.Deadline()
	c.SetDeadline(dl)
	cert := c.(*tls.Conn).ConnectionState().PeerCertificates[0].Raw

	req, err := http.NewRequestWithContext(ctx, method, "https://"+addr+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Proof "+proof(secret, cert, clientSide))
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	if err := req.Write(c); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &Error{Status: resp.StatusCode, Msg: strings.TrimSpace(string(b))}
	}
	if !hmac.Equal([]byte(resp.Header.Get("Proof")), []byte(proof(secret, cert, serverSide))) {
		return fmt.Errorf("%s does not hold this site's secret", address)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// Server is the first machine's end. Its certificate is made once and
// kept; nobody checks it against anything but the proofs.
type Server struct {
	cert tls.Certificate
}

// NewServer loads the key and certificate kept at path, or makes them.
func NewServer(path string) (*Server, error) {
	if b, err := os.ReadFile(path); err == nil {
		if c, err := tls.X509KeyPair(b, b); err == nil {
			return &Server{cert: c}, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "amorce"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	b := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return nil, err
	}
	c, err := tls.X509KeyPair(b, b)
	return &Server{cert: c}, err
}

// Proven checks the caller's proof of secret; when it holds, the answer
// carries the first machine's own proof.
func (s *Server) Proven(w http.ResponseWriter, r *http.Request, secret string) bool {
	got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Proof ")
	cert := s.cert.Certificate[0]
	if secret == "" || !hmac.Equal([]byte(got), []byte(proof(secret, cert, clientSide))) {
		return false
	}
	w.Header().Set("Proof", proof(secret, cert, serverSide))
	return true
}

// Serve answers on l until it closes.
func (s *Server) Serve(l net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS13}))
}

// ListenAndServe answers on Port.
func (s *Server) ListenAndServe(h http.Handler) error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", Port))
	if err != nil {
		return err
	}
	return s.Serve(l, h)
}
