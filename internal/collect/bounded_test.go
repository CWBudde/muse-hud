package collect_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
)

func envLine(seq int, payloadType, payload string) string {
	return fmt.Sprintf(`{"record_type":"event","sequence":%d,"recorded_at":%d,"payload_type":"%s","payload":%s}`,
		seq, 1762000000000000+int64(seq)*1000000, payloadType, payload) + "\n"
}

// bigSyntheticLog builds a deterministic synthetic log: session
// metadata at the head, a finished middle_tool in the unscanned middle
// (buried in filler), and a running tail_tool plus token estimate at
// the tail.
func bigSyntheticLog(fillerLines int) string {
	var b strings.Builder
	seq := 0
	next := func(payloadType, payload string) {
		seq++
		b.WriteString(envLine(seq, payloadType, payload))
	}
	next("runtime.session.metadata", `{"kind":"metadata","record":{"model_id":"synthetic-model-1","workspace_root":"/example/example-workspace"}}`)
	next("session.workspace_branch.observed", `{"kind":"workspace_branch","record":{"reference":{"kind":"branch","name":"main"},"workspace_root":"/example/example-workspace"}}`)
	next("session.opened.observed", `{"kind":"session_opened","record":{"session_id":"synthetic-session-big"}}`)
	padding := strings.Repeat("synthetic-padding-", 12)
	filler := func(i int) {
		next("runtime.retained_fact", fmt.Sprintf(`{"kind":"retained","record":{"note":"%s%d"}}`, padding, i))
	}
	half := fillerLines / 2
	for i := 0; i < half; i++ {
		filler(i)
	}
	next("tool_batch.effect.started", `{"kind":"tool_batch_effect","record":{"tool_name":"middle_tool","call_id":"c-mid","effect_id":"e-mid"}}`)
	next("tool_batch.effect.terminal", `{"kind":"tool_batch_effect","record":{"call_id":"c-mid","effect_id":"e-mid","outcome":{"kind":"completed"}}}`)
	for i := half; i < fillerLines; i++ {
		filler(i)
	}
	next("runtime.session", `{"kind":"run","event":{"kind":"model_completed","estimated_prompt_tokens":777}}`)
	next("tool_batch.effect.started", `{"kind":"tool_batch_effect","record":{"tool_name":"tail_tool","call_id":"c-tail","effect_id":"e-tail"}}`)
	return b.String()
}

func writeTempLog(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestDeriveFileBoundedHeadTail(t *testing.T) {
	content := bigSyntheticLog(200)
	f := writeTempLog(t, content)
	now := time.UnixMicro(1762000000000000).Add(time.Hour)
	snap, stats, err := collect.DeriveFile(f, now, 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Bounded || stats.ScannedBytes != 2048 || stats.FileBytes != int64(len(content)) {
		t.Errorf("stats bounded=%v scanned=%d file=%d (content %d)", stats.Bounded, stats.ScannedBytes, stats.FileBytes, len(content))
	}
	if stats.FileBytes <= stats.ScannedBytes {
		t.Fatalf("fixture not bigger than the window: %d bytes", stats.FileBytes)
	}
	// Head-only metadata survives the bounded read.
	if snap.Model != "synthetic-model-1" || snap.Workspace != "example-workspace" || snap.Branch != "main" {
		t.Errorf("head metadata lost: %+v", snap)
	}
	if snap.SessionID != "synthetic-session-big" {
		t.Errorf("session id %q", snap.SessionID)
	}
	// Tail activity is present; the middle is honestly absent.
	var names []string
	for _, tool := range snap.Tools {
		names = append(names, tool.Name)
		if tool.Name == "tail_tool" && !tool.Running {
			t.Errorf("tail_tool should be running: %+v", tool)
		}
	}
	if len(names) != 1 || names[0] != "tail_tool" {
		t.Errorf("tools %v, want only tail_tool (middle_tool is outside the window)", names)
	}
	if snap.EstimatedPromptTokens == nil || *snap.EstimatedPromptTokens != 777 {
		t.Errorf("tokens %v, want 777 from the tail", snap.EstimatedPromptTokens)
	}
	// Age spans head first record to now, not just the tail window.
	if snap.Age < 59*time.Minute {
		t.Errorf("age %v, want ~1h from the head timestamp", snap.Age)
	}
	// No percentage is ever derived from the token estimate.
	if snap.ContextPercent != nil {
		t.Errorf("context percent fabricated: %d", *snap.ContextPercent)
	}
}

func TestDeriveFileSmallEqualsFullDerive(t *testing.T) {
	content := bigSyntheticLog(3)
	now := time.UnixMicro(1762000000000000).Add(time.Hour)

	full, _, err := collect.Derive(strings.NewReader(content), now)
	if err != nil {
		t.Fatal(err)
	}
	f := writeTempLog(t, content)
	got, stats, err := collect.DeriveFile(f, now, 4096, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(content)) > 8192 {
		t.Fatalf("small fixture is %d bytes, want <= 8192 for this test", len(content))
	}
	if stats.Bounded {
		t.Error("small file must be a full parse, not bounded")
	}
	if got.Model != full.Model || len(got.Tools) != len(full.Tools) {
		t.Errorf("bounded-path full parse differs: %+v vs %+v", got, full)
	}
	// The full parse sees the middle tool the bounded read must skip.
	found := false
	for _, tool := range full.Tools {
		if tool.Name == "middle_tool" {
			found = true
		}
	}
	if !found {
		t.Errorf("full derive lost middle_tool: %+v", full.Tools)
	}
}

// BenchmarkDeriveFile10MB measures the Phase 2 gate shape: a ~10 MiB
// synthetic log derived through the default bounded head+tail window.
func BenchmarkDeriveFile10MB(b *testing.B) {
	content := bigSyntheticLog(30000)
	path := filepath.Join(b.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
	now := time.UnixMicro(1762000000000000).Add(time.Hour)
	b.ReportMetric(float64(len(content))/1024/1024, "MiB-log")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		_, stats, derr := collect.DeriveFile(f, now, 0, 0)
		_ = f.Close()
		if derr != nil || !stats.Bounded {
			b.Fatalf("derive: err=%v bounded=%v", derr, stats.Bounded)
		}
	}
}
