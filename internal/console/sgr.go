package console

import (
	"os"
	"strconv"
	"strings"
)

// LinuxFile is a console device whose writes are fixed for the Linux
// console's color model, where bright colors (SGR 90 to 97) are normal colors
// plus bold: a later normal color keeps that bold and draws bright too. Each
// normal color gets an explicit "normal intensity" (22) here.
type LinuxFile struct {
	*os.File
	pending []byte   // an escape sequence cut by the end of a write
	Trace   *os.File // when set, receives every byte written
}

func (f *LinuxFile) Write(b []byte) (int, error) {
	in := append(f.pending, b...)
	f.pending = nil
	out := make([]byte, 0, len(in)+64)
	for i := 0; i < len(in); i++ {
		if in[i] != 0x1b || i+1 >= len(in) || in[i+1] != '[' {
			if in[i] == 0x1b && i+1 == len(in) {
				f.pending = append(f.pending, in[i:]...)
				break
			}
			out = append(out, in[i])
			continue
		}
		j := i + 2
		for j < len(in) && (in[j] >= '0' && in[j] <= '9' || in[j] == ';' || in[j] == ':' || in[j] == '?' || in[j] == '>' || in[j] == '!' || in[j] == '$' || in[j] == ' ') {
			j++
		}
		if j == len(in) {
			f.pending = append(f.pending, in[i:]...)
			break
		}
		seq := in[i : j+1]
		if in[j] == 'm' {
			seq = fixSGR(string(in[i+2 : j]))
		}
		out = append(out, seq...)
		i = j
	}
	if f.Trace != nil {
		f.Trace.Write(out)
	}
	if _, err := f.File.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}

func fixSGR(params string) []byte {
	ps := strings.Split(params, ";")
	needNormal := false
	for k := 0; k < len(ps); k++ {
		n, err := strconv.Atoi(ps[k])
		if err != nil {
			continue
		}
		switch {
		case n == 38 || n == 48:
			// Extended color: skip its arguments.
			if k+1 < len(ps) && ps[k+1] == "5" {
				k += 2
			} else if k+1 < len(ps) && ps[k+1] == "2" {
				k += 4
			}
		case n >= 30 && n <= 37 || n == 39:
			needNormal = true
		case n >= 90 && n <= 97:
			needNormal = false
		}
	}
	if needNormal {
		params += ";22"
	}
	return []byte("\x1b[" + params + "m")
}
