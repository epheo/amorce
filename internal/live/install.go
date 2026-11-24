package live

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/epheo/microshift-installer/internal/engine"
	"github.com/epheo/microshift-installer/internal/site"
)

// PlanPath is where the data disk plan lands in the installed /etc; the
// purpose reads it once at its first start.
const PlanPath = "etc/microshift-installer/data-disks.json"

// SitePath keeps this machine's answers, to install more machines alike.
const SitePath = "etc/microshift-installer/" + site.FileName

const target = "/run/install/target"

func (s *System) Install(ctx context.Context, req engine.InstallRequest, ev func(engine.InstallEvent)) error {
	step := func(i int, f func() (string, error)) error {
		ev(engine.InstallEvent{Step: i, Status: engine.Running})
		detail, err := f()
		if err != nil {
			ev(engine.InstallEvent{Step: i, Status: engine.Failed, Log: err.Error()})
			return err
		}
		st := engine.Done
		if detail != "" {
			st = engine.Skipped
		}
		ev(engine.InstallEvent{Step: i, Status: st, Detail: detail, Progress: 1})
		return nil
	}
	prev := s.Log
	s.Log = func(line string) {
		if prev != nil {
			prev(line)
		}
		ev(engine.InstallEvent{Step: -1, Log: line})
	}
	defer func() { s.Log = prev }()

	disk := func(name string) (engine.Disk, error) {
		for _, d := range req.Machine.Disks {
			if d.Name == name {
				return d, nil
			}
		}
		return engine.Disk{}, fmt.Errorf("disk %s is not in the plan's machine", name)
	}
	system, err := disk(req.Plan.System)
	if err != nil {
		return err
	}

	if err := step(engine.InstCheck, func() (string, error) {
		for _, n := range req.Plan.Erased() {
			d, err := disk(n)
			if err != nil {
				return "", err
			}
			if _, err := os.Stat(d.Path); err != nil {
				return "", fmt.Errorf("%s is gone (%s)", d.Name, d.Path)
			}
		}
		if _, err := os.Stat(ociPath(req.Purpose.Source)); err != nil {
			return "", fmt.Errorf("the system image is not readable: %v", err)
		}
		return "", nil
	}); err != nil {
		return err
	}

	if err := step(engine.InstWipe, func() (string, error) {
		for _, n := range req.Plan.Erased() {
			d, _ := disk(n)
			if err := s.wipe(ctx, d); err != nil {
				return "", err
			}
		}
		s.cmd(ctx, "udevadm", "settle")
		return "", nil
	}); err != nil {
		return err
	}

	if err := step(engine.InstCopy, func() (string, error) {
		return "", s.bootcInstall(ctx, req, system, func(p float64) {
			ev(engine.InstallEvent{Step: engine.InstCopy, Status: engine.Running, Progress: p})
		})
	}); err != nil {
		return err
	}

	deploy, unmount, err := s.mountTarget(ctx, system)
	if err != nil {
		ev(engine.InstallEvent{Step: engine.InstNetwork, Status: engine.Failed})
		return err
	}
	defer unmount()
	// The installed system keeps the install log, for whoever asks later.
	defer s.keepLog(deploy)

	if err := step(engine.InstNetwork, func() (string, error) {
		return "", s.configure(ctx, deploy, req)
	}); err != nil {
		return err
	}

	if err := step(engine.InstData, func() (string, error) {
		if len(req.Plan.Pools) == 0 {
			return "no data disks", nil
		}
		return "", s.writePlan(ctx, deploy, req)
	}); err != nil {
		return err
	}

	return step(engine.InstSecureBoot, func() (string, error) {
		if !req.Machine.SecureBoot {
			return "Secure Boot is off", nil
		}
		if req.Purpose.NeedsSecureBootOff {
			return req.Purpose.Name + " needs Secure Boot off", nil
		}
		cert := filepath.Join(deploy, "usr/lib/microshift-installer/mok.der")
		if _, err := os.Stat(cert); err != nil {
			return "the image needs no key", nil
		}
		pw, err := s.enrollMOK(ctx, cert)
		if err != nil {
			return "", err
		}
		ev(engine.InstallEvent{Step: engine.InstSecureBoot, MOK: pw})
		return "", nil
	})
}

