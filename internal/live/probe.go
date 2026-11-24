package live

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"net/netip"
	"sort"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	ethAll            = 0x0003
	ethARP            = 0x0806
	eth8021Q          = 0x8100
	tpStatusVLANValid = 0x10
)

// rawSock is a packet socket on one NIC that sees tags the NIC strips (auxdata).
type rawSock struct {
	fd  int
	ifi *net.Interface
}

func openRaw(ifname string) (*rawSock, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ethAll)))
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(ethAll), Ifindex: ifi.Index}); err != nil {
		unix.Close(fd)
		return nil, err
	}
	unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_AUXDATA, 1)
	tv := unix.Timeval{Usec: 200000}
	unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	return &rawSock{fd: fd, ifi: ifi}, nil
}

func (r *rawSock) Close() { unix.Close(r.fd) }

// recv returns one frame with its VLAN (0 untagged) and the payload after
// the Ethernet header, wherever the tag was.
func (r *rawSock) recv(buf []byte) (vlan int, etype uint16, src net.HardwareAddr, payload []byte, ok bool) {
	oob := make([]byte, 64)
	n, oobn, _, _, err := unix.Recvmsg(r.fd, buf, oob, 0)
	if err != nil || n < 14 {
		return 0, 0, nil, nil, false
	}
	f := buf[:n]
	src = net.HardwareAddr(f[6:12])
	etype = binary.BigEndian.Uint16(f[12:14])
	payload = f[14:]
	if etype == eth8021Q && len(payload) >= 4 {
		vlan = int(binary.BigEndian.Uint16(payload[0:2]) & 0x0fff)
		etype = binary.BigEndian.Uint16(payload[2:4])
		payload = payload[4:]
	}
	if msgs, err := unix.ParseSocketControlMessage(oob[:oobn]); err == nil {
		for _, m := range msgs {
			if m.Header.Level == unix.SOL_PACKET && m.Header.Type == unix.PACKET_AUXDATA &&
				len(m.Data) >= int(unsafe.Sizeof(unix.TpacketAuxdata{})) {
				aux := (*unix.TpacketAuxdata)(unsafe.Pointer(&m.Data[0]))
				if aux.Status&tpStatusVLANValid != 0 {
					vlan = int(aux.Vlan_tci & 0x0fff)
				}
			}
		}
	}
	return vlan, etype, src, payload, true
}

// sendARP broadcasts "who has target, tell src", tagged when vlan is set.
func (r *rawSock) sendARP(vlan int, src, target netip.Addr) error {
	f := make([]byte, 0, 64)
	f = append(f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	f = append(f, r.ifi.HardwareAddr...)
	if vlan != 0 {
		f = binary.BigEndian.AppendUint16(f, eth8021Q)
		f = binary.BigEndian.AppendUint16(f, uint16(vlan))
	}
	f = binary.BigEndian.AppendUint16(f, ethARP)
	f = append(f, 0, 1, 8, 0, 6, 4, 0, 1)
	f = append(f, r.ifi.HardwareAddr...)
	s4, t4 := src.As4(), target.As4()
	f = append(f, s4[:]...)
	f = append(f, 0, 0, 0, 0, 0, 0)
	f = append(f, t4[:]...)
	for len(f) < 60 {
		f = append(f, 0)
	}
	return unix.Sendto(r.fd, f, 0, &unix.SockaddrLinklayer{Ifindex: r.ifi.Index, Halen: 6,
		Addr: [8]uint8{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}})
}

// arpAsk returns who answers for target on vlan; src 0.0.0.0 probes for conflicts.
func arpAsk(ctx context.Context, ifname string, vlan int, src, target netip.Addr, wait time.Duration) (net.HardwareAddr, bool) {
	r, err := openRaw(ifname)
	if err != nil {
		return nil, false
	}
	defer r.Close()
	buf := make([]byte, 2048)
	deadline := time.Now().Add(wait)
	next := time.Time{}
	t4 := target.As4()
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if time.Now().After(next) {
			r.sendARP(vlan, src, target)
			next = time.Now().Add(400 * time.Millisecond)
		}
		v, et, _, p, ok := r.recv(buf)
		if !ok || et != ethARP || v != vlan || len(p) < 28 {
			continue
		}
		// A reply, or a request from the target itself, proves it is there.
		if binary.BigEndian.Uint16(p[6:8]) == 2 && [4]byte(p[14:18]) == t4 {
			return net.HardwareAddr(append([]byte(nil), p[8:14]...)), true
		}
	}
	return nil, false
}

// vlansSeen lists the VLAN tags on the wire for a moment.
func vlansSeen(ctx context.Context, ifname string, wait time.Duration) []int {
	r, err := openRaw(ifname)
	if err != nil {
		return nil
	}
	defer r.Close()
	seen := map[int]bool{}
	buf := make([]byte, 2048)
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if v, _, _, _, ok := r.recv(buf); ok && v != 0 {
			seen[v] = true
		}
	}
	var out []int
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// dnsAnswers is true when server replies to any query; NXDOMAIN counts.
func dnsAnswers(ctx context.Context, server netip.Addr) bool {
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.DialContext(ctx, "udp", netip.AddrPortFrom(server, 53).String())
	if err != nil {
		return false
	}
	defer c.Close()
	id := make([]byte, 2)
	rand.Read(id)
	// One question, localhost A: every resolver answers it, if only with
	// NXDOMAIN, while some never answer for the root zone.
	q := append(id, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0)
	q = append(q, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0, 0, 1, 0, 1)
	for range 2 {
		c.SetDeadline(time.Now().Add(1500 * time.Millisecond))
		if _, err := c.Write(q); err != nil {
			return false
		}
		resp := make([]byte, 512)
		n, err := c.Read(resp)
		if err == nil && n >= 2 && resp[0] == id[0] && resp[1] == id[1] {
			return true
		}
	}
	return false
}
