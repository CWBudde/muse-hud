package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func driftFixturePath() string {
	return filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session-drift.jsonl")
}

// TestDoctorDriftFixtureHints checks the Phase 5 compatibility
// behaviour end to end: a drifted log (build 9.9.9, renamed and new
// payload types) degrades to fewer segments, and doctor names the
// support table, each unknown type, and its closest known type.
func TestDoctorDriftFixtureHints(t *testing.T) {
	hermeticEnv(t)
	var out, errBuf bytes.Buffer
	if code := run([]string{"doctor", "--file", driftFixturePath()}, &out, &errBuf); code != 0 {
		t.Fatalf("doctor exit %d, stderr %q\n%s", code, errBuf.String(), out.String())
	}
	got := out.String()
	for _, want := range []string{
		"muse: 9.9.9 (UNVERIFIED",
		"support: verified Muse versions: 1.4.14 (docs/compatibility.md)",
		"unknown payload_type values (ignored, format drift?):",
		"tool_batch.effect.begun×1",
		"? tool_batch.effect.begun (×1):",
		`resembles known "tool_batch.effect.started"`,
		"? future.hologram.observed (×1): new payload family",
		"unknown types are never guessed at",
		"model: shown (synthetic-model-9, source jsonl, unverified muse version)",
		"branch: shown (drift-branch",
		"tools: hidden — no tool_batch.effect records in the scanned window",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("doctor to a pipe must be plain:\n%q", got)
	}

	// print on the same drifted log still exits 0, shows only the
	// sourced model/branch line, and invents no tools line.
	var pout bytes.Buffer
	if code := run([]string{"print", "--file", driftFixturePath()}, &pout, &errBuf); code != 0 {
		t.Fatalf("print exit %d, want 0", code)
	}
	if s := pout.String(); !strings.Contains(s, "[synthetic-model-9]") || strings.Contains(s, "bash") {
		t.Errorf("print on drifted log = %q, want model line without guessed tools", s)
	}
}
