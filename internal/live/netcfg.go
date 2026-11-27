package live

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/epheo/amorce/internal/engine"
)

// conName is the live system's connection while the wizard runs.
const conName = "installer"

// Network waits for DHCP on any linked NIC, then tests what it got.
func (s *System) Network(ctx context.Context) engine.NetReport {
	linked := ""
	for _, n := range physicalNICs() {
		if readSys("/sys/class/net/"+n+"/carrier") == "1" {
			linked = n
			break
		}
	}
	r := engine.NetReport{Config: engine.NetConfig{Mode: engine.NetDHCP, Iface: linked}, At: time.Now()}
	if linked == "" {
		return r
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if dev, ok := s.connectedDevice(ctx); ok {
			cfg := s.readConfig(ctx, dev)
			cfg.Mode = engine.NetDHCP
			return s.test(ctx, cfg)
		}
		time.Sleep(time.Second)
	}
	return r
}

// connectedDevice is the first Ethernet or VLAN device NetworkManager has up.
func (s *System) connectedDevice(ctx context.Context) (string, bool) {
	out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "DEVICE,TYPE,STATE", "device").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, ":")
		if len(f) == 3 && (f[1] == "ethernet" || f[1] == "vlan") && f[2] == "connected" {
			return f[0], true
		}
	}
	return "", false
}

// readConfig reads what a device has now; a VLAN device names its parent.
func (s *System) readConfig(ctx context.Context, dev string) engine.NetConfig {
	cfg := engine.NetConfig{Iface: dev}
	if parent, id, ok := strings.Cut(dev, "."); ok {
		cfg.Iface = parent
		cfg.VLAN, _ = strconv.Atoi(id)
	}
	out, _ := exec.CommandContext(ctx, "nmcli", "-t", "-f", "IP4.ADDRESS,IP4.GATEWAY,IP4.DNS", "device", "show", dev).Output()
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || v == "" {
			continue
		}
		switch {
		case strings.HasPrefix(k, "IP4.ADDRESS") && !cfg.Address.IsValid():
			cfg.Address, _ = netip.ParsePrefix(v)
		case k == "IP4.GATEWAY":
			cfg.Gateway, _ = netip.ParseAddr(v)
		case strings.HasPrefix(k, "IP4.DNS"):
			if a, err := netip.ParseAddr(v); err == nil {
				cfg.DNS = append(cfg.DNS, a)
			}
		}
	}
	return cfg
}

// TestNetwork applies cfg to the live system, then tests it.
func (s *System) TestNetwork(ctx context.Context, cfg engine.NetConfig) engine.NetReport {
	if cfg.Mode == engine.NetStatic && cfg.Address.IsValid() {
		// Ask before taking the address, so a conflict names its owner.
		s.cmd(ctx, "nmcli", "-w", "10", "device", "disconnect", cfg.Iface)
		if mac, ok := arpAsk(ctx, cfg.Iface, cfg.VLAN, netip.IPv4Unspecified(), cfg.Address.Addr(), 1500*time.Millisecond); ok {
			return engine.NetReport{Config: cfg, Up: true, Conflict: mac.String(), At: time.Now()}
		}
	}
	if err := s.apply(ctx, cfg); err != nil {
		return engine.NetReport{Config: cfg, At: time.Now()}
	}
	dev := cfg.Iface
	if cfg.VLAN != 0 {
		dev = fmt.Sprintf("%s.%d", cfg.Iface, cfg.VLAN)
	}
	got := s.readConfig(ctx, dev)
	got.Mode = cfg.Mode
	if cfg.Mode == engine.NetStatic {
		got = cfg
	}
	return s.test(ctx, got)
}

