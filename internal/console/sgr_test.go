package console

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxFileClearsBrightBeforeNormalColors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out")
	f, _ := os.Create(p)
	lf := &LinuxFile{File: f}
	lf.Write([]byte("\x1b[95mll\x1b[35mer\x1b"))
	lf.Write([]byte("[34m /\x1b[38;5;34;1m.\x1b[m"))
	f.Close()
	got, _ := os.ReadFile(p)
	want := "\x1b[95mll\x1b[35;22mer\x1b[34;22m /\x1b[38;5;34;1m.\x1b[m"
	if string(got) != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
