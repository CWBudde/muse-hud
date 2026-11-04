package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// UsageReport is the subset of `muse usage --json` muse-hud consumes
// (verified against Muse 1.4.14): the subscription allowance. This is
// account usage, NOT live session context (spike a).
type UsageReport struct {
	SchemaVersion int `json:"schemaVersion"`
	Allowance     *struct {
		Tier   string       `json:"tier"`
		Window *UsageWindow `json:"window"`
		Weekly *UsageWindow `json:"weekly"`
	} `json:"allowance"`
	Hint   *string `json:"hint"`
	Reason *string `json:"reason"`
}

// UsageWindow is one allowance window. UsedPercent is a pointer so a
// missing value stays distinguishable from a real 0.
type UsageWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	ResetsAtMs  *int64   `json:"resetsAtMs"`
}

// ParseUsage parses `muse usage --json` output.
func ParseUsage(data []byte) (*UsageReport, error) {
	var rep UsageReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("parse usage output: %w", err)
	}
	return &rep, nil
}

// Usage runs `muse usage --json`. Callers opt in explicitly: it is a
// subprocess against Muse's own allowance surface, not needed for the
// default log-derived snapshot.
func Usage(ctx context.Context, runner Runner, museBin string) (*UsageReport, error) {
	if museBin == "" {
		museBin = "muse"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := runner(ctx, museBin, "usage", "--json")
	if err != nil {
		return nil, fmt.Errorf("run muse usage: %w", err)
	}
	return ParseUsage(out)
}

// FormatUsage renders the sourced allowance windows as one HUD segment,
// e.g. "Usage 28% window · 55% weekly". Windows without a reported
// percentage are omitted; an empty report yields "" (segment hidden).
func FormatUsage(rep *UsageReport) string {
	if rep == nil || rep.Allowance == nil {
		return ""
	}
	var parts []string
	if w := rep.Allowance.Window; w != nil && w.UsedPercent != nil {
		parts = append(parts, fmt.Sprintf("%g%% window", *w.UsedPercent))
	}
	if w := rep.Allowance.Weekly; w != nil && w.UsedPercent != nil {
		parts = append(parts, fmt.Sprintf("%g%% weekly", *w.UsedPercent))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Usage " + strings.Join(parts, " · ")
}
