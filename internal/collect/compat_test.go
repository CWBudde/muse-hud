package collect_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
)

func TestDriftHintsClassifyUnknownPayloadTypes(t *testing.T) {
	hints := collect.DriftHints(map[string]int{
		"tool_batch.effect.begun":  3,
		"future.hologram.observed": 1,
		"runtime.future.thing":     2,
	})
	if len(hints) != 3 {
		t.Fatalf("got %d hints: %+v", len(hints), hints)
	}
	// Sorted by unknown type.
	if hints[0].Unknown != "future.hologram.observed" || hints[2].Unknown != "tool_batch.effect.begun" {
		t.Errorf("hints not sorted: %+v", hints)
	}
	byName := map[string]collect.DriftHint{}
	for _, h := range hints {
		byName[h.Unknown] = h
	}
	begun := byName["tool_batch.effect.begun"]
	if begun.Count != 3 || begun.Closest != "tool_batch.effect.started" {
		t.Errorf("begun hint %+v, want closest tool_batch.effect.started ×3", begun)
	}
	if !strings.Contains(begun.Hint, "rename or new variant") {
		t.Errorf("begun hint text %q", begun.Hint)
	}
	holo := byName["future.hologram.observed"]
	if holo.Closest != "" || !strings.Contains(holo.Hint, "new payload family") {
		t.Errorf("hologram hint %+v, want new-family hint", holo)
	}
	rt := byName["runtime.future.thing"]
	if rt.Closest == "" || !strings.HasPrefix(rt.Closest, "runtime.") {
		t.Errorf("runtime hint %+v, want a runtime.* closest", rt)
	}
	if got := collect.DriftHints(nil); len(got) != 0 {
		t.Errorf("nil unknowns gave %+v", got)
	}
}

func TestKnownPayloadTypesSortedAndContainsDispatched(t *testing.T) {
	known := collect.KnownPayloadTypes()
	for i := 1; i < len(known); i++ {
		if known[i-1] >= known[i] {
			t.Fatalf("known types not strictly sorted at %d: %q >= %q", i, known[i-1], known[i])
		}
	}
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	for _, dispatched := range []string{
		"run.model.configured", "runtime.session.metadata",
		"session.workspace_branch.observed", "session.started",
		"session.opened.observed", "runtime.session_start.intake_v3",
		"runtime.command_intake.received", "session.end",
		"tool_batch.effect.started", "tool_batch.effect.terminal",
		"runtime.session",
	} {
		if !set[dispatched] {
			t.Errorf("dispatched payload type %q missing from the known table", dispatched)
		}
	}
	if got := collect.VerifiedVersionList(); len(got) != 1 || got[0] != "1.4.14" {
		t.Errorf("verified versions %v, want [1.4.14]", got)
	}
}

// TestDeriveDriftFixtureDegradesHonestly replays the synthetic
// drifted log shape (build 9.9.9, a renamed tool payload type, a new
// payload family): known facts are still derived, the segment fed by
// the renamed type stays hidden instead of being guessed, and every
// unknown type is counted for doctor.
func TestDeriveDriftFixtureDegradesHonestly(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session-drift.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	snap, stats, err := collect.Derive(f, time.Unix(1762003600, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Model != "synthetic-model-9" || snap.MuseVersion != "9.9.9" {
		t.Errorf("model/version %q/%q from drifted log", snap.Model, snap.MuseVersion)
	}
	if snap.Branch != "drift-branch" {
		t.Errorf("branch %q, want drift-branch", snap.Branch)
	}
	if len(snap.Tools) != 0 {
		t.Errorf("tools %v: the renamed tool_batch.effect.begun must not be guessed as a tool start", snap.Tools)
	}
	for _, unknown := range []string{"tool_batch.effect.begun", "future.hologram.observed", "runtime.future.thing"} {
		if stats.UnknownPayloadTypes[unknown] != 1 {
			t.Errorf("unknown count for %q = %d (%v)", unknown, stats.UnknownPayloadTypes[unknown], stats.UnknownPayloadTypes)
		}
	}
}
