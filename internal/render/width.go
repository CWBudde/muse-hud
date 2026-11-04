package render

import (
	"strings"
	"unicode"
)

// renderLine styles and fits one line of segments. Fitting first
// shrinks path segments from the left, then truncates the whole line
// from the right with an ellipsis. ANSI sequences never count towards
// the width and a truncated styled line is always reset-terminated.
func renderLine(segs []segment, width int, color bool) string {
	if width > 0 {
		segs = fitSegments(segs, width)
	}
	var b strings.Builder
	for _, sg := range segs {
		b.WriteString(sg.sep)
		b.WriteString(styled(sg, color))
	}
	if width > 0 {
		return truncateVisible(b.String(), width, color)
	}
	return b.String()
}

func styled(sg segment, color bool) string {
	if !color || sg.style == "" {
		return sg.text
	}
	return sg.style + sg.text + styleReset
}

func segmentsWidth(segs []segment) int {
	w := 0
	for _, sg := range segs {
		w += displayWidth(sg.sep + sg.text)
	}
	return w
}

// fitSegments shrinks path text from the left (path component by path
// component, then rune by rune) until the line fits or no path can
// shrink further.
func fitSegments(segs []segment, width int) []segment {
	out := append([]segment(nil), segs...)
	for segmentsWidth(out) > width {
		idx := longestPathSegment(out)
		if idx < 0 {
			break
		}
		out[idx] = shrinkPathOnce(out[idx])
	}
	return out
}

const minPathRunes = 4 // a shrunk path keeps at least "…/xy"

func longestPathSegment(segs []segment) int {
	idx, longest := -1, minPathRunes
	for i, sg := range segs {
		if !sg.path {
			continue
		}
		if n := len([]rune(sg.text)) - sg.pathPrefix; n > longest {
			idx, longest = i, n
		}
	}
	return idx
}

// shrinkPathOnce removes the first remaining path component (adding an
// ellipsis marker once), or one rune when no component boundary remains.
func shrinkPathOnce(sg segment) segment {
	runes := []rune(sg.text)
	prefix, path := string(runes[:sg.pathPrefix]), string(runes[sg.pathPrefix:])
	body := strings.TrimPrefix(path, "…/")
	if i := strings.IndexByte(body, '/'); i >= 0 && len([]rune(body[i+1:])) >= 2 {
		body = body[i+1:]
	} else if r := []rune(body); len(r) > minPathRunes-1 {
		body = string(r[1:])
	} else {
		sg.path = false // cannot shrink further
		return sg
	}
	if !strings.HasPrefix(body, "…/") {
		body = "…/" + body
	}
	sg.text = prefix + body
	return sg
}

// truncateVisible cuts s to width display columns, marking the cut with
// …. ANSI escape sequences (only our own styles can occur) are copied
// without counting towards the width.
func truncateVisible(s string, width int, color bool) string {
	if visibleWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	cols := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b { // copy the whole CSI sequence, width 0
			j := i + 1
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r := []rune(s[i:])[0]
		w := runeWidth(r)
		if cols+w > width-1 {
			break
		}
		b.WriteRune(r)
		cols += w
		i += len(string(r))
	}
	b.WriteString("…")
	if color {
		b.WriteString(styleReset)
	}
	return b.String()
}

// visibleWidth counts display columns, skipping ANSI sequences.
func visibleWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			i = j + 1
			if i > len(s) {
				i = len(s)
			}
			continue
		}
		r := []rune(s[i:])[0]
		w += runeWidth(r)
		i += len(string(r))
	}
	return w
}

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// runeWidth approximates terminal cell width with the standard library
// only: combining marks are 0, East-Asian wide/fullwidth and common
// emoji ranges are 2, everything else is 1.
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
		return 2
	}
	switch {
	case r >= 0x1100 && r <= 0x115f, // Hangul Jamo
		r >= 0x2e80 && r <= 0x303e,
		r >= 0x3041 && r <= 0xa4cf,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,   // fullwidth forms
		r >= 0x1f300 && r <= 0x1f9ff, // emoji blocks
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}