// wipe clears every signature on a disk and its partitions, so nothing on
// it can be auto-assembled or mistaken for data later.
func (s *System) wipe(ctx context.Context, d engine.Disk) error {
	devs, _ := lsblk(ctx, "/dev/"+d.Name)
	var parts []string
	var walk func(b blockdev)
	walk = func(b blockdev) {
		for _, c := range b.Children {
			walk(c)
			if c.Type == "part" {
				parts = append(parts, "/dev/"+c.Name)
			}
		}
	}
	for _, b := range devs {
		walk(b)
	}
	for _, p := range parts {
		s.cmd(ctx, "wipefs", "-a", p)
	}
	_, err := s.cmd(ctx, "wipefs", "-a", d.Path)
	return err
}

// bootcInstall writes the image; progress is bytes read from the media
// against the image's size, since bootc prints no progress to a pipe.
func (s *System) bootcInstall(ctx context.Context, req engine.InstallRequest, system engine.Disk, progress func(float64)) error {
	args := []string{"install", "to-disk", "--wipe",
		"--source-imgref", req.Purpose.Source, "--target-imgref", req.Purpose.Target}
	for _, k := range consoleArgs() {
		args = append(args, "--karg", k)
	}
	if len(req.SSHKeys) > 0 {
		keys := "/run/install/root-authorized-keys"
		if err := os.WriteFile(keys, []byte(strings.Join(req.SSHKeys, "\n")+"\n"), 0o600); err != nil {
			return err
		}
		args = append(args, "--root-ssh-authorized-keys", keys)
	}
	args = append(args, system.Path)
	s.logf("$ bootc %s", strings.Join(args, " "))

	dev := sourceDevice(req.Purpose.Source)
	start := readBytes(dev)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if req.Purpose.Bytes > 0 && dev != "" {
					p := float64(readBytes(dev)-start) / float64(req.Purpose.Bytes)
					progress(min(p, 0.97))
				}
			}
		}
	}()
	defer close(done)

	c := exec.CommandContext(ctx, "bootc", args...)
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			s.logf("  %s", sc.Text())
		}
	}()
	err := c.Run()
	pw.Close()
	if err != nil {
		return fmt.Errorf("bootc install failed: %v", err)
	}
	return nil
}

// ociPath is the layout directory of an oci:DIR:TAG reference.
func ociPath(ref string) string {
	p := strings.TrimPrefix(ref, "oci:")
	if i := strings.LastIndex(p, ":"); i > strings.LastIndex(p, "/") {
		p = p[:i]
	}
	return p
}

// consoleArgs keeps the consoles the media was started with.
func consoleArgs() []string {
	b, _ := os.ReadFile("/proc/cmdline")
	var out []string
	for _, f := range strings.Fields(string(b)) {
		if strings.HasPrefix(f, "console=") {
			out = append(out, f)
		}
	}
	return out
}

// readBytes is how much was read from a block device since boot.
func readBytes(dev string) int64 {
	if dev == "" {
		return 0
	}
	f := strings.Fields(readSys(filepath.Join("/sys/class/block", filepath.Base(dev), "stat")))
	if len(f) < 3 {
		return 0
	}
	n, _ := strconv.ParseInt(f[2], 10, 64)
	return n * 512
}

// mountTarget mounts the new root and returns its deployment directory.
func (s *System) mountTarget(ctx context.Context, system engine.Disk) (string, func(), error) {
	s.cmd(ctx, "udevadm", "settle")
	devs, err := lsblk(ctx, "/dev/"+system.Name)
	if err != nil || len(devs) == 0 {
		return "", nil, fmt.Errorf("cannot read the new partitions on %s", system.Name)
	}
	root := ""
	for _, c := range devs[0].Children {
		if c.Label == "root" {
			root = "/dev/" + c.Name
		}
	}
	if root == "" {
		return "", nil, fmt.Errorf("no root filesystem on %s", system.Name)
	}
	// bootc leaves the new root mounted (its container storage) and
	// finalizes it read-only; a fresh mount needs those released first.
	for _, mnt := range mountsOf(root) {
		s.cmd(ctx, "umount", "-l", mnt)
	}
	os.MkdirAll(target, 0o755)
	if _, err := s.cmd(ctx, "mount", "-o", "rw", root, target); err != nil {
		return "", nil, err
	}
	unmount := func() { s.cmd(context.Background(), "umount", target) }
	deps, _ := filepath.Glob(filepath.Join(target, "ostree/deploy/*/deploy/*.0"))
	if len(deps) != 1 {
		unmount()
		return "", nil, fmt.Errorf("expected one deployment, found %d", len(deps))
	}
	return deps[0], unmount, nil
}

