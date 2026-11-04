package collect_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
)

// Fuzz seed corpus runs as ordinary tests under `go test`; the same
// targets run as fuzzers with e.g. `go test ./internal/collect -run x
// -fuzz FuzzDerive -fuzztime=10s` (see docs/security.md). Every target
// asserts the parser never panics and its honesty invariants hold:
// no context percentage is ever derived from a raw log, and every
// populated snapshot group records a source.

func seedLogLines(f *testing.F) {
	f.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session.jsonl"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	drift, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session-drift.jsonl"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(drift)
	f.Add([]byte(""))
	f.Add([]byte("\n\n"))
	f.Add([]byte("not json at all\n"))
	f.Add([]byte(`{"record_type":"event","sequence":1,"recorded_at":1762000000000000,"payload_type":"future.example.observed","payload":{"kind":"k","record":{}}}` + "\n"))
	f.Add([]byte(`{"record_type":"event","sequence":-1,"recorded_at":-9223372036854775808,"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"model_completed","estimated_prompt_tokens":-5}}}` + "\n"))
	f.Add([]byte(`{"record_type":"event","sequence":1,"recorded_at":999999999999999999,"payload_type":"tool_batch.effect.started","payload":{"kind":"tool_batch_effect","record":{"kind":"started","tool_name":"bash","call_id":"c","effect_id":"e"}}}` + "\n"))
	f.Add([]byte(`{"record_type":"event",`)) // partial final line, no newline
}

var fuzzNow = time.Unix(1762003600, 0).UTC()

func FuzzDerive(f *testing.F) {
	seedLogLines(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		// Bound input so a fuzz iteration stays fast; Derive itself
		// streams, the bound is only about the test budget.
		if len(data) > 1<<20 {
			data = data[:1<<20]
		}
		snap, stats, _ := collect.Derive(bytes.NewReader(data), fuzzNow)
		if snap.ContextPercent != nil {
			t.Errorf("raw log derived a context percentage %d — must stay nil (never fabricate)", *snap.ContextPercent)
		}
		if snap.Sources == nil {
			t.Error("snapshot Sources is nil")
		}
		if stats.Envelopes < 0 || stats.Skipped < 0 {
			t.Errorf("negative stats %+v", stats)
		}
		if snap.Model != "" && !snap.Sourced("model") {
			t.Errorf("model %q without a source", snap.Model)
		}
		if snap.Branch != "" && !snap.Sourced("branch") {
			t.Errorf("branch %q without a source", snap.Branch)
		}
		if snap.Age < 0 {
			t.Errorf("negative age %v", snap.Age)
		}
		for _, tool := range snap.Tools {
			if tool.Count < 0 || tool.Elapsed < 0 {
				t.Errorf("bad tool %+v", tool)
			}
		}
		// Drift hints must survive arbitrary unknown-type sets.
		_ = collect.DriftHints(stats.UnknownPayloadTypes)
	})
}

func FuzzAddLine(f *testing.F) {
	seedLogLines(f)
	f.Add([]byte(`{"record_type":[],"payload_type":{},"recorded_at":"x"}`))
	f.Add([]byte{0x00, 0xff, 0xfe, '{', '}'})
	f.Fuzz(func(t *testing.T, line []byte) {
		d := collect.NewDeriver()
		_ = d.AddLine(line)
		_ = d.Snapshot(fuzzNow)
		// The whole-stream scanner must agree the input is handled.
		_, _, _ = collect.Derive(strings.NewReader(string(line)), fuzzNow)
	})
}

func FuzzParseAdapters(f *testing.F) {
	for _, name := range []string{"trace.json", "usage.json", "export.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte("null"))
	f.Add([]byte("{}"))
	f.Add([]byte(`{"allowance":{"window":{"usedPercent":1e999}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if rep, err := collect.ParseTrace(data); err == nil {
			_ = rep.ToolTasks()
			_ = rep.HasUsage()
			_ = rep.PayloadCount("runtime.session")
		}
		if rep, err := collect.ParseUsage(data); err == nil {
			_ = collect.FormatUsage(rep)
		}
		_, _ = collect.ParseExport(data)
		_ = collect.ParseVersion(string(data))
	})
}
