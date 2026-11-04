package collect_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
)

// writeSession creates <home>/sessions/2026/01/02/<id>/session.jsonl
// with a workspace_branch head line and the given mtime.
func writeSession(t *testing.T, home, id, workspaceRoot string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "01", "02", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.jsonl")
	line := `{"record_type":"event","sequence":1,"recorded_at":1762000000000000,"payload_type":"session.workspace_branch.observed","payload":{"kind":"workspace_branch","record":{"vcs":"git","workspace_root":"` + workspaceRoot + `","reference":{"kind":"branch","name":"main"}}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverWorkspaceLastAndID(t *testing.T) {
	home := t.TempDir()
	old := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	writeSession(t, home, "synthetic-session-aaaa", "/example/ws-a", old)
	writeSession(t, home, "synthetic-session-bbbb", "/example/ws-b", newer)
	// A nested subagent log must not win top-level discovery.
	subDir := filepath.Join(home, "sessions", "2026", "01", "02", "synthetic-session-bbbb", "subagent", "synthetic-child-cccc")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(subDir, "session.jsonl")
	if err := os.WriteFile(childPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(childPath, newer.Add(time.Hour), newer.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	s, err := collect.Find(collect.FindOptions{MuseHome: home, Workspace: "/example/ws-a"})
	if err != nil || s.ID != "synthetic-session-aaaa" {
		t.Errorf("workspace find: %+v err=%v", s, err)
	}
	s, err = collect.Find(collect.FindOptions{MuseHome: home, Last: true})
	if err != nil || s.ID != "synthetic-session-bbbb" {
		t.Errorf("last find: %+v err=%v (nested child must not win)", s, err)
	}
	s, err = collect.Find(collect.FindOptions{MuseHome: home, SessionID: "synthetic-child-cccc"})
	if err != nil || s.Path != childPath {
		t.Errorf("id find (nested allowed): %+v err=%v", s, err)
	}
	if _, err := collect.Find(collect.FindOptions{MuseHome: home, Workspace: "/example/absent"}); err == nil {
		t.Error("absent workspace must not match")
	}
	if _, err := collect.Find(collect.FindOptions{MuseHome: home, SessionID: "../escape"}); err == nil {
		t.Error("path-like session id must be refused")
	}
	if _, err := collect.Find(collect.FindOptions{MuseHome: filepath.Join(home, "nope"), Last: true}); err == nil {
		t.Error("missing sessions dir must error")
	}
}

func fixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeMuse answers the three fixed muse invocations from fixtures.
// trace inspect mimics the real partial-log behaviour: an error
// (exit 1) with a usable JSON report on stdout.
func fakeMuse(t *testing.T) collect.Runner {
	t.Helper()
	usageJSON := fixtureFile(t, "usage.json")
	traceJSON := fixtureFile(t, "trace.json")
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case len(args) == 1 && args[0] == "--version":
			return []byte("Muse Code 1.4.14 (1.4.14-R0000.1)\n"), nil
		case len(args) >= 1 && args[0] == "usage":
			return usageJSON, nil
		case len(args) >= 2 && args[0] == "trace" && args[1] == "inspect":
			return traceJSON, errors.New("exit status 1 (partial log)")
		default:
			return nil, errors.New("unexpected call: " + joined)
		}
	}
}

func TestCollectWithFakeRunnerAndFixtureHome(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "01", "02", "synthetic-session-0001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), fixtureFile(t, "synthetic-session.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := collect.Collect(context.Background(), collect.Options{
		MuseHome:  home,
		Workspace: "/example/example-workspace",
		Now:       fixtureNow,
		UseUsage:  true,
		UseTrace:  true,
		Runner:    fakeMuse(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := res.Snapshot
	if res.Session.ID != "synthetic-session-0001" || res.FellBackToLast {
		t.Errorf("session %+v fellBack=%v", res.Session, res.FellBackToLast)
	}
	if !res.Support.Verified || snap.Unverified || snap.MuseVersion != "1.4.14" {
		t.Errorf("support %+v unverified=%v version=%q", res.Support, snap.Unverified, snap.MuseVersion)
	}
	// /example/... is not a git repo: probe fails, log branch stays.
	if snap.Branch != "main" || snap.Sources["branch"] != "jsonl" {
		t.Errorf("branch %q source %q, want main/jsonl fallback", snap.Branch, snap.Sources["branch"])
	}
	if snap.Usage != "Usage 28% window · 55% weekly" || snap.Sources["usage"] != "usage" {
		t.Errorf("usage %q source %q", snap.Usage, snap.Sources["usage"])
	}
	if res.Trace == nil || res.Trace.PayloadCount("tool_batch.effect.started") != 2 {
		t.Errorf("trace not captured from partial (exit 1) report: %+v", res.Trace)
	}
	// Log already produced tools, so the trace fill-in must not fire.
	if snap.Sources["tools"] != "jsonl" {
		t.Errorf("tools source %q, want jsonl", snap.Sources["tools"])
	}
	if len(res.Warnings) == 0 {
		t.Error("expected a git-probe warning for the non-repo workspace")
	}
}
