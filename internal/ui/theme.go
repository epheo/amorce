// Package ui draws the amorce screens for one terminal.
package ui

import (
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

// Look is how a terminal can draw: our console palette and font, the stock
// console, or a true-color terminal over SSH.
type Look int

const (
	LookStyled Look = iota
	LookNormal
	LookTrue
)

func (l Look) String() string {
	return [...]string{"styled", "normal", "true"}[l]
}

// Theme maps the screens' roles to colors and symbols for one Look.
type Theme struct {
	Look Look

	Fg, Key, Muted, Faint      color.Color
	Border, BorderDim, Title   color.Color
	Accent, Mauve, Good, Bad   color.Color
	Warn, Info, Bok, Bar, Dark color.Color
	Light, BarText             color.Color

	grad []color.Color
	bold bool
	Box  lipgloss.Border

	Ok, Fail, Cur, Pend, Alert, Sep, Cable, Focus, List string
	Spin                                                []string
	Full, Empty, Slash                                  string
}

// Palette is the console palette for LookStyled, slot by slot (charmtone).
// Slots 0 to 7 hold every background color: the console only draws those.
var Palette = [16]string{
	"#201F26", // 0 Pepper
	"#FF577D", // 1 Coral
	"#00FFB2", // 2 Julep
	"#3A3943", // 3 Char
	"#6B50FF", // 4 Charple
	"#FF60FF", // 5 Dolly
	"#00A4FF", // 6 Malibu
	"#ECEBF0", // 7 Sash
	"#4D4C57", // 8 Iron
	"#605F6B", // 9 Oyster
	"#68FFD6", // 10 Bok
	"#F5EF34", // 11 Mustard
	"#8B75FF", // 12 Hazy
	"#D46EFF", // 13 Mauve
	"#A2A0AD", // 14 Steam
	"#BFBCC8", // 15 Smoke
}

func slot(n int) color.Color { return lipgloss.Color(itoa(n)) }

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "1" + string(rune('0'+n-10))
}

func NewTheme(l Look) Theme {
	t := Theme{Look: l}
	unicode := func() {
		t.Ok, t.Fail, t.Cur, t.Pend, t.Alert = "✓", "×", "●", "•", "!"
		t.Sep, t.Cable, t.Focus, t.List = " • ", "━", "┃", "│"
		t.Spin = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
		t.Full, t.Empty, t.Slash = "█", "░", "/"
		t.Box = lipgloss.RoundedBorder()
	}
	switch l {
	case LookStyled:
		unicode()
		t.Fg, t.Key, t.Muted, t.Faint = slot(7), slot(15), slot(14), slot(9)
		t.Border, t.BorderDim, t.Title = slot(4), slot(8), slot(12)
		t.Accent, t.Mauve, t.Good, t.Bad = slot(5), slot(13), slot(2), slot(1)
		t.Warn, t.Info, t.Bok, t.Bar, t.Dark = slot(11), slot(6), slot(10), slot(3), slot(0)
		t.Light, t.BarText = slot(7), slot(15)
		t.grad = []color.Color{slot(4), slot(12), slot(13), slot(5)}
	case LookNormal:
		t.Ok, t.Fail, t.Cur, t.Pend, t.Alert = "+", "x", "*", "-", "!"
		t.Sep, t.Cable, t.Focus, t.List = " | ", "=", "|", "|"
		t.Spin = []string{"|", "/", "-", "\\"}
		t.Full, t.Empty, t.Slash = "█", "░", "/"
		t.Box = lipgloss.NormalBorder()
		t.Fg, t.Key, t.Muted, t.Faint = slot(15), slot(7), slot(7), slot(8)
		t.Border, t.BorderDim, t.Title = slot(12), slot(8), slot(13)
		t.Accent, t.Mauve, t.Good, t.Bad = slot(13), slot(13), slot(10), slot(9)
		t.Warn, t.Info, t.Bok, t.Bar, t.Dark = slot(11), slot(14), slot(14), slot(7), slot(0)
		t.Light, t.BarText = slot(15), slot(0)
	case LookTrue:
		unicode()
		hex := func(i int) color.Color { return lipgloss.Color(Palette[i]) }
		t.Fg, t.Key, t.Muted, t.Faint = hex(7), hex(15), hex(14), hex(9)
		t.Border, t.BorderDim, t.Title = hex(4), hex(8), hex(12)
		t.Accent, t.Mauve, t.Good, t.Bad = hex(5), hex(13), hex(2), hex(1)
		t.Warn, t.Info, t.Bok, t.Bar, t.Dark = hex(11), hex(6), hex(10), hex(3), hex(0)
		t.Light, t.BarText = lipgloss.Color("#FFFAF1"), hex(15)
		t.grad = []color.Color{hex(4), hex(5)}
		t.bold = true
	}
	return t
}