// LogPath is the live log; the installed system gets a copy in /var/log.
const LogPath = "/run/install/installer.log"

func (s *System) keepLog(deploy string) {
	b, err := os.ReadFile(LogPath)
	if err != nil {
		return
	}
	// The stateroot's var is the installed /var.
	dst := filepath.Join(deploy, "../../var/log/microshift-installer.log")
	if err := os.WriteFile(dst, b, 0o600); err == nil {
		// Outside the deployment, so setfiles cannot map it: label it directly.
		unix.Setxattr(dst, "security.selinux", []byte("system_u:object_r:var_log_t:s0\x00"), 0)
	}
}

// mountsOf lists where dev is mounted, deepest first.
func mountsOf(dev string) []string {
	data, _ := os.ReadFile("/proc/self/mountinfo")
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		for i := 6; i+2 < len(f); i++ {
			if f[i] == "-" && f[i+2] == dev {
				out = append([]string{f[4]}, out...)
			}
		}
	}
	return out
}

// NodeConfigPath gives MicroShift the installer's answers.
const NodeConfigPath = "etc/microshift/config.d/10-installer.yaml"

// configure writes network, name and keyboard into the new /etc.
func (s *System) configure(ctx context.Context, deploy string, req engine.InstallRequest) error {
	files := map[string]string{
		"etc/hostname":      req.Hostname + "\n",
		"etc/vconsole.conf": setKey(readFile(filepath.Join(deploy, "etc/vconsole.conf")), "KEYMAP", req.Keyboard),
		NodeConfigPath:      nodeConfig(req),
	}
	for name, body := range keyfiles(req.Net) {
		files["etc/NetworkManager/system-connections/"+name+".nmconnection"] = body
	}
	var written []string
	if req.Site != nil {
		files[SitePath] = string(req.Site)
		written = append(written, filepath.Join(deploy, filepath.Dir(SitePath)))
	}
	if req.Profile == site.Worker {
		// Started before the join, MicroShift would make this machine a
		// controller; the console unmasks it when it joins.
		p := filepath.Join(deploy, "etc/systemd/system/microshift.service")
		os.Remove(p)
		if err := os.Symlink("/dev/null", p); err != nil {
			return err
		}
		written = append(written, p)
	}
	for rel, body := range files {
		p := filepath.Join(deploy, rel)
		mode := os.FileMode(0o644)
		// The site file holds the site's token.
		if strings.HasSuffix(rel, ".nmconnection") || rel == SitePath {
			mode = 0o600
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			return err
		}
		written = append(written, p)
	}
	return s.relabel(ctx, deploy, written)
}

