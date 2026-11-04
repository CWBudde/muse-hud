package watch_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/watch"
)

// Synthetic timestamps: base aligned to a fixed synthetic "now" so
// ages/elapsed are deterministic; the integration loop advances the
// injected clock by the real poll interval it waits.
var base = time.UnixMicro(1762000000000000).UTC()

func lineAt(at time.Time, seq int, payloadType, payload string) string {
	return fmt.Sprintf(`{"record_type":"event","sequence":%d,"recorded_at":%d,"payload_type":%q,"payload":%s}`,
		seq, at.UnixMicro(), payloadType, payload) + "\n"
}

func seedLog(at time.Time) string {
	var b strings.Builder
	b.WriteString(lineAt(at, 1, "runtime.session.metadata",
		`{"kind":"metadata","record":{"model_id":"synthetic-model-1","workspace_root":"/example/example-workspace","build":{"semver":"1.4.14"}}}`))
	b.WriteString(lineAt(at.Add(time.Second), 2, "session.opened.observed",
		`{"kind":"session_opened","record":{"session_id":"synthetic-session-watch"}}`))
	return b.String()
}

func toolStarted(at time.Time, seq int, name, effect string) string {
	return lineAt(at, seq, "tool_batch.effect.started",
		`{"kind":"tool_batch_effect","record":{"kind":"started","tool_name":"`+name+`","call_id":"c-`+effect+`","effect_id":"`+effect+`"}}`)
}

