package link

import (
	"context"
	"encoding/json"
	"net"
	"time"
)

// Beacon is what a first machine broadcasts on its networks, so an
// installer lists it without being told its address. It is a hint only:
// joining still takes the words from its screen.
type Beacon struct {
	Site    string `json:"site,omitempty"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Purpose string `json:"purpose,omitempty"`
	// Open is set while its screen shows join words.
	Open bool `json:"open,omitempty"`
}

// Announce broadcasts the beacon now, then every interval, on every network
// with an address, until ctx ends.
func Announce(ctx context.Context, interval time.Duration, beacon func() Beacon, skip func(net.IP) bool) {
	c, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return
	}
	defer c.Close()
	for {
		if b, err := json.Marshal(beacon()); err == nil {
			for _, to := range broadcasts(skip) {
				c.WriteToUDP(b, &net.UDPAddr{IP: to, Port: Port})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// broadcasts are the broadcast addresses of this machine's networks.
func broadcasts(skip func(net.IP) bool) []net.IP {
	addrs, _ := net.InterfaceAddrs()
	var out []net.IP
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, mask := n.IP.To4(), n.Mask
		if ip == nil || len(mask) != net.IPv4len || ip.IsLoopback() || ip.IsLinkLocalUnicast() || (skip != nil && skip(ip)) {
			continue
		}
		if ones, _ := mask.Size(); ones >= 31 {
			continue
		}
		b := make(net.IP, net.IPv4len)
		for i := range b {
			b[i] = ip[i] | ^mask[i]
		}
		out = append(out, b)
	}
	return out
}

// Listen calls found for every beacon heard, until ctx ends.
func Listen(ctx context.Context, found func(Beacon)) error {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{Port: Port})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		c.Close()
	}()
	buf := make([]byte, 4096)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return ctx.Err()
		}
		var b Beacon
		if json.Unmarshal(buf[:n], &b) != nil || b.Name == "" {
			continue
		}
		if b.Address == "" {
			b.Address = from.IP.String()
		}
		found(b)
	}
}
