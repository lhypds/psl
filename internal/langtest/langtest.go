// Package langtest is the harness every language folder's tests are written
// against: a table of source and the slot span it must produce, so a new
// language is tested the same way as the others.
//
// It lives beside internal/lang rather than in it because internal/lang holds
// one folder per language and nothing else, and because a language folder's
// tests import this the same way any other package would.
package langtest

import (
	"testing"

	"psl/internal/lang"
)

// Case is one source file and the slot span it must produce, or "" when it
// must produce none.
type Case struct {
	Name string
	Src  string
	Want string // "" means: no slot here
}

// Run checks every case against l.
func Run(t *testing.T, l *lang.Language, cases []Case) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, ok := FirstSlot(l, tc.Src)
			switch {
			case tc.Want == "" && ok:
				t.Errorf("%s read %q as a slot in:\n%s", l.Name, got, tc.Src)
			case tc.Want != "" && !ok:
				t.Errorf("%s found no slot, want %q, in:\n%s", l.Name, tc.Want, tc.Src)
			case tc.Want != got:
				t.Errorf("%s slot = %q, want %q, in:\n%s", l.Name, got, tc.Want, tc.Src)
			}
		})
	}
}

// FirstSlot is what the parser does, in miniature. internal/slot cannot be
// used here — it is what the languages are for, and its own tests import them —
// so the language tests scan for a delimiter pair the same way it does, and
// report the span they agree on.
func FirstSlot(l *lang.Language, src string) (string, bool) {
	sx := l.Analyze(src)
	for i := 0; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' || !sx.CanOpen(i) {
			continue
		}
		for j := i + 2; j+1 < len(src); j++ {
			if src[j] != ':' || src[j+1] != ':' || !sx.CanClose(i, j) {
				continue
			}
			return src[i : j+2], true
		}
	}
	return "", false
}
