package font

import "fmt"

// Line positions in the 8x16 cell; every line glyph uses them so boxes join.
const (
	vx = 3 // vertical light line column
	hy = 7 // horizontal light line row
)

func hline(g *Glyph, y, x0, x1 int) {
	for x := x0; x <= x1; x++ {
		g[y] |= 0x80 >> x
	}
}

func vline(g *Glyph, x, y0, y1 int) {
	for y := y0; y <= y1; y++ {
		g[y] |= 0x80 >> x
	}
}

// box draws a light box-drawing glyph from the arms it has.
func box(up, down, left, right bool) Glyph {
	var g Glyph
	if left {
		hline(&g, hy, 0, vx)
	}
	if right {
		hline(&g, hy, vx, W-1)
	}
	if up {
		vline(&g, vx, 0, hy)
	}
	if down {
		vline(&g, vx, hy, H-1)
	}
	return g
}

// arc is a rounded corner: the arms step through one diagonal pixel.
func arc(up, right bool) Glyph {
	var g Glyph
	dx, y := -1, hy+1
	if right {
		dx = 1
	}
	if up {
		y = hy - 1
	}
	g[y] |= 0x80 >> (vx + dx)
	if right {
		hline(&g, hy, vx+2, W-1)
	} else {
		hline(&g, hy, 0, vx-2)
	}
	if up {
		vline(&g, vx, 0, hy-2)
	} else {
		vline(&g, vx, hy+2, H-1)
	}
	return g
}

func double(up, down, left, right bool) Glyph {
	var g Glyph
	const x1, x2, y1, y2 = vx - 1, vx + 1, hy - 1, hy + 1
	switch {
	case left && right && !up && !down:
		hline(&g, y1, 0, W-1)
		hline(&g, y2, 0, W-1)
	case up && down && !left && !right:
		vline(&g, x1, 0, H-1)
		vline(&g, x2, 0, H-1)
	case down && right:
		hline(&g, y1, x1, W-1)
		hline(&g, y2, x2, W-1)
		vline(&g, x1, y1, H-1)
		vline(&g, x2, y2, H-1)
	case down && left:
		hline(&g, y1, 0, x2)
		hline(&g, y2, 0, x1)
		vline(&g, x2, y1, H-1)
		vline(&g, x1, y2, H-1)
	case up && right:
		hline(&g, y2, x1, W-1)
		hline(&g, y1, x2, W-1)
		vline(&g, x1, 0, y2)
		vline(&g, x2, 0, y1)
	case up && left:
		hline(&g, y2, 0, x2)
		hline(&g, y1, 0, x1)
		vline(&g, x2, 0, y2)
		vline(&g, x1, 0, y1)
	}
	return g
}

func shade(on func(x, y int) bool) Glyph {
	var g Glyph
	for y := range H {
		for x := range W {
			if on(x, y) {
				g[y] |= 0x80 >> x
			}
		}
	}
	return g
}

// braille draws dots 1 to 8 of a braille cell, as the dot spinner uses them.
func braille(r rune) Glyph {
	bits := int(r - 0x2800)
	// Dot order: 1 2 3 down the left, 4 5 6 down the right, 7 left and 8 right at the bottom.
	pos := [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
	var g Glyph
	for i, p := range pos {
		if bits&(1<<i) == 0 {
			continue
		}
		x, y := 1+p[0]*4, 2+p[1]*4
		hline(&g, y, x, x+1)
		hline(&g, y+1, x, x+1)
	}
	return g
}

// builder keeps the first slot error so Build reads as a list.
type builder struct {
	*Font
	err error
}

func (b *builder) add(g Glyph, runes ...rune) {
	if err := b.Font.Add(g, runes...); err != nil && b.err == nil {
		b.err = err
	}
}

// Build assembles the font from a Terminus 8x16 font for text.
func Build(terminus string) (*Font, error) {
	base, err := ReadPSF(terminus)
	if err != nil {
		return nil, err
	}
	f := &builder{Font: &Font{}}
	for r := rune(0x20); r <= 0x7E; r++ {
		g, ok := base[r]
		if !ok {
			return nil, fmt.Errorf("%s has no %U", terminus, r)
		}
		f.Put(int(r), g, r)
	}
	// Lat2-Terminus covers most of Latin-1; what it lacks falls back to U+FFFD.
	for r := rune(0xA0); r <= 0xFF; r++ {
		if g, ok := base[r]; ok {
			f.Put(int(r), g, r)
		}
	}
	if g, ok := base['?']; ok {
		f.add(g, 0xFFFD)
	}
	if g, ok := base['€']; ok {
		f.add(g, '€')
	}

	f.add(box(false, false, true, true), '─')
	f.add(box(true, true, false, false), '│')
	f.add(box(false, true, false, true), '┌')
	f.add(box(false, true, true, false), '┐')
	f.add(box(true, false, false, true), '└')
	f.add(box(true, false, true, false), '┘')
	f.add(box(true, true, false, true), '├')
	f.add(box(true, true, true, false), '┤')
	f.add(box(false, true, true, true), '┬')
	f.add(box(true, false, true, true), '┴')
	f.add(box(true, true, true, true), '┼')
	f.add(arc(false, true), '╭')
	f.add(arc(false, false), '╮')
	f.add(arc(true, false), '╯')
	f.add(arc(true, true), '╰')
	var heavyH, heavyV Glyph
	hline(&heavyH, hy, 0, W-1)
	hline(&heavyH, hy+1, 0, W-1)
	vline(&heavyV, vx, 0, H-1)
	vline(&heavyV, vx+1, 0, H-1)
	f.add(heavyH, '━')
	f.add(heavyV, '┃')
	f.add(double(false, false, true, true), '═')
	f.add(double(true, true, false, false), '║')
	f.add(double(false, true, false, true), '╔')
	f.add(double(false, true, true, false), '╗')
	f.add(double(true, false, false, true), '╚')
	f.add(double(true, false, true, false), '╝')

	f.add(shade(func(x, y int) bool { return true }), '█')
	f.add(shade(func(x, y int) bool { return x%2 == 0 && y%2 == 0 }), '░')
	f.add(shade(func(x, y int) bool { return (x+y)%2 == 0 }), '▒')
	f.add(shade(func(x, y int) bool { return !(x%2 == 0 && y%2 == 0) }), '▓')
	f.add(shade(func(x, y int) bool { return y < H/2 }), '▀')
	f.add(shade(func(x, y int) bool { return y >= H/2 }), '▄')
	f.add(shade(func(x, y int) bool { return x < W/2 }), '▌')
	f.add(shade(func(x, y int) bool { return x >= W/2 }), '▐')

	f.add(Pix(4,
		"......##",
		".....##.",
		"#...##..",
		"##.##...",
		".###....",
		"..#.....",
	), '✓', '✔')
	f.add(Pix(5,
		"..####..",
		".######.",
		".######.",
		".######.",
		".######.",
		"..####..",
	), '●')
	f.add(Pix(6,
		"...##...",
		"..####..",
		"..####..",
		"...##...",
	), '•')
	f.add(Pix(12, "#..#..#."), '…')
	for _, r := range "⣾⣽⣻⢿⡿⣟⣯⣷" {
		f.add(braille(r), r)
	}

	// Typographic marks other programs print draw as their ASCII twins.
	f.Alias('\'', '‘', '’')
	f.Alias('"', '“', '”')
	f.Alias('-', '–', '—', '‐')
	f.Alias('>', '›')
	f.Alias('<', '‹')
	return f.Font, f.err
}
