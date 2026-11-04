package collect_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
)

// Tail tests use the same synthetic envelope shapes as envLine
// (bounded_test.go): base 1762000000000000 µs, +1s per sequence.
var tailNow = time.UnixMicro(1762000000000000).Add(time.Hour)

func toolStartedLine(seq int, name, effect string) string {
	return envLine(seq, "tool_batch.effect.started",
		`{"kind":"tool_batch_effect","record":{"kind":"started","tool_name":"`+name+`","call_id":"c-`+effect+`","effect_id":"`+effect+`"}}`)
}

func toolTerminalLine(seq int, effect, outcome string) string {
	return envLine(seq, "tool_batch.effect.terminal",
		`{"kind":"tool_batch_effect","record":{"kind":"terminal","call_id":"c-`+effect+`","effect_id":"`+effect+`","outcome":{"kind":"`+outcome+`"}}}`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTailerAppendStartAndEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeFile(t, path, toolStartedLine(1, "bash", "e1"))

	tailer, err := collect.NewTailer(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	snap := tailer.Snapshot(tailNow)
	if len(snap.Tools) != 1 || !snap.Tools[0].Running || snap.Tools[0].Name != "bash" {
		t.Fatalf("seed tools %+v, want running bash", snap.Tools)
	}

	changed, err := tailer.Poll()
	if err != nil || changed {
		t.Errorf("idle poll changed=%v err=%v, want false/nil", changed, err)
	}

	appendFile(t, path, toolTerminalLine(2, "e1", "completed"))
	changed, err = tailer.Poll()
	if err != nil || !changed {
		t.Fatalf("append poll changed=%v err=%v, want true/nil", changed, err)
	}
	snap = tailer.Snapshot(tailNow)
	if len(snap.Tools) != 1 || snap.Tools[0].Running || snap.Tools[0].Count != 1 {
		t.Fatalf("tools %+v, want finished bash ×1", snap.Tools)
	}
	if tailer.Resets() != 0 {
		t.Errorf("resets %d, want 0", tailer.Resets())
	}
}

func TestTailerPartialLineBufferedUntilNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	line := toolStartedLine(1, "bash", "e1")
	writeFile(t, path, line[:len(line)-1]) // no trailing newline yet

	tailer, err := collect.NewTailer(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if snap := tailer.Snapshot(tailNow); len(snap.Tools) != 0 {
		t.Fatalf("partial line fed early: %+v", snap.Tools)
	}

	// More partial bytes (still no newline) change nothing.
	appendFile(t, path, "")
	if changed, err := tailer.Poll(); err != nil || changed {
		t.Errorf("empty append poll changed=%v err=%v", changed, err)
	}

	appendFile(t, path, "\n")
	changed, err := tailer.Poll()
	if err != nil || !changed {
		t.Fatalf("newline poll changed=%v err=%v", changed, err)
	}
	snap := tailer.Snapshot(tailNow)
	if len(snap.Tools) != 1 || !snap.Tools[0].Running {
		t.Fatalf("tools %+v, want running bash after newline", snap.Tools)
	}
}

func TestTailerTruncationResets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeFile(t, path, toolStartedLine(1, "bash", "e1")+toolTerminalLine(2, "e1", "completed"))

	tailer, err := collect.NewTailer(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if snap := tailer.Snapshot(tailNow); len(snap.Tools) != 1 {
		t.Fatalf("seed tools %+v", snap.Tools)
	}

	// Same file, truncated and rewritten shorter with only read_file.
	writeFile(t, path, toolStartedLine(1, "read_file", "e9"))
	changed, err := tailer.Poll()
	if err != nil || !changed {
		t.Fatalf("truncate poll changed=%v err=%v", changed, err)
	}
	if tailer.Resets() != 1 {
		t.Errorf("resets %d, want 1", tailer.Resets())
	}
	snap := tailer.Snapshot(tailNow)
	if len(snap.Tools) != 1 || snap.Tools[0].Name != "read_file" || !snap.Tools[0].Running {
		t.Fatalf("tools %+v, want only running read_file (no stale bash)", snap.Tools)
	}
}

func TestTailerRotationReplacesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeFile(t, path, toolStartedLine(1, "bash", "e1"))

	tailer, err := collect.NewTailer(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Rotate: rename the old log away and create a fresh one (new inode).
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, toolStartedLine(1, "web_fetch", "e2")+toolTerminalLine(2, "e2", "failed"))
	changed, err := tailer.Poll()
	if err != nil || !changed {
		t.Fatalf("rotation poll changed=%v err=%v", changed, err)
	}
	if tailer.Resets() != 1 {
		t.Errorf("resets %d, want 1", tailer.Resets())
	}
	snap := tailer.Snapshot(tailNow)
	if len(snap.Tools) != 1 || snap.Tools[0].Name != "web_fetch" || !snap.Tools[0].Failed {
		t.Fatalf("tools %+v, want failed web_fetch only", snap.Tools)
	}
}

func TestTailerMissingFilePollErrorsAndKeepsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeFile(t, path, toolStartedLine(1, "bash", "e1"))
	tailer, err := collect.NewTailer(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := tailer.Poll(); err == nil {
		t.Fatal("poll on missing file: want error")
	}
	if snap := tailer.Snapshot(tailNow); len(snap.Tools) != 1 {
		t.Errorf("state lost on missing file: %+v", snap.Tools)
	}
	// The file reappears (new inode): rotation path recovers.
	writeFile(t, path, toolStartedLine(1, "bash", "e1")+toolTerminalLine(2, "e1", "completed"))
	if changed, err := tailer.Poll(); err != nil || !changed {
		t.Errorf("recovery poll changed=%v err=%v", changed, err)
	}
}

func TestTailerBoundedSeedMatchesDeriveFile(t *testing.T) {
	// Head+tail smaller than the log: metadata from the head, the
	// middle tool honestly absent, tail tool present (same contract
	// as DeriveFile/bounded_test).
	var b strings.Builder
	b.WriteString(envLine(1, "runtime.session.metadata",
		`{"kind":"metadata","record":{"model_id":"synthetic-model-1","workspace_root":"/example/example-workspace","build":{"semver":"1.4.14"}}}`))
	seq := 2
	b.WriteString(toolStartedLine(seq, "middle_tool", "em"))
	seq++
	b.WriteString(toolTerminalLine(seq, "em", "completed"))
	seq++
	for i := 0; i < 300; i++ {
		b.WriteString(envLine(seq, "runtime.retained_fact",
			`{"kind":"retained","record":{"note":"synthetic-padding-synthetic-padding-synthetic-padding-synthetic-padding"}}`))
		seq++
	}
	b.WriteString(toolStartedLine(seq, "tail_tool", "et"))
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeFile(t, path, b.String())

	tailer, err := collect.NewTailer(path, 512, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !tailer.Bounded() {
		t.Fatal("want bounded seed")
	}
	snap := tailer.Snapshot(tailNow)
	if snap.Model != "synthetic-model-1" || snap.WorkspaceRoot != "/example/example-workspace" {
		t.Errorf("head metadata lost: %+v", snap)
	}
	for _, tool := range snap.Tools {
		if tool.Name == "middle_tool" {
			t.Errorf("middle tool must be honestly absent on a bounded seed: %+v", snap.Tools)
		}
	}
	found := false
	for _, tool := range snap.Tools {
		if tool.Name == "tail_tool" && tool.Running {
			found = true
		}
	}
	if !found {
		t.Errorf("tail tool missing: %+v", snap.Tools)
	}

	// Appends after a bounded seed are still tracked incrementally.
	appendFile(t, path, toolTerminalLine(9999, "et", "completed"))
	if changed, err := tailer.Poll(); err != nil || !changed {
		t.Fatalf("post-seed append poll changed=%v err=%v", changed, err)
	}
	snap = tailer.Snapshot(tailNow)
	for _, tool := range snap.Tools {
		if tool.Name == "tail_tool" && !tool.Running && tool.Count == 1 {
			return
		}
	}
	t.Errorf("tail_tool not finished after append: %+v", snap.Tools)
}
