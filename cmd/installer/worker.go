package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/epheo/microshift-installer/internal/report"
)

// Written by add-node: the role marker, and the kubelet's own credentials.
var workerFiles = []string{"/var/lib/microshift/multinode-worker", "/var/lib/microshift/resources/kubelet/kubeconfig"}

func workerJoined() bool {
	for _, f := range workerFiles {
		if _, err := os.Stat(f); err != nil {
			return false
		}
	}
	return true
}

// joinCluster makes this machine a worker of the first machine's cluster,
// once, with the distro's own steps. It tries again every 30 seconds until
// the first machine's MicroShift can let it in.
func (h *home) joinCluster(ctx context.Context, logf func(string)) {
	to := h.site.Report
	for !workerJoined() {
		h.set(report.Joining, "asking "+to.Host()+" to let it in")
		kc, err := report.Join(ctx, to.Address, to.Token, h.self())
		if err == nil {
			h.set(report.Joining, "starting MicroShift as a worker")
			err = addNode(ctx, kc, logf)
		}
		if err == nil {
			break
		}
		logf("join: " + err.Error())
		h.set(report.Joining, err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
	h.set(report.Running, "")
}

// addNode is the distro's worker join: the profile, then add-node, which
// starts MicroShift and waits for the node to be Ready. The installer masked
// MicroShift, so it never ran as a controller here.
func addNode(ctx context.Context, kubeconfig string, logf func(string)) error {
	for _, c := range [][]string{
		{"microshift-profile", "worker"},
		{"systemctl", "unmask", "microshift.service"},
		{"systemctl", "enable", "microshift.service"},
	} {
		if err := runLogged(ctx, logf, c[0], c[1:]...); err != nil {
			return err
		}
	}
	// The join kubeconfig can make node certificates for minutes: it lives
	// in memory, for this one command.
	dir, err := os.MkdirTemp("/run", "microshift-join-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	return runLogged(ctx, logf, "microshift", "add-node", "--worker", "--kubeconfig", path)
}

func runLogged(ctx context.Context, logf func(string), name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	logf(fmt.Sprintf("$ %s %s\n%s", name, strings.Join(args, " "), strings.TrimSpace(out.String())))
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, lastLine(out.String()))
	}
	return nil
}
