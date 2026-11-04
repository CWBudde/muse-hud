package collect

import (
	"fmt"
	"sort"
	"strings"
)

// KnownPayloadTypes returns the sorted payload_type values muse-hud
// recognizes on the verified Muse log shapes (see
// docs/compatibility.md). Dispatch consumes a subset; the rest are
// recognized and ignored.
func KnownPayloadTypes() []string {
	out := make([]string, 0, len(knownPayloadTypes))
	for k := range knownPayloadTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// VerifiedVersionList returns the sorted Muse versions in the support
// table (VerifiedVersions).
func VerifiedVersionList() []string {
	out := make([]string, 0, len(VerifiedVersions))
	for k := range VerifiedVersions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// DriftHint is one doctor-facing explanation for an unknown
// payload_type: what was seen, the closest known type (if any), and
// what muse-hud does about it (ignore it — never guess).
type DriftHint struct {
	Unknown string
	Count   int
	Closest string // closest known payload_type by shared dot-segments; "" if none shares the first segment
	Hint    string
}

// DriftHints turns Stats.UnknownPayloadTypes into diff-style hints:
// each unknown type is shown against the known type it most resembles
// (longest shared prefix of dot-separated segments), so a rename or a
// new variant in a known family is recognizable at a glance. The
// result is sorted by unknown type for stable doctor output.
func DriftHints(unknown map[string]int) []DriftHint {
	known := KnownPayloadTypes()
	names := make([]string, 0, len(unknown))
	for name := range unknown {
		names = append(names, name)
	}
	sort.Strings(names)
	hints := make([]DriftHint, 0, len(names))
	for _, name := range names {
		closest, shared := closestKnown(name, known)
		h := DriftHint{Unknown: name, Count: unknown[name], Closest: closest}
		switch {
		case closest == "":
			h.Hint = "new payload family this muse-hud build does not know; ignored, the segments it might feed stay hidden"
		case shared >= 2:
			h.Hint = fmt.Sprintf("resembles known %q — a rename or new variant in that family; ignored until support is verified", closest)
		default:
			h.Hint = fmt.Sprintf("shares only the %q family prefix with known types (e.g. %q); ignored", firstSegment(name), closest)
		}
		hints = append(hints, h)
	}
	return hints
}

func firstSegment(payloadType string) string {
	if i := strings.IndexByte(payloadType, '.'); i >= 0 {
		return payloadType[:i]
	}
	return payloadType
}

// closestKnown returns the known type sharing the most leading
// dot-separated segments with name (ties break alphabetically), and
// how many segments are shared. Zero shared segments yields "".
func closestKnown(name string, known []string) (string, int) {
	parts := strings.Split(name, ".")
	best, bestShared := "", 0
	for _, k := range known {
		kparts := strings.Split(k, ".")
		shared := 0
		for shared < len(parts) && shared < len(kparts) && parts[shared] == kparts[shared] {
			shared++
		}
		if shared > bestShared {
			best, bestShared = k, shared
		}
	}
	return best, bestShared
}