func (s *System) apply(ctx context.Context, cfg engine.NetConfig) error {
	for _, c := range []string{conName, conName + "-vlan"} {
		exec.CommandContext(ctx, "nmcli", "connection", "delete", c).Run()
	}
	s.cmd(ctx, "nmcli", "-w", "10", "device", "disconnect", cfg.Iface)
	ip := ipArgs(cfg)
	if cfg.VLAN == 0 {
		args := append([]string{"connection", "add", "type", "ethernet", "con-name", conName, "ifname", cfg.Iface,
			"connection.autoconnect", "no"}, ip...)
		if _, err := s.cmd(ctx, "nmcli", args...); err != nil {
			return err
		}
		_, err := s.cmd(ctx, "nmcli", "-w", "30", "connection", "up", conName)
		return err
	}
	if _, err := s.cmd(ctx, "nmcli", "connection", "add", "type", "ethernet", "con-name", conName, "ifname", cfg.Iface,
		"connection.autoconnect", "no", "ipv4.method", "disabled", "ipv6.method", "disabled"); err != nil {
		return err
	}
	args := append([]string{"connection", "add", "type", "vlan", "con-name", conName + "-vlan",
		"dev", cfg.Iface, "id", strconv.Itoa(cfg.VLAN), "connection.autoconnect", "no"}, ip...)
	if _, err := s.cmd(ctx, "nmcli", args...); err != nil {
		return err
	}
	s.cmd(ctx, "nmcli", "-w", "10", "connection", "up", conName)
	_, err := s.cmd(ctx, "nmcli", "-w", "30", "connection", "up", conName+"-vlan")
	return err
}

func ipArgs(cfg engine.NetConfig) []string {
	if cfg.Mode == engine.NetDHCP {
		return []string{"ipv4.method", "auto", "ipv6.method", "auto"}
	}
	var dns []string
	for _, d := range cfg.DNS {
		dns = append(dns, d.String())
	}
	args := []string{"ipv4.method", "manual", "ipv4.addresses", cfg.Address.String(), "ipv6.method", "auto"}
	if cfg.Gateway.IsValid() {
		args = append(args, "ipv4.gateway", cfg.Gateway.String())
	}
	if len(dns) > 0 {
		args = append(args, "ipv4.dns", strings.Join(dns, " "))
	}
	return args
}

// test checks the gateway where it is configured, then everywhere else it
// could be: untagged, the switch's port VLAN, and tags seen on the wire.
func (s *System) test(ctx context.Context, cfg engine.NetConfig) engine.NetReport {
	r := engine.NetReport{Config: cfg, Up: cfg.Address.IsValid(), At: time.Now()}
	if !r.Up {
		return r
	}
	src := cfg.Address.Addr()
	if cfg.Gateway.IsValid() {
		if _, ok := arpAsk(ctx, cfg.Iface, cfg.VLAN, src, cfg.Gateway, 2*time.Second); ok {
			r.Gateway = engine.CheckOK
		} else {
			r.Gateway = engine.CheckFailed
			r.GatewayVLANs = s.findGateway(ctx, cfg)
		}
	}
	if len(cfg.DNS) > 0 && r.Gateway != engine.CheckFailed {
		r.DNS = engine.CheckFailed
		for _, d := range cfg.DNS {
			if dnsAnswers(ctx, d) {
				r.DNS = engine.CheckOK
				break
			}
		}
	}
	if len(cfg.NTP) > 0 && r.Gateway != engine.CheckFailed {
		r.NTP = engine.CheckFailed
		for _, n := range cfg.NTP {
			if ntpAnswers(ctx, n) {
				r.NTP = engine.CheckOK
				break
			}
		}
	}
	s.logf("network %s vlan %d: gateway %v dns %v ntp %v elsewhere %v", cfg.Address, cfg.VLAN, r.Gateway, r.DNS, r.NTP, r.GatewayVLANs)
	return r
}

func (s *System) findGateway(ctx context.Context, cfg engine.NetConfig) []int {
	cands := []int{0}
	s.mu.Lock()
	if nb, ok := s.neighbors[cfg.Iface]; ok && nb.VLAN != 0 {
		cands = append(cands, nb.VLAN)
	}
	s.mu.Unlock()
	cands = append(cands, vlansSeen(ctx, cfg.Iface, 3*time.Second)...)
	var found []int
	tried := map[int]bool{cfg.VLAN: true}
	for _, v := range cands {
		if tried[v] {
			continue
		}
		tried[v] = true
		if _, ok := arpAsk(ctx, cfg.Iface, v, cfg.Address.Addr(), cfg.Gateway, time.Second); ok {
			found = append(found, v)
		}
	}
	slices.Sort(found)
	return found
}
