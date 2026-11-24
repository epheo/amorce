// Package font builds the styled console's font: Terminus for text, our own
// pixels for the symbols the screens draw, 256 glyphs so the console keeps
// 16 foreground colors.
package font

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	W, H     = 8, 16
	maxGlyph = 256
)

// Glyph is one 8x16 bitmap, a byte per row, most significant bit left.
type Glyph [H]byte

// Font has 256 slots. ASCII and Latin-1 sit at their own code points: the
// kernel clears the screen with slot 32 and expects a space there.
type Font struct {
	Glyphs [maxGlyph]Glyph
	Runes  [maxGlyph][]rune // the code points each slot draws
	used   [maxGlyph]bool
}

// Count is how many slots hold a glyph.
func (f *Font) Count() int {
	n := 0
	for _, u := range f.used {
		if u {
			n++
		}
	}
	return n
}

// Put fills slot i.
func (f *Font) Put(i int, g Glyph, runes ...rune) {
	f.Glyphs[i], f.Runes[i], f.used[i] = g, runes, true
}

// ReadPSF reads a PSF1 or PSF2 font with a Unicode table, gzipped or not.
func ReadPSF(path string) (map[rune]Glyph, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		z, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if raw, err = io.ReadAll(z); err != nil {
			return nil, err
		}
	}
	out := map[rune]Glyph{}
	switch {
	case len(raw) > 4 && raw[0] == 0x36 && raw[1] == 0x04:
		mode, size := raw[2], int(raw[3])
		if size != H {
			return nil, fmt.Errorf("%s: glyphs are %d rows, want %d", path, size, H)
		}
		n := 256
		if mode&1 != 0 {
			n = 512
		}
		if mode&2 == 0 {
			return nil, fmt.Errorf("%s: no Unicode table", path)
		}
		glyphs := raw[4 : 4+n*size]
		table := raw[4+n*size:]
		for i := 0; i < n && len(table) >= 2; i++ {
			var g Glyph
			copy(g[:], glyphs[i*size:(i+1)*size])
			for len(table) >= 2 {
				u := binary.LittleEndian.Uint16(table)
				table = table[2:]
				if u == 0xFFFF {
					break
				}
				if u == 0xFFFE {
					// Sequences of combining characters: skip to the glyph's end.
					for len(table) >= 2 && binary.LittleEndian.Uint16(table) != 0xFFFF {
						table = table[2:]
					}
					continue
				}
				if _, ok := out[rune(u)]; !ok {
					out[rune(u)] = g
				}
			}
		}
	case len(raw) > 32 && bytes.Equal(raw[:4], []byte{0x72, 0xb5, 0x4a, 0x86}):
		hdr := func(i int) int { return int(binary.LittleEndian.Uint32(raw[4*i:])) }
		hsize, flags, n, size, height, width := hdr(2), hdr(3), hdr(4), hdr(5), hdr(6), hdr(7)
		if height != H || width != W {
			return nil, fmt.Errorf("%s: glyphs are %dx%d, want %dx%d", path, width, height, W, H)
		}
		if flags&1 == 0 {
			return nil, fmt.Errorf("%s: no Unicode table", path)
		}
		table := raw[hsize+n*size:]
		for i := 0; i < n && len(table) > 0; i++ {
			var g Glyph
			copy(g[:], raw[hsize+i*size:hsize+(i+1)*size])
			seq := false
			for len(table) > 0 {
				if table[0] == 0xFF {
					table = table[1:]
					break
				}
				if table[0] == 0xFE {
					seq = true
					table = table[1:]
					continue
				}
				r, l := utf8.DecodeRune(table)
				table = table[l:]
				if _, ok := out[r]; !ok && !seq {
					out[r] = g
				}
			}
		}
	default:
		return nil, fmt.Errorf("%s: not a PSF font", path)
	}
	return out, nil
}

// Add puts a glyph in the first free slot outside printable ASCII.
func (f *Font) Add(g Glyph, runes ...rune) error {
	for i := range maxGlyph {
		if !f.used[i] && (i < 0x20 || i > 0x7E) {
			f.Put(i, g, runes...)
			return nil
		}
	}
	return fmt.Errorf("no free slot for %q", runes)
}

// Alias makes more code points draw the glyph that already draws to.
func (f *Font) Alias(to rune, more ...rune) bool {
	for i, rs := range f.Runes {
		for _, r := range rs {
			if r == to {
				f.Runes[i] = append(f.Runes[i], more...)
				return true
			}
		}
	}
	return false
}

// WritePSF2 writes the font scaled by an integer factor: 1 is 8x16, 2 is 16x32.
func (f *Font) WritePSF2(w io.Writer, scale int) error {
	width, height := W*scale, H*scale
	rowBytes := (width + 7) / 8
	size := rowBytes * height
	n := maxGlyph
	var b bytes.Buffer
	for _, v := range []uint32{0x864ab572, 0, 32, 1, uint32(n), uint32(size), uint32(height), uint32(width)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	for i := range n {
		g := f.Glyphs[i]
		for y := range height {
			row := make([]byte, rowBytes)
			for x := range width {
				if g[y/scale]&(0x80>>(x/scale)) != 0 {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
			b.Write(row)
		}
	}
	for i := range n {
		for _, r := range f.Runes[i] {
			b.WriteString(string(r))
		}
		b.WriteByte(0xFF)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// Pix draws a glyph from rows of '#' and '.', top row first, starting at row y.
func Pix(y int, rows ...string) Glyph {
	var g Glyph
	for i, r := range rows {
		for x, c := range strings.TrimSpace(r) {
			if c == '#' && x < W {
				g[y+i] |= 0x80 >> x
			}
		}
	}
	return g
}

func (g Glyph) or(o Glyph) Glyph {
	for i := range g {
		g[i] |= o[i]
	}
	return g
}
