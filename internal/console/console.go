// Package console prepares a Linux console or serial line for the wizard:
// palette, font, speed and size.
package console

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux console ioctls (linux/kd.h).
const (
	gioCmap   = 0x4B70
	pioCmap   = 0x4B71
	gioUnimap = 0x4B66
	kdGetMode = 0x4B3B
)

// IsVT tells a virtual console from a serial line.
func IsVT(f *os.File) bool {
	var mode int32
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), kdGetMode, uintptr(unsafe.Pointer(&mode)))
	return errno == 0
}

// Colormap is the console's 16 colors, three bytes each.
type Colormap [48]byte

// KernelColormap is the Linux console's own 16 colors.
var KernelColormap = ColormapOf([16]string{
	"#000000", "#aa0000", "#00aa00", "#aa5500", "#0000aa", "#aa00aa", "#00aaaa", "#aaaaaa",
	"#555555", "#ff5555", "#55ff55", "#ffff55", "#5555ff", "#ff55ff", "#55ffff", "#ffffff",
})

// ColormapOf parses #rrggbb colors.
func ColormapOf(pal [16]string) Colormap {
	var c Colormap
	for i, h := range pal {
		v, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
		c[3*i], c[3*i+1], c[3*i+2] = byte(v>>16), byte(v>>8), byte(v)
	}
	return c
}

// GetColormap reads the colors every console draws with.
func GetColormap(f *os.File) (Colormap, error) {
	var c Colormap
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), gioCmap, uintptr(unsafe.Pointer(&c))); errno != 0 {
		return c, errno
	}
	return c, nil
}

// SetColormap redefines the 16 colors on every console, then reads them
// back; a console that cannot change them says so here.
func SetColormap(f *os.File, c Colormap) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), pioCmap, uintptr(unsafe.Pointer(&c))); errno != 0 {
		return errno
	}
	got, err := GetColormap(f)
	if err != nil {
		return err
	}
	if got != c {
		return fmt.Errorf("the console kept its colors")
	}
	return nil
}

// LoadFont loads a PSF font on the console behind dev.
func LoadFont(dev, path string) error {
	out, err := exec.Command("setfont", "-C", dev, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("setfont: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

type unipair struct{ unicode, fontpos uint16 }

type unimapdesc struct {
	count   uint16
	_       [6]byte
	entries *unipair
}

// HasGlyphs reads the loaded font's Unicode map and checks every rune is in it.
func HasGlyphs(f *os.File, runes []rune) bool {
	buf := make([]unipair, 4096)
	d := unimapdesc{count: uint16(len(buf)), entries: &buf[0]}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), gioUnimap, uintptr(unsafe.Pointer(&d))); errno != 0 {
		return false
	}
	have := map[rune]bool{}
	for _, p := range buf[:d.count] {
		have[rune(p.unicode)] = true
	}
	for _, r := range runes {
		if !have[r] {
			return false
		}
	}
	return true
}

// FontFor picks the font height that still fits cols x rows on the framebuffer.
func FontFor(cols, rows int) int {
	b, err := os.ReadFile("/sys/class/graphics/fb0/virtual_size")
	if err != nil {
		return 16
	}
	w, h, ok := strings.Cut(strings.TrimSpace(string(b)), ",")
	if !ok {
		return 16
	}
	pw, _ := strconv.Atoi(w)
	ph, _ := strconv.Atoi(h)
	if pw/16 >= cols && ph/32 >= rows {
		return 32
	}
	return 16
}

// RedirectKernelMessages sends the kernel's messages for the screen to VT
// vt instead of the one in front, so they never scroll a frame drawn there;
// serial consoles keep them. f is any VT. The kernel ignores a VT that was
// never opened, so vt is opened first; it stays allocated after close.
func RedirectKernelMessages(f *os.File, vt int) error {
	target, err := os.OpenFile(fmt.Sprintf("/dev/tty%d", vt), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	target.Close()
	const tioclSetKmsgRedirect = 11
	buf := [2]byte{tioclSetKmsgRedirect, byte(vt)}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), unix.TIOCLINUX, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return errno
	}
	return nil
}

// QuietKernel keeps kernel messages off the consoles while the wizard runs.
func QuietKernel() {
	// SYSLOG_ACTION_CONSOLE_LEVEL: only emergencies reach the console.
	unix.Syscall(unix.SYS_SYSLOG, 8, 0, 1)
}

// SetSpeed sets a serial line's baud rate and keeps it 8N1.
func SetSpeed(f *os.File, baud int) error {
	speeds := map[int]uint32{9600: unix.B9600, 19200: unix.B19200, 38400: unix.B38400,
		57600: unix.B57600, 115200: unix.B115200}
	sp, ok := speeds[baud]
	if !ok {
		return fmt.Errorf("unsupported speed %d", baud)
	}
	t, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		return err
	}
	t.Cflag = t.Cflag&^unix.CBAUD | sp | unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Ispeed, t.Ospeed = sp, sp
	return unix.IoctlSetTermios(int(f.Fd()), unix.TCSETS, t)
}

// ProbeSize asks the terminal at the other end of a serial line for its
// size: move the cursor far away and ask where it landed.
func ProbeSize(f *os.File) (cols, rows int, ok bool) {
	fd := int(f.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return 0, 0, false
	}
	raw := *old
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 0, 0
	unix.IoctlSetTermios(fd, unix.TCSETS, &raw)
	defer unix.IoctlSetTermios(fd, unix.TCSETS, old)
	unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)

	f.WriteString("\x1b7\x1b[999;999H\x1b[6n\x1b8")
	var got []byte
	deadline := time.Now().Add(1500 * time.Millisecond)
	buf := make([]byte, 64)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if n, _ := unix.Poll(fds, 100); n <= 0 {
			continue
		}
		n, _ := unix.Read(fd, buf)
		got = append(got, buf[:max(n, 0)]...)
		if i := bytes.IndexByte(got, 'R'); i >= 0 {
			if j := bytes.LastIndex(got[:i], []byte("\x1b[")); j >= 0 {
				r, c, ok2 := strings.Cut(string(got[j+2:i]), ";")
				rows, _ = strconv.Atoi(r)
				cols, _ = strconv.Atoi(c)
				return cols, rows, ok2 && cols > 0 && rows > 0
			}
		}
	}
	return 0, 0, false
}

// SetSize records a size on the tty so programs that ask the tty get it.
func SetSize(f *os.File, cols, rows int) error {
	return unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
}
