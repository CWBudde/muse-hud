package render_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/CWBudde/muse-hud/internal/render"
)

// FuzzSanitize asserts the terminal-escape sanitizer's contract on
// arbitrary input: the output holds no ESC, no control or format
// characters, no line breaks, no collapsed-whitespace surprises — and
// sanitizing twice changes nothing (see docs/security.md).
func FuzzSanitize(f *testing.F) {
	f.Add("plain-branch_1.2/x")
	f.Add("ma\x1b[31min\nnext\x07")
	f.Add("\x1b]8;;http://example.invalid\x07link\x1b]8;;\x07")
	f.Add("\x1bPpayload\x1b\\after")
	f.Add("bidi\u202eoverride\u202c zero\u200bwidth")
	f.Add("c1\u009bcontrol del\x7fhere")
	f.Add("\x1b")
	f.Add("\x1b[")
	f.Add("tab\tand\r\nlines\x85next")
	f.Add(string([]byte{0xff, 0xfe, 'a'})) // invalid UTF-8
	f.Fuzz(func(t *testing.T, in string) {
		got := render.Sanitize(in)
		for _, r := range got {
			switch {
			case r == 0x1b:
				t.Errorf("ESC survived Sanitize(%q) = %q", in, got)
			case unicode.IsControl(r):
				t.Errorf("control %U survived Sanitize(%q) = %q", r, in, got)
			case unicode.Is(unicode.Cf, r):
				t.Errorf("format char %U survived Sanitize(%q) = %q", r, in, got)
			}
		}
		if strings.ContainsAny(got, "\n\r") {
			t.Errorf("line break survived Sanitize(%q) = %q", in, got)
		}
		if again := render.Sanitize(got); again != got {
			t.Errorf("Sanitize not idempotent: %q -> %q -> %q", in, got, again)
		}
	})
}
