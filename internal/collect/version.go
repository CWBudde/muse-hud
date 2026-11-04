package collect

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Support describes how well a Muse version is understood. Unknown
// versions still attempt collection but tag the snapshot unverified.
type Support struct {
	Version  string // dotted version as reported, e.g. "1.4.14"; empty if unknown
	Verified bool   // version is in the verified support table
	Source   string // "session log build" | "muse --version" | "" (unknown)
}

// VerifiedVersions is the support table: Muse versions whose session-log
// shapes muse-hud has been verified against (fixtures + hand-checks).
var VerifiedVersions = map[string]bool{
	"1.4.14": true,
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseVersion extracts the dotted version from `muse --version` output
// (e.g. "Muse Code 1.4.14 (1.4.14-R7309.1)").
func ParseVersion(output string) Support {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(output))
	if m == nil {
		return Support{}
	}
	v := m[1] + "." + m[2] + "." + m[3]
	return Support{Version: v, Verified: VerifiedVersions[v]}
}

// ProbeVersion runs `<museBin> --version` (fixed arguments, no shell)
// with a short timeout and parses the result. A failure yields an
// unknown, unverified Support plus the error for diagnostics; callers
// must still attempt collection.
func ProbeVersion(ctx context.Context, runner Runner, museBin string) (Support, error) {
	if museBin == "" {
		museBin = "muse"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := runner(ctx, museBin, "--version")
	if err != nil {
		return Support{}, fmt.Errorf("probe muse version: %w", err)
	}
	return ParseVersion(string(out)), nil
}