// nodeConfig pins what MicroShift would otherwise guess: the node keeps its
// name if the hostname changes, and keeps the address workers reach.
func nodeConfig(req engine.InstallRequest) string {
	b := "# Written by the installer.\nnode:\n  hostnameOverride: " + req.Hostname + "\n"
	if req.Net.Mode == engine.NetStatic && req.Net.Address.IsValid() {
		b += "  nodeIP: " + req.Net.Address.Addr().String() + "\n"
	}
	return b
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// setKey sets KEY=value in a shell-style config, keeping every other line.
func setKey(conf, key, value string) string {
	var out []string
	found := false
	for _, line := range strings.Split(strings.TrimRight(conf, "\n"), "\n") {
		if strings.HasPrefix(line, key+"=") {
			line, found = key+"="+value, true
		}
		if line != "" || len(out) > 0 {
			out = append(out, line)
		}
	}
	if !found {
		out = append(out, key+"="+value)
	}
	return strings.Join(out, "\n") + "\n"
}

// relabel gives new files the SELinux labels the installed policy expects.
func (s *System) relabel(ctx context.Context, deploy string, paths []string) error {
	fc := filepath.Join(deploy, "etc/selinux/targeted/contexts/files/file_contexts")
	if _, err := os.Stat(fc); err != nil {
		return nil
	}
	// Check contexts against the installed policy: it knows types, such as
	// Open vSwitch's, that the live system's policy does not.
	args := []string{"-r", deploy}
	if pol, _ := filepath.Glob(filepath.Join(deploy, "etc/selinux/targeted/policy/policy.*")); len(pol) > 0 {
		args = append(args, "-c", pol[len(pol)-1])
	}
	args = append(append(args, fc), paths...)
	_, err := s.cmd(ctx, "setfiles", args...)
	return err
}

// keyfiles turns the chosen network into NetworkManager profiles.
func keyfiles(c engine.NetConfig) map[string]string {
	ip := "[ipv4]\nmethod=auto\n"
	if c.Mode == engine.NetStatic {
		ip = fmt.Sprintf("[ipv4]\nmethod=manual\naddress1=%s", c.Address)
		if c.Gateway.IsValid() {
			ip += "," + c.Gateway.String()
		}
		ip += "\n"
		if len(c.DNS) > 0 {
			var d []string
			for _, a := range c.DNS {
				d = append(d, a.String())
			}
			ip += "dns=" + strings.Join(d, ";") + ";\n"
		}
	}
	ip += "\n[ipv6]\nmethod=auto\n"
	if c.VLAN == 0 {
		return map[string]string{c.Iface: fmt.Sprintf(
			"[connection]\nid=%s\nuuid=%s\ntype=ethernet\ninterface-name=%s\n\n[ethernet]\n\n%s",
			c.Iface, uuid(), c.Iface, ip)}
	}
	vname := fmt.Sprintf("%s.%d", c.Iface, c.VLAN)
	return map[string]string{
		// The parent carries the VLAN only; without a profile it would try DHCP.
		c.Iface: fmt.Sprintf("[connection]\nid=%s\nuuid=%s\ntype=ethernet\ninterface-name=%s\n\n[ethernet]\n\n[ipv4]\nmethod=disabled\n\n[ipv6]\nmethod=disabled\n",
			c.Iface, uuid(), c.Iface),
		vname: fmt.Sprintf("[connection]\nid=%s\nuuid=%s\ntype=vlan\ninterface-name=%s\n\n[vlan]\nid=%d\nparent=%s\n\n%s",
			vname, uuid(), vname, c.VLAN, c.Iface, ip),
	}
}

func uuid() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// planFile is the contract with the purpose: pools by stable device path.
type planFile struct {
	Version int        `json:"version"`
	Pools   []planPool `json:"pools"`
}

type planPool struct {
	Name    string   `json:"name"`
	Raid    string   `json:"raid"`
	Tier    string   `json:"tier"`
	Devices []string `json:"devices"`
}

func (s *System) writePlan(ctx context.Context, deploy string, req engine.InstallRequest) error {
	paths := map[string]string{}
	for _, d := range req.Machine.Disks {
		paths[d.Name] = d.Path
	}
	pf := planFile{Version: 1}
	for _, p := range req.Plan.Pools {
		tier, _, _ := strings.Cut(p.Name, "-")
		pp := planPool{Name: p.Name, Raid: p.Raid, Tier: tier}
		for _, n := range p.Disks {
			pp.Devices = append(pp.Devices, paths[n])
		}
		pf.Pools = append(pf.Pools, pp)
	}
	b, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(deploy, PlanPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		return err
	}
	s.logf("wrote %s: %d pools", PlanPath, len(pf.Pools))
	return s.relabel(ctx, deploy, []string{filepath.Dir(p), p})
}

// enrollMOK queues our key for MokManager with a one-time password.
func (s *System) enrollMOK(ctx context.Context, cert string) (string, error) {
	n, _ := rand.Int(rand.Reader, big.NewInt(1e8))
	pw := fmt.Sprintf("%08d", n.Int64())
	hash, err := exec.CommandContext(ctx, "mokutil", "--generate-hash="+pw).Output()
	if err != nil {
		return "", fmt.Errorf("mokutil: %v", err)
	}
	hf := "/run/install/mok.hash"
	if err := os.WriteFile(hf, hash, 0o600); err != nil {
		return "", err
	}
	if _, err := s.cmd(ctx, "mokutil", "--import", cert, "--hash-file", hf); err != nil {
		return "", err
	}
	return pw[:4] + " " + pw[4:], nil
}
