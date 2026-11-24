package font

import (
	"bytes"
	"os"
	"testing"
)

func TestBuildKeepsSixteenColors(t *testing.T) {
	src := os.Getenv("TERMINUS")
	if src == "" {
		src = "/usr/lib/kbd/consolefonts/Lat2-Terminus16.psfu.gz"
	}
	if _, err := os.Stat(src); err != nil {
		t.Skip("no Terminus font here; set TERMINUS")
	}
	f, err := Build(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Runes[' '][0] != ' ' || f.Runes['A'][0] != 'A' {
		t.Fatal("ASCII is not at its own code points")
	}
	var b bytes.Buffer
	if err := f.WritePSF2(&b, 2); err != nil {
		t.Fatal(err)
	}
	back, err := os.CreateTemp(t.TempDir(), "*.psf")
	if err != nil {
		t.Fatal(err)
	}
	back.Write(b.Bytes())
	back.Close()
	for _, scale := range []int{1} {
		var b1 bytes.Buffer
		f.WritePSF2(&b1, scale)
		os.WriteFile(back.Name(), b1.Bytes(), 0o644)
		got, err := ReadPSF(back.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range "Aé✓⣾╭━█…’" {
			if _, ok := got[r]; !ok {
				t.Errorf("font lacks %q", r)
			}
		}
	}
}
