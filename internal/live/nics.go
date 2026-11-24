package live

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/epheo/microshift-installer/internal/engine"
)

func readSys(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

// physicalNICs are Ethernet interfaces backed by a device, so no bridges or VLANs.
func physicalNICs() []string {
	entries, _ := os.ReadDir("/sys/class/net")
	var out []string
	for _, e := range entries {
		dir := filepath.Join("/sys/class/net", e.Name())
		if _, err := os.Stat(filepath.Join(dir, "device")); err != nil {
			continue
		}
		if readSys(filepath.Join(dir, "type")) != "1" {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func (s *System) nics(ctx context.Context) []engine.NIC {
	names := physicalNICs()
	for _, n := range names {
		s.cmd(ctx, "ip", "link", "set", n, "up")
	}
	// Links take a moment to come up after the port is enabled.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		up := 0
		for _, n := range names {
			if readSys("/sys/class/net/"+n+"/carrier") == "1" {
				up++
			}
		}
		if up == len(names) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	var out []engine.NIC
	for _, n := range names {
		dir := "/sys/class/net/" + n
		nic := engine.NIC{Name: n, MAC: readSys(dir + "/address"), Carrier: readSys(dir+"/carrier") == "1"}
		if sp, err := strconv.Atoi(readSys(dir + "/speed")); err == nil && sp > 0 {
			nic.SpeedMbps = sp
		}
		s.mu.Lock()
		if nb, ok := s.neighbors[n]; ok {
			nic.Neighbor = &nb
		}
		s.mu.Unlock()
		out = append(out, nic)
	}
	return out
}

// Neighbors listens for LLDP on every NIC until ctx ends; switches send
// every 30 seconds, so the first one can take that long.
func (s *System) Neighbors(ctx context.Context, found func(nic string, n engine.Neighbor)) {
	for _, name := range physicalNICs() {
		go func() {
			err := listenLLDP(ctx, name, func(n engine.Neighbor) {
				s.mu.Lock()
				s.neighbors[name] = n
				s.mu.Unlock()
				found(name, n)
			})
			if err != nil {
				s.logf("lldp %s: %v", name, err)
			}
		}()
	}
	<-ctx.Done()
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

const ethLLDP = 0x88cc

func listenLLDP(ctx context.Context, ifname string, found func(engine.Neighbor)) error {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ethLLDP)))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(ethLLDP), Ifindex: ifi.Index}); err != nil {
		return err
	}
	mreq := unix.PacketMreq{Ifindex: int32(ifi.Index), Type: unix.PACKET_MR_MULTICAST, Alen: 6,
		Address: [8]uint8{0x01, 0x80, 0xc2, 0x00, 0x00, 0x0e}}
	if err := unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &mreq); err != nil {
		return err
	}
	tv := unix.Timeval{Sec: 1}
	unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	buf := make([]byte, 2048)
	for ctx.Err() == nil {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil || n < 14 {
			continue
		}
		if nb, ok := parseLLDP(buf[14:n]); ok {
			nb.Seen = time.Now()
			found(nb)
		}
	}
	return nil
}

// parseLLDP reads the TLVs the screens show: system name, port, port VLAN.
func parseLLDP(b []byte) (engine.Neighbor, bool) {
	var nb engine.Neighbor
	var chassis, portID, portDesc string
	for len(b) >= 2 {
		typ := int(b[0] >> 1)
		l := int(b[0]&1)<<8 | int(b[1])
		b = b[2:]
		if l > len(b) {
			return nb, false
		}
		v := b[:l]
		b = b[l:]
		switch typ {
		case 0:
			b = nil
		case 1:
			chassis = idString(v)
		case 2:
			portID = idString(v)
		case 4:
			portDesc = printable(v)
		case 5:
			nb.Switch = printable(v)
		case 127:
			// IEEE 802.1 OUI, subtype 1: the port VLAN ID.
			if len(v) >= 6 && v[0] == 0x00 && v[1] == 0x80 && v[2] == 0xc2 && v[3] == 1 {
				nb.VLAN = int(binary.BigEndian.Uint16(v[4:6]))
			}
		}
	}
	if nb.Switch == "" {
		nb.Switch = chassis
	}
	nb.Port = portID
	if nb.Port == "" {
		nb.Port = portDesc
	}
	return nb, nb.Switch != "" || nb.Port != ""
}

// idString formats a chassis or port ID by its subtype; MACs as hex.
func idString(v []byte) string {
	if len(v) < 2 {
		return ""
	}
	sub, val := v[0], v[1:]
	if (sub == 4 || sub == 3) && len(val) == 6 {
		return net.HardwareAddr(val).String()
	}
	return printable(val)
}

func printable(v []byte) string {
	s := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return -1
		}
		return r
	}, string(v))
	return strings.TrimSpace(s)
}
