package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/epheo/microshift-installer/internal/font"
)

// terminus is the text font the console fonts start from; kbd ships it.
const terminus = "/usr/lib/kbd/consolefonts/Lat2-Terminus16.psfu.gz"

// fontCmd writes console-16.psf (8x16) and console-32.psf (16x32) to dir.
func fontCmd(dir string) error {
	src := os.Getenv("TERMINUS")
	if src == "" {
		src = terminus
	}
	f, err := font.Build(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, scale := range []int{1, 2} {
		var b bytes.Buffer
		if err := f.WritePSF2(&b, scale); err != nil {
			return err
		}
		p := filepath.Join(dir, fmt.Sprintf("console-%d.psf", 16*scale))
		if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("%d glyphs\n", f.Count())
	return nil
}