func toolTerminal(at time.Time, seq int, effect, outcome string) string {
	return lineAt(at, seq, "tool_batch.effect.terminal",
		`{"kind":"tool_batch_effect","record":{"kind":"terminal","call_id":"c-`+effect+`","effect_id":"`+effect+`","outcome":{"kind":"`+outcome+`"}}}`)
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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

func newFileModel(t *testing.T, path string, long time.Duration) *watch.Model {
	t.Helper()
	return watch.New(watch.Options{
		Collect:          collect.Options{File: path, SkipGit: true},
		Config:           config.Default(),
		SkipGit:          true,
		LongRunning:      long,
		DiscoverInterval: 10 * time.Millisecond,
	})
}

func viewText(m *watch.Model) string {
	return strings.Join(m.View(render.Options{}), "\n")
}

// TestWatchIntegrationAppendsWithinSecond is the automatable half of
// the Phase 3 gate: synthetic tool-start, tool-end, write_todos and
// approval events appended to a fixture log become visible in the
// watch model (and its rendered frame) within ~1s of polling.
func TestWatchIntegrationAppendsWithinSecond(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic-session-watch", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(seedLog(base)), 0o644); err != nil {
		t.Fatal(err)
	}
	model := newFileModel(t, path, 30*time.Second)
	ctx := context.Background()
	now := base.Add(2 * time.Minute)
	model.Update(ctx, now)
	if !strings.Contains(viewText(model), "session synthetic-session-watch") {
		t.Fatalf("header missing session id:\n%s", viewText(model))
	}
	if !strings.Contains(viewText(model), "age 2m") {
		t.Fatalf("header missing age:\n%s", viewText(model))
	}

	// pollUntil polls the model at a 10ms cadence like the real loop
	// and fails if want is not observed within 1s of wall time.
	pollUntil := func(want string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			now = now.Add(10 * time.Millisecond)
			model.Update(ctx, now)
			if strings.Contains(viewText(model), want) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("did not observe %q within 1s; view:\n%s", want, viewText(model))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Tool starting appears.
	appendTo(t, path, toolStarted(now, 3, "bash", "e1"))
	pollUntil("◐ bash")

	// write_todos is a tool-start like any other on 1.4.14: it shows
	// as a finished tool, and the todos line honestly stays hidden
	// (field cut, spike b) — never fabricated from the tool name.
	appendTo(t, path, toolStarted(now, 4, "write_todos", "e2")+toolTerminal(now, 5, "e2", "completed"))
	pollUntil("✓ write_todos")
	if model.Snapshot().Todos != nil {
		t.Errorf("todos must stay nil (cut, spike b), got %+v", model.Snapshot().Todos)
	}
	if strings.Contains(viewText(model), "▸") {
		t.Errorf("todos line fabricated:\n%s", viewText(model))
	}

	// Tool finishing (failed) appears, with a failed attention cue.
	appendTo(t, path, toolTerminal(now, 6, "e1", "failed"))
	pollUntil("✗ bash")
	pollUntil("attention: ✗ failed: bash")

	// A pending approval appears with its cue; resolving it clears both.
	appendTo(t, path, lineAt(now, 7, "runtime.session",
		`{"kind":"approval","event":{"kind":"requested","pending_action_id":"synthetic-approval-1"}}`))
	pollUntil("⚠ 1 approval pending")
	appendTo(t, path, lineAt(now, 8, "runtime.session",
		`{"kind":"approval","event":{"kind":"decision_applied","pending_action_id":"synthetic-approval-1"}}`))
	deadline := time.Now().Add(time.Second)
	for strings.Contains(viewText(model), "approval pending") {
		now = now.Add(10 * time.Millisecond)
		model.Update(ctx, now)
		if time.Now().After(deadline) {
			t.Fatalf("approval cue not cleared within 1s:\n%s", viewText(model))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatchLongRunningAndFinishedAgentCues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := seedLog(base) +
		toolStarted(base.Add(2*time.Second), 3, "bash", "e1") +
		lineAt(base.Add(3*time.Second), 4, "subagent.control.spawn_accepted",
			`{"kind":"subagent_control","record":{"kind":"spawn_accepted","subagent_id":"synthetic-sub-1","role":"explore"}}`)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	model := newFileModel(t, path, 30*time.Second)
	ctx := context.Background()

	// Two minutes in: bash is long-running past the 30s threshold.
	model.Update(ctx, base.Add(2*time.Minute))
	found := false
	for _, cue := range model.Attention() {
		if cue.Kind == "long-running" && strings.Contains(cue.Text, "bash") {
			found = true
		}
	}
	if !found {
		t.Fatalf("long-running cue missing: %+v\n%s", model.Attention(), viewText(model))
	}

	// The agent finishing is observed as a transition cue.
	appendTo(t, path, lineAt(base.Add(2*time.Minute), 5, "subagent.control.result_ready",
		`{"kind":"subagent_control","record":{"kind":"result_ready","subagent_id":"synthetic-sub-1"}}`))
	model.Update(ctx, base.Add(2*time.Minute+time.Second))
	found = false
	for _, cue := range model.Attention() {
		if cue.Kind == "finished" && strings.Contains(cue.Text, "explore") {
			found = true
		}
	}
	if !found {
		t.Fatalf("finished-agent cue missing: %+v\n%s", model.Attention(), viewText(model))
	}
	// …and the cue expires instead of staying on screen forever.
	model.Update(ctx, base.Add(10*time.Minute))
	for _, cue := range model.Attention() {
		if cue.Kind == "finished" {
			t.Errorf("finished cue should have expired: %+v", model.Attention())
		}
	}
}

func TestWatchPauseAndTogglesAndQuit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := seedLog(base) + toolStarted(base.Add(2*time.Second), 3, "bash", "e1")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	model := newFileModel(t, path, 30*time.Second)
	ctx := context.Background()
	now := base.Add(time.Minute)
	model.Update(ctx, now)
	if !strings.Contains(viewText(model), "◐ bash") {
		t.Fatalf("tools line missing:\n%s", viewText(model))
	}

	// t toggles the tools line off and back on.
	if model.HandleKey('t') {
		t.Fatal("t must not quit")
	}
	if strings.Contains(viewText(model), "◐ bash") {
		t.Errorf("tools line still shown after t:\n%s", viewText(model))
	}
	model.HandleKey('t')
	if !strings.Contains(viewText(model), "◐ bash") {
		t.Errorf("tools line not restored after second t:\n%s", viewText(model))
	}
	// a/d toggle their config flags (no agent/todo data is sourced,
	// so only the flags are observable here).
	model.HandleKey('a')
	model.HandleKey('d')
	if model.Config().ShowAgents || model.Config().ShowTodos {
		t.Errorf("a/d toggles: %+v", model.Config())
	}

	// Pause freezes the model: an append is not picked up.
	model.HandleKey('p')
	if !model.Paused() {
		t.Fatal("want paused")
	}
	if !strings.Contains(viewText(model), "PAUSED") {
		t.Errorf("header missing PAUSED:\n%s", viewText(model))
	}
	appendTo(t, path, toolTerminal(now, 4, "e1", "completed"))
	model.Update(ctx, now.Add(time.Second))
	if !strings.Contains(viewText(model), "◐ bash") {
		t.Errorf("paused view changed:\n%s", viewText(model))
	}
	// Resume picks the change up.
	model.HandleKey('p')
	model.Update(ctx, now.Add(2*time.Second))
	if !strings.Contains(viewText(model), "✓ bash") {
		t.Errorf("resumed view not updated:\n%s", viewText(model))
	}

	if !model.HandleKey('q') {
		t.Error("q must quit")
	}
	if !model.HandleKey(3) {
		t.Error("Ctrl-C byte must quit")
	}
}

func TestWatchWaitingThenSessionAppears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl") // does not exist yet
	model := newFileModel(t, path, 30*time.Second)
	ctx := context.Background()
	now := base.Add(time.Minute)
	model.Update(ctx, now)
	if !strings.Contains(viewText(model), "waiting for a Muse session") {
		t.Fatalf("want waiting view:\n%s", viewText(model))
	}
	if err := os.WriteFile(path, []byte(seedLog(base)), 0o644); err != nil {
		t.Fatal(err)
	}
	model.Update(ctx, now.Add(time.Second))
	if !strings.Contains(viewText(model), "[synthetic-model-1]") {
		t.Fatalf("session not picked up after appearing:\n%s", viewText(model))
	}
}

func TestWatchSwitchesToNewerSession(t *testing.T) {
	// A synthetic muse home: sessions/YYYY/MM/DD/<id>/session.jsonl.
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "01", "01")
	oldDir := filepath.Join(day, "synthetic-old")
	newDir := filepath.Join(day, "synthetic-new")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := filepath.Join(oldDir, "session.jsonl")
	newPath := filepath.Join(newDir, "session.jsonl")
	oldLog := seedLog(base) + toolStarted(base, 3, "bash", "e-old")
	if err := os.WriteFile(oldPath, []byte(oldLog), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	model := watch.New(watch.Options{
		Collect:          collect.Options{MuseHome: home, Last: true, SkipGit: true},
		Config:           config.Default(),
		SkipGit:          true,
		DiscoverInterval: 10 * time.Millisecond,
	})
	ctx := context.Background()
	now := base.Add(time.Minute)
	model.Update(ctx, now)
	if model.Session().ID != "synthetic-old" {
		t.Fatalf("following %q, want synthetic-old", model.Session().ID)
	}

	// A newer session appears (fresh mtime): watch switches to it.
	newLog := seedLog(base) + toolStarted(base, 3, "web_fetch", "e-new")
	if err := os.WriteFile(newPath, []byte(newLog), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for model.Session().ID != "synthetic-new" {
		now = now.Add(20 * time.Millisecond)
		model.Update(ctx, now)
		if time.Now().After(deadline) {
			t.Fatalf("no switch within 1s; following %q view:\n%s", model.Session().ID, viewText(model))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if model.Switches() != 1 {
		t.Errorf("switches %d, want 1", model.Switches())
	}
	if !strings.Contains(viewText(model), "◐ web_fetch") || strings.Contains(viewText(model), "bash") {
		t.Errorf("view not switched to new session state:\n%s", viewText(model))
	}
}
