package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	bm "charm.land/wish/v2/bubbletea"

	"github.com/epheo/amorce/internal/ui"
)

// sshPort serves the console over SSH: the same screens, words and
// settings as tty1, for root's keys, read at every login.
const sshPort = 2222

func serveSSH(stateDir string, model func(ui.Options) *ui.Model, logf func(string)) {
	srv, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf(":%d", sshPort)),
		wish.WithHostKeyPath(filepath.Join(stateDir, "ssh_host_ed25519_key")),
		wish.WithPublicKeyAuth(func(_ ssh.Context, key ssh.PublicKey) bool { return rootKey(key) }),
		wish.WithIdleTimeout(time.Hour),
		wish.WithMiddleware(
			bm.Middleware(func(ssh.Session) (tea.Model, []tea.ProgramOption) {
				return model(ui.Options{Look: ui.LookTrue, Where: "ssh"}), nil
			}),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		logf("ssh: " + err.Error())
		return
	}
	logf(fmt.Sprintf("serving the console over ssh on port %d: %v", sshPort, srv.ListenAndServe()))
}

func rootKey(key ssh.PublicKey) bool {
	b, _ := os.ReadFile("/root/.ssh/authorized_keys")
	for len(b) > 0 {
		k, _, _, rest, err := ssh.ParseAuthorizedKey(b)
		if err != nil {
			return false
		}
		if ssh.KeysEqual(k, key) {
			return true
		}
		b = rest
	}
	return false
}
