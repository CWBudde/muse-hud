package collect

import (
	"bufio"
	"io"
	"os"
	"time"

	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// Bounded scan budgets for DeriveFile. Session logs can exceed 100 MiB;
// a one-shot HUD print must not parse them whole.
const (
	// DefaultHeadBytes covers the session-start records (workspace,
	// initial model/branch, first timestamp for the session age).
	DefaultHeadBytes = 256 * 1024
	// DefaultTailBytes covers recent activity: running/recent tools,
	// agents, approvals and the latest model/token records.
	DefaultTailBytes = 2 * 1024 * 1024
)

// DeriveFile derives a snapshot from a session log file with a bounded
// read: files up to head+tail bytes are parsed whole (identical to
// Derive); larger files are derived from a head window (session
// metadata) merged with a tail window (recent activity). Tool counts
// and agents on a bounded read therefore describe recent activity only,
// and Stats.Bounded says so — nothing from the unscanned middle is
// presented as a whole-session total.
func DeriveFile(f *os.File, now time.Time, headBytes, tailBytes int64) (snapshot.Snapshot, Stats, error) {
	if headBytes <= 0 {
		headBytes = DefaultHeadBytes
	}
	if tailBytes <= 0 {
		tailBytes = DefaultTailBytes
	}
	fi, err := f.Stat()
	if err != nil {
		// Not stat-able: fall back to a plain stream from the start.
		return Derive(f, now)
	}
	size := fi.Size()
	if size <= headBytes+tailBytes {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return Derive(f, now)
		}
		snap, stats, derr := Derive(f, now)
		stats.FileBytes = size
		stats.ScannedBytes = size
		return snap, stats, derr
	}

	head := NewDeriver()
	headSkipped, herr := scanLines(io.NewSectionReader(f, 0, headBytes), head)
	head.stats.Skipped = headSkipped

	tail := NewDeriver()
	tailSkipped, terr := scanTail(f, size-tailBytes, tailBytes, tail)
	tail.stats.Skipped = tailSkipped

	merged := mergeHeadIntoTail(head, tail)
	stats := merged.Stats()
	stats.Envelopes += head.stats.Envelopes
	stats.Skipped += head.stats.Skipped
	mergeCounts(stats.PayloadCounts, head.stats.PayloadCounts)
	mergeCounts(stats.UnknownPayloadTypes, head.stats.UnknownPayloadTypes)
	stats.FileBytes = size
	stats.ScannedBytes = headBytes + tailBytes
	stats.Bounded = true
	if herr != nil {
		return merged.Snapshot(now), stats, herr
	}
	return merged.Snapshot(now), stats, terr
}

// scanTail feeds a deriver from a file section, discarding the first
// (partial) line so parsing starts on a line boundary.
func scanTail(f *os.File, offset, length int64, d *Deriver) (int, error) {
	br := bufio.NewReaderSize(io.NewSectionReader(f, offset, length), 64*1024)
	if _, err := br.ReadBytes('\n'); err != nil {
		return 0, nil // section held no complete line
	}
	return scanLines(br, d)
}

func mergeCounts(dst, src map[string]int) {
	for k, v := range src {
		dst[k] += v
	}
}

// mergeHeadIntoTail fills the tail deriver's gaps with session metadata
// only the head carries. Recent-activity state (tools, agents,
// approvals, prompt-token estimate, end mark) stays tail-only so a
// bounded read never mixes stale counts into recent ones.
func mergeHeadIntoTail(head, tail *Deriver) *Deriver {
	if !head.first.IsZero() && (tail.first.IsZero() || head.first.Before(tail.first)) {
		tail.first = head.first
	}
	if tail.sessionID == "" {
		tail.sessionID = head.sessionID
	}
	if tail.model == "" {
		tail.model = head.model
	}
	if tail.effort == "" {
		tail.effort = head.effort
	}
	if tail.workspaceRoot == "" {
		tail.workspaceRoot = head.workspaceRoot
	}
	if tail.branch == "" {
		tail.branch = head.branch
		tail.branchDirty = head.branchDirty
	}
	if tail.buildVersion == "" {
		tail.buildVersion = head.buildVersion
	}
	return tail
}
