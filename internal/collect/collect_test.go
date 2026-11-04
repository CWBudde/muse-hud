package collect_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CWBudde/muse-hud/internal/collect"
	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
)

func TestStubSnapshotIsDeterministicAndSourced(t *testing.T) {
	a := render.String(collect.StubSnapshot(), config.Default())
	b := render.String(collect.StubSnapshot(), config.Default())
	if a != b {
		t.Errorf("stub render not stable:\n%s\n---\n%s", a, b)
	}
	if a == "" {
		t.Fatal("stub render empty")
	}
	s := collect.StubSnapshot()
	for _, group := range []string{"model", "workspace", "branch", "tools", "todos"} {
		if !s.Sourced(group) {
			t.Errorf("stub group %q not sourced", group)
		}
	}
	if s.ContextPercent != nil {
		t.Error("stub must not fabricate a context percentage")
	}
}

func TestScanSyntheticFixture(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture (regenerate with `make fixture`): %v", err)
	}
	defer f.Close()

	envs, skipped, err := collect.ScanEnvelopes(f)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Errorf("skipped %d lines, want 0", skipped)
	}
	if len(envs) != 16 {
		t.Fatalf("got %d envelopes, want 16", len(envs))
	}
	wantTypes := []string{
		"session.started", "runtime.session.metadata",
		"run.model.configured", "session.workspace_branch.observed",
		"session.opened.observed", "runtime.session",
		"tool_batch.effect.started", "tool_batch.effect.terminal",
		"runtime.session", "runtime.session", "runtime.session",
		"subagent.control.spawn_accepted", "subagent.control.status_updated",
		"runtime.session", "future.example.observed",
		"tool_batch.effect.started",
	}
	for i, env := range envs {
		if env.RecordType != "event" {
			t.Errorf("line %d record_type %q, want event", i, env.RecordType)
		}
		if env.Sequence != int64(i+1) {
			t.Errorf("line %d sequence %d, want %d", i, env.Sequence, i+1)
		}
		if env.PayloadType != wantTypes[i] {
			t.Errorf("line %d payload_type %q, want %q", i, env.PayloadType, wantTypes[i])
		}
		p, err := env.DecodePayload()
		if err != nil {
			t.Errorf("line %d: %v", i, err)
		}
		if p.Kind == "" {
			t.Errorf("line %d payload kind empty", i)
		}
	}
}

func TestScanSkipsCorruptAndPartialLines(t *testing.T) {
	in := strings.NewReader(
		"{\"record_type\":\"event\",\"sequence\":1,\"payload_type\":\"session.started\",\"payload\":{\"kind\":\"session_started\",\"record\":{}}}\n" +
			"not json\n" +
			"\n" +
			"{\"record_type\":\"event\",\"sequence\":2",
	)
	envs, skipped, err := collect.ScanEnvelopes(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || skipped != 2 {
		t.Errorf("got %d envelopes, %d skipped; want 1 and 2", len(envs), skipped)
	}
}
