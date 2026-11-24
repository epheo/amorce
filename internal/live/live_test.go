package live

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/epheo/microshift-installer/internal/engine"
)

func tlv(typ int, v []byte) []byte {
	return append([]byte{byte(typ<<1) | byte(len(v)>>8), byte(len(v))}, v...)
}

func TestParseLLDP(t *testing.T) {
	var f []byte
	f = append(f, tlv(1, append([]byte{4}, 0x3c, 0xec, 0xef, 0, 0, 1))...)
	f = append(f, tlv(2, append([]byte{5}, "Ethernet12"...))...)
	f = append(f, tlv(3, []byte{0, 120})...)
	f = append(f, tlv(5, []byte("tor-a"))...)
	f = append(f, tlv(127, []byte{0x00, 0x80, 0xc2, 1, 0, 110})...)
	f = append(f, tlv(0, nil)...)
	nb, ok := parseLLDP(f)
	if !ok || nb.Switch != "tor-a" || nb.Port != "Ethernet12" || nb.VLAN != 110 {
		t.Fatalf("got %+v %v", nb, ok)
	}
}

func TestParseLLDPWithoutName(t *testing.T) {
	f := append(tlv(1, []byte{4, 1, 2, 3, 4, 5, 6}), tlv(2, []byte{3, 1, 2, 3, 4, 5, 7})...)
	nb, ok := parseLLDP(f)
	if !ok || nb.Switch != "01:02:03:04:05:06" || nb.Port != "01:02:03:04:05:07" {
		t.Fatalf("got %+v", nb)
	}
	if _, ok := parseLLDP([]byte{0x02, 0xff}); ok {
		t.Fatal("a truncated TLV parsed")
	}
}

func TestKeyfilesVLAN(t *testing.T) {
	c := engine.NetConfig{Mode: engine.NetStatic, Iface: "eno1", VLAN: 110,
		Address: netip.MustParsePrefix("10.20.120.11/24"), Gateway: netip.MustParseAddr("10.20.120.1"),
		DNS: []netip.Addr{netip.MustParseAddr("10.20.0.53")}}
	k := keyfiles(c)
	if !strings.Contains(k["eno1"], "method=disabled") {
		t.Fatalf("parent carries an address:\n%s", k["eno1"])
	}
	v := k["eno1.110"]
	for _, want := range []string{"type=vlan", "id=110", "parent=eno1", "address1=10.20.120.11/24,10.20.120.1", "dns=10.20.0.53;"} {
		if !strings.Contains(v, want) {
			t.Errorf("vlan profile lacks %q:\n%s", want, v)
		}
	}
}

func TestKeyfilesDHCP(t *testing.T) {
	k := keyfiles(engine.NetConfig{Mode: engine.NetDHCP, Iface: "enp1s0"})
	if len(k) != 1 || !strings.Contains(k["enp1s0"], "method=auto") {
		t.Fatalf("got %v", k)
	}
}

func TestOCIPath(t *testing.T) {
	if got := ociPath("oci:/run/x/images/stornas-os:latest"); got != "/run/x/images/stornas-os" {
		t.Fatal(got)
	}
}

func TestHolds(t *testing.T) {
	sys := blockdev{Children: []blockdev{{FSType: "vfat", Type: "part"}, {FSType: "xfs", Type: "part"}}}
	if got := holds(sys); got != "an installed system" {
		t.Fatal(got)
	}
	if got := holds(blockdev{}); got != "" {
		t.Fatal(got)
	}
	if got := holds(blockdev{FSType: "LVM2_member"}); got != "LVM volumes" {
		t.Fatal(got)
	}
}

func TestSetKeyKeepsOtherLines(t *testing.T) {
	if got := setKey("FONT=eurlatgr\nKEYMAP=us\n", "KEYMAP", "fr"); got != "FONT=eurlatgr\nKEYMAP=fr\n" {
		t.Fatalf("%q", got)
	}
	if got := setKey("", "KEYMAP", "fr"); got != "KEYMAP=fr\n" {
		t.Fatalf("%q", got)
	}
}
