package render

import (
	"strings"
	"unicode"
)

// Sanitize makes an externally sourced string safe to print into a
// terminal: ANSI/OSC escape sequences are removed whole, remaining
// control and format characters (C0/C1 controls, bidi overrides,
// zero-width characters) are dropped, and whitespace is collapsed to
// single spaces. A branch name, file target or task label can therefore
// neither inject terminal escapes nor break the HUD onto extra lines.
func Sanitize(in string) string {
	runes := []rune(in)
	var b strings.Builder
	b.Grow(len(in))
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b { // ESC: swallow the whole escape sequence
			i = skipEscape(runes, i)
			continue
		}
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == 0x85:
			b.WriteRune(' ')
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// Drop. Cf covers bidi controls (U+202A–202E,
			// U+2066–2069), zero-width space/joiners and BOM.
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// skipEscape returns the index of the last rune of the escape sequence
// starting at ESC (runes[i]), or i itself for a lone trailing ESC.
func skipEscape(runes []rune, i int) int {
	if i+1 >= len(runes) {
		return i
	}
	switch next := runes[i+1]; {
	case next == '[': // CSI: parameter/intermediate bytes, final @–~
		for j := i + 2; j < len(runes); j++ {
			if runes[j] >= 0x40 && runes[j] <= 0x7e {
				return j
			}
		}
		return len(runes) - 1
	case next == ']' || next == 'P' || next == 'X' || next == '^' || next == '_':
		// OSC/DCS/SOS/PM/APC: terminated by BEL or ST (ESC \).
		for j := i + 2; j < len(runes); j++ {
			if runes[j] == 0x07 {
				return j
			}
			if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' {
				return j + 1
			}
		}
		return len(runes) - 1
	default:
		// Two-rune Fe/Fs escape, or ESC + intermediates + final.
		j := i + 1
		for j < len(runes) && runes[j] >= 0x20 && runes[j] <= 0x2f {
			j++
		}
		return j
	}
}
