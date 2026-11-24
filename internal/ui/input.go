package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// input is a one-line text field; the cursor always sits at the end, so
// typing, backspace and ctrl+u are the whole editing model.
type input struct {
	value string
}

func newInput(v string) input { return input{value: v} }

// handle edits the field and reports whether the key was used.
func (in *input) handle(s string, k tea.KeyPressMsg) bool {
	switch s {
	case "backspace":
		if r := []rune(in.value); len(r) > 0 {
			in.value = string(r[:len(r)-1])
		}
		return true
	case "ctrl+u":
		in.value = ""
		return true
	case "space":
		in.value += " "
		return true
	}
	if k.Text != "" && !strings.ContainsFunc(k.Text, unicode.IsControl) {
		in.value += k.Text
		return true
	}
	return false
}

// view draws the field with a block cursor when focused.
func (m *Model) inputView(in input, focused bool) string {
	t := m.t
	if !focused {
		return "  " + t.S(t.Fg).Render(in.value)
	}
	return t.S(t.Accent).Render("> ") + t.S(t.Fg).Render(in.value) + t.S(t.Good).Render("█")
}
