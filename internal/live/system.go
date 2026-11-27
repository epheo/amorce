// Package live is the engine's System on the install media: real disks,
// real network, bootc.
package live

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/epheo/amorce/internal/engine"
	"github.com/epheo/amorce/internal/site"
)

type System struct {
	// Log receives one line per command and result.
	Log func(string)

	mu        sync.Mutex
	media     []string // mounted media roots holding images/purposes.json
	neighbors map[string]engine.Neighbor
}

func New(log func(string)) *System {
	return &System{Log: log, neighbors: map[string]engine.Neighbor{}}
}

func (s *System) logf(format string, a ...any) {
	if s.Log != nil {
		s.Log(fmt.Sprintf(format, a...))
	}
}

// cmd runs a command and logs it; output is returned trimmed.
func (s *System) cmd(ctx context.Context, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	text := strings.TrimSpace(out.String())
	s.logf("$ %s %s", name, strings.Join(args, " "))
	if err != nil {
		s.logf("  %v: %s", err, firstLines(text, 5))
		return text, fmt.Errorf("%s: %v: %s", name, err, firstLines(text, 2))
	}
	return text, nil
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " / ")
}

func (s *System) Discover(ctx context.Context) (engine.Machine, error) {
	m := engine.Machine{Serial: DMISerial(), CPUs: runtime.NumCPU(), MemoryBytes: memTotal()}
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		m.UEFI = true
		m.SecureBoot = secureBoot()
	}
	disks, err := s.disks(ctx)
	if err != nil {
		return m, fmt.Errorf("could not list the disks: %w", err)
	}
	m.Disks = disks
	m.NICs = s.nics(ctx)
	return m, nil
}

// DMISerial is the machine's serial number, or "" when the firmware left a
// placeholder that many machines share.
func DMISerial() string {
	for _, f := range []string{"product_serial", "chassis_serial", "board_serial"} {
		b, _ := os.ReadFile("/sys/class/dmi/id/" + f)
		v := strings.TrimSpace(string(b))
		switch strings.ToLower(v) {
		case "", "none", "not specified", "to be filled by o.e.m.", "system serial number",
			"chassis serial number", "default string", "0123456789", "0":
			continue
		}
		return v
	}
	return ""
}

func memTotal() uint64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "MemTotal:"); ok {
			kb, _ := strconv.ParseUint(strings.Fields(v)[0], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// secureBoot reads the SecureBoot EFI variable: 4 attribute bytes, then the value.
func secureBoot() bool {
	b, err := os.ReadFile("/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c")
	return err == nil && len(b) >= 5 && b[4] == 1
}

func (s *System) Keymaps() []string {
	out, err := exec.Command("localectl", "list-keymaps", "--no-pager").Output()
	if err != nil {
		return []string{"de", "es", "fr", "gb", "it", "us"}
	}
	return strings.Fields(string(out))
}

func (s *System) SetKeymap(name string) error {
	_, err := s.cmd(context.Background(), "loadkeys", name)
	return err
}

func (s *System) Reboot() error {
	_, err := s.cmd(context.Background(), "systemctl", "reboot")
	return err
}

// LinkedNIC is the first network interface with a link: the one an
// install takes when the site file names none.
func LinkedNIC() string {
	for _, n := range physicalNICs() {
		if readSys(filepath.Join("/sys/class/net", n, "carrier")) == "1" {
			return n
		}
	}
	return ""
}

// Identity is what site files match this machine by.
func Identity() site.Identity {
	id := site.Identity{Serial: DMISerial()}
	for _, n := range physicalNICs() {
		if mac := readSys(filepath.Join("/sys/class/net", n, "address")); mac != "" {
			id.MACs = append(id.MACs, mac)
		}
	}
	return id
}
