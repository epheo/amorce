package console

import (
	"bytes"
	"os"
)

// KeyFile reads a Linux console, turning its own sequences for F1 to F5,
// which input parsers do not know, into the common ones.
type KeyFile struct {
	*os.File
	pending []byte
}

var linuxKeys = [][2][]byte{
	{[]byte("\x1b[[A"), []byte("\x1bOP")},
	{[]byte("\x1b[[B"), []byte("\x1bOQ")},
	{[]byte("\x1b[[C"), []byte("\x1bOR")},
	{[]byte("\x1b[[D"), []byte("\x1bOS")},
	{[]byte("\x1b[[E"), []byte("\x1b[15~")},
}

// The console writes a key's whole sequence at once, so one read never
// splits it.
func (k *KeyFile) Read(p []byte) (int, error) {
	if len(k.pending) == 0 {
		buf := make([]byte, max(len(p), 64))
		n, err := k.File.Read(buf)
		if n == 0 {
			return 0, err
		}
		k.pending = translateKeys(buf[:n])
	}
	n := copy(p, k.pending)
	k.pending = k.pending[n:]
	return n, nil
}

func translateKeys(b []byte) []byte {
	if !bytes.Contains(b, []byte("\x1b[[")) {
		return b
	}
	for _, r := range linuxKeys {
		b = bytes.ReplaceAll(b, r[0], r[1])
	}
	return b
}