// S styles text in one color.
func (t Theme) S(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// B is S plus bold where the terminal draws bold as bold.
func (t Theme) B(c color.Color) lipgloss.Style { return t.S(c).Bold(t.bold) }

// Pill is a status-bar or button label on a role's color; the text color
// follows the background so it stays readable in every look.
func (t Theme) Pill(bg color.Color, s string) string {
	fg := t.Dark
	switch bg {
	case t.Border:
		fg = t.Light
	case t.Bar:
		fg = t.BarText
	}
	if t.Look == LookNormal {
		// The console draws backgrounds from slots 0 to 7 only: use the dim
		// twin of each bright role, with text that reads on it.
		twin := map[color.Color]color.Color{t.Accent: slot(5), t.Border: slot(4), t.Good: slot(2), t.Bad: slot(1), t.Info: slot(6), t.Warn: slot(3)}
		if c, ok := twin[bg]; ok {
			bg = c
			fg = t.Light
			if c == slot(2) || c == slot(6) || c == slot(3) {
				fg = t.Dark
			}
		}
	}
	return lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(t.bold).Render(s)
}

// Button is a pill that may be disabled.
func (t Theme) Button(s string, focused, enabled bool, on color.Color) string {
	switch {
	case !enabled:
		return lipgloss.NewStyle().Background(t.Bar).Foreground(t.Faint).Render(s)
	case focused:
		return t.Pill(on, s)
	}
	return t.Pill(t.Bar, s)
}

// gradAt is the gradient color at position i of n.
func (t Theme) gradAt(i, n int) color.Color {
	switch {
	case len(t.grad) == 0:
		return t.Accent
	case t.Look == LookStyled:
		// The console has four slots for it: step through them.
		k := i * len(t.grad) / max(n, 1)
		return t.grad[min(k, len(t.grad)-1)]
	}
	f := float64(i) / math.Max(float64(n-1), 1)
	a, b := t.grad[0], t.grad[len(t.grad)-1]
	return blend(a, b, f)
}

func blend(a, b color.Color, f float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 { return uint8((float64(x)*(1-f) + float64(y)*f) / 257) }
	return color.RGBA{mix(ar, br), mix(ag, bg), mix(ab, bb), 0xff}
}

// Gradient colors s from the first gradient stop to the last.
func (t Theme) Gradient(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		b.WriteString(t.B(t.gradAt(i, len(rs))).Render(string(r)))
	}
	return b.String()
}

// Progress draws a progress bar; the gradient spans the full width, as Bubbles does.
func (t Theme) Progress(width int, frac float64) string {
	filled := int(math.Round(frac * float64(width)))
	filled = max(0, min(width, filled))
	var b strings.Builder
	for i := range filled {
		c := t.Title
		if t.Look != LookNormal {
			c = t.gradAt(i, width)
		}
		b.WriteString(t.S(c).Render(t.Full))
	}
	b.WriteString(t.S(t.Faint).Render(strings.Repeat(t.Empty, width-filled)))
	return b.String()
}
