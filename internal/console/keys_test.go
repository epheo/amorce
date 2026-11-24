package console

import "testing"

func TestTranslateKeys(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[[C":          "\x1bOR",
		"a\x1b[[Db\x1b[[E": "a\x1bOSb\x1b[15~",
		"\x1b[A\x1b[24~":   "\x1b[A\x1b[24~",
	} {
		if got := string(translateKeys([]byte(in))); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
