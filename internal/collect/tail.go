package collect

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// Tailer incrementally tails one session.jsonl: it seeds a Deriver from
// the file as it exists at open (bounded head+tail window for large
// logs, same semantics as DeriveFile), then each Poll feeds only the
// newly appended, newline-terminated lines. A partial final line — a
// record Muse is still writing — is buffered, never fed, until its
// newline lands. Truncation (size shrinks below the tracked offset)
// and rotation/replacement (the path now names a different file)
// reset the derivation from scratch, so state from a dead incarnation
// of the log is never mixed into the new one.
//
// Polling is deliberate (PLAN Phase 3): no file-notify dependency. See
// docs/spikes/e-watch-polling-terminal.md.
type Tailer struct {
	path      string
	headBytes int64
	tailBytes int64

	deriver *Deriver
	offset  int64 // bytes consumed from the current file incarnation
	pending []byte
	prev    os.FileInfo
	bounded bool
	resets  int
}

// NewTailer opens path and seeds the derivation from its current
// contents. headBytes/tailBytes <= 0 use the DeriveFile defaults.
func NewTailer(path string, headBytes, tailBytes int64) (*Tailer, error) {
	if headBytes <= 0 {
		headBytes = DefaultHeadBytes
	}
	if tailBytes <= 0 {
		tailBytes = DefaultTailBytes
	}
	t := &Tailer{path: path, headBytes: headBytes, tailBytes: tailBytes}
	if err := t.seed(); err != nil {
		return nil, err
	}
	return t, nil
}

// Path returns the tailed file path.
func (t *Tailer) Path() string { return t.path }

// Bounded reports whether the seed used the bounded head+tail window
// (tool/agent state then covers the tail window only, as in print).
func (t *Tailer) Bounded() bool { return t.bounded }

// Resets counts truncation/rotation resets (the initial seed is 0).
func (t *Tailer) Resets() int { return t.resets }

// Stats returns the deriver's scan statistics.
func (t *Tailer) Stats() Stats {
	st := t.deriver.Stats()
	st.Bounded = t.bounded
	return st
}

// Snapshot builds the current snapshot as of now.
func (t *Tailer) Snapshot(now time.Time) snapshot.Snapshot {
	return t.deriver.Snapshot(now)
}

// seed (re)builds the derivation from the file's current contents,
// consuming only newline-terminated lines; a trailing partial line
// becomes pending.
func (t *Tailer) seed() error {
	f, err := os.Open(t.path)
	if err != nil {
		return fmt.Errorf("open session log: %w", err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat session log: %w", err)
	}
	size := fi.Size()
	effectiveEnd, err := lastNewlineEnd(f, size)
	if err != nil {
		return err
	}
	pending, err := readRange(f, effectiveEnd, size)
	if err != nil {
		return err
	}

	t.bounded = effectiveEnd > t.headBytes+t.tailBytes
	if !t.bounded {
		d := NewDeriver()
		skipped, serr := scanLines(io.NewSectionReader(f, 0, effectiveEnd), d)
		d.stats.Skipped = skipped
		d.stats.FileBytes = size
		d.stats.ScannedBytes = effectiveEnd
		t.deriver = d
		if serr != nil {
			return serr
		}
	} else {
		head := NewDeriver()
		headSkipped, herr := scanLines(io.NewSectionReader(f, 0, t.headBytes), head)
		head.stats.Skipped = headSkipped

		tail := NewDeriver()
		tailSkipped, terr := scanTail(f, effectiveEnd-t.tailBytes, t.tailBytes, tail)
		tail.stats.Skipped = tailSkipped

		merged := mergeHeadIntoTail(head, tail)
		merged.stats.Envelopes += head.stats.Envelopes
		merged.stats.Skipped += head.stats.Skipped
		mergeCounts(merged.stats.PayloadCounts, head.stats.PayloadCounts)
		mergeCounts(merged.stats.UnknownPayloadTypes, head.stats.UnknownPayloadTypes)
		merged.stats.FileBytes = size
		merged.stats.ScannedBytes = t.headBytes + t.tailBytes
		merged.stats.Bounded = true
		t.deriver = merged
		if herr != nil {
			return herr
		}
		if terr != nil {
			return terr
		}
	}
	t.offset = size
	t.pending = pending
	t.prev = fi
	return nil
}

// Poll consumes any newly appended complete lines. It reports whether
// the derivation changed (new lines fed, or a reset). A missing file
// is an error the caller can ride out (rotation gap); the previous
// derivation is kept untouched in that case.
func (t *Tailer) Poll() (bool, error) {
	fi, err := os.Stat(t.path)
	if err != nil {
		return false, fmt.Errorf("stat session log: %w", err)
	}
	if t.prev != nil && !os.SameFile(t.prev, fi) {
		// Rotation/replacement: the path names a new file.
		if err := t.seed(); err != nil {
			return false, err
		}
		t.resets++
		return true, nil
	}
	if fi.Size() < t.offset {
		// Truncation (or a same-inode rewrite): start over rather
		// than keep state from bytes that no longer exist.
		if err := t.seed(); err != nil {
			return false, err
		}
		t.resets++
		return true, nil
	}
	t.prev = fi
	if fi.Size() == t.offset {
		return false, nil
	}
	f, err := os.Open(t.path)
	if err != nil {
		return false, fmt.Errorf("open session log: %w", err)
	}
	defer func() { _ = f.Close() }()
	section := io.NewSectionReader(f, t.offset, fi.Size()-t.offset)
	changed, read, rerr := t.feed(section)
	t.offset += read
	return changed, rerr
}

// feed reads complete lines from r into the deriver, buffering a
// trailing partial line in pending. It returns whether any line was
// fed and how many bytes were consumed from r.
func (t *Tailer) feed(r io.Reader) (bool, int64, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var consumed int64
	changed := false
	for {
		chunk, err := br.ReadBytes('\n')
		consumed += int64(len(chunk))
		if err == io.EOF {
			if len(chunk) > 0 {
				t.pending = append(t.pending, chunk...)
			}
			return changed, consumed, nil
		}
		if err != nil {
			return changed, consumed, fmt.Errorf("read session log: %w", err)
		}
		line := chunk
		if len(t.pending) > 0 {
			line = append(t.pending, chunk...)
			t.pending = nil
		}
		if len(trimSpaceBytes(line)) > 0 {
			if !t.deriver.AddLine(line) {
				t.deriver.stats.Skipped++
			}
		}
		changed = true
	}
}

// lastNewlineEnd returns the offset just past the file's last newline
// (0 when the file holds no newline), scanning backwards in chunks.
func lastNewlineEnd(f *os.File, size int64) (int64, error) {
	const chunkSize = 64 * 1024
	buf := make([]byte, chunkSize)
	end := size
	for end > 0 {
		start := end - chunkSize
		if start < 0 {
			start = 0
		}
		n := end - start
		if _, err := f.ReadAt(buf[:n], start); err != nil && err != io.EOF {
			return 0, fmt.Errorf("read session log: %w", err)
		}
		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return start + i + 1, nil
			}
		}
		end = start
	}
	return 0, nil
}

func readRange(f *os.File, start, end int64) ([]byte, error) {
	if end <= start {
		return nil, nil
	}
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, fmt.Errorf("read session log: %w", err)
	}
	return buf, nil
}

// ResolveSession is the exported form of the discovery Collect uses
// (workspace match with fallback to the newest session overall), for
// the watch loop's periodic re-discovery.
func ResolveSession(opts Options) (Session, bool, error) {
	return discover(opts)
}
