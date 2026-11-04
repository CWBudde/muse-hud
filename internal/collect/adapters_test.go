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

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in       string
		version  string
		verified bool
	}{
		{"Muse Code 1.4.14 (1.4.14-R7309.1)\n", "1.4.14", true},
		{"Muse Code 9.9.9\n", "9.9.9", false},
		{"no version here", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got := collect.ParseVersion(c.in)
		if got.Version != c.version || got.Verified != c.verified {
			t.Errorf("ParseVersion(%q) = %+v, want %s/%v", c.in, got, c.version, c.verified)
		}
	}
}

func TestProbeVersionUnknownStillUsable(t *testing.T) {
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("Muse Code 2.0.0\n"), nil
	}
	support, err := collect.ProbeVersion(context.Background(), runner, "muse")
	if err != nil {
		t.Fatal(err)
	}
	if support.Version != "2.0.0" || support.Verified {
		t.Errorf("support %+v, want 2.0.0 unverified", support)
	}
	fail := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("not installed")
	}
	if _, err := collect.ProbeVersion(context.Background(), fail, "muse"); err == nil {
		t.Error("missing muse binary must surface an error for diagnostics")
	}
}

func TestParseTraceFixture(t *testing.T) {
	rep, err := collect.ParseTrace(fixtureFile(t, "trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.LoadStatus != "partial" || rep.HasUsage() {
		t.Errorf("load %q hasUsage=%v, want partial/false", rep.LoadStatus, rep.HasUsage())
	}
	if rep.PayloadCount("runtime.session") != 4 || rep.PayloadCount("absent") != 0 {
		t.Errorf("payload counts wrong: %+v", rep.Summary.PayloadCounts)
	}
	tools := rep.ToolTasks()
	if len(tools) != 2 || tools[0].TaskKind != "tool.bash" {
		t.Errorf("tool tasks %+v", tools)
	}
	if _, err := collect.ParseTrace([]byte("junk")); err == nil {
		t.Error("junk trace output must error")
	}
}

func TestTraceInspectAcceptsPartialExit1(t *testing.T) {
	traceJSON := fixtureFile(t, "trace.json")
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "trace inspect") || !strings.Contains(joined, "--format json") {
			return nil, errors.New("bad args: " + joined)
		}
		return traceJSON, errors.New("exit status 1")
	}
	rep, err := collect.TraceInspect(context.Background(), runner, "muse", "/example/session.jsonl")
	if err != nil || rep == nil || rep.LoadStatus != "partial" {
		t.Fatalf("rep=%+v err=%v, want partial report despite exit 1", rep, err)
	}
}

func TestParseAndFormatUsage(t *testing.T) {
	rep, err := collect.ParseUsage(fixtureFile(t, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := collect.FormatUsage(rep); got != "Usage 28% window · 55% weekly" {
		t.Errorf("usage %q", got)
	}
	empty, err := collect.ParseUsage([]byte(`{"schemaVersion":1,"allowance":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := collect.FormatUsage(empty); got != "" {
		t.Errorf("empty allowance formatted as %q, want hidden", got)
	}
	if got := collect.FormatUsage(nil); got != "" {
		t.Errorf("nil usage formatted as %q", got)
	}
}

func TestParseExportFixture(t *testing.T) {
	doc, err := collect.ParseExport(fixtureFile(t, "export.json"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.ExportSchemaVersion != 1 || len(doc.Sessions) != 1 || doc.Sessions[0].TurnCount != 2 {
		t.Errorf("export doc %+v", doc)
	}
	if _, err := collect.ParseExport([]byte(`{"export_schema_version":99}`)); err == nil {
		t.Error("unverified export schema must be flagged")
	}
}

func TestExportAdapterUsesRedactedTempFile(t *testing.T) {
	exportJSON := fixtureFile(t, "export.json")
	var sawArgs []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		sawArgs = args
		// Emulate muse export: write the document to --out.
		for i, a := range args {
			if a == "--out" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], exportJSON, 0o644); err != nil {
					return nil, err
				}
			}
		}
		return []byte("wrote session export\n"), nil
	}
	doc, err := collect.Export(context.Background(), runner, "muse", "/example/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Sessions[0].SessionID != "synthetic-session-0001" {
		t.Errorf("doc %+v", doc)
	}
	joined := strings.Join(sawArgs, " ")
	if !strings.Contains(joined, "--redacted") || !strings.Contains(joined, "--session /example/session.jsonl") {
		t.Errorf("export args %q must be fixed and redacted", joined)
	}
}

func TestGitProbeAgainstRealRepo(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := runCmd(dir, "git", args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "synthetic@example.invalid")
	git("config", "user.name", "Synthetic")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "synthetic")

	state, err := collect.ProbeGit(context.Background(), dir, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "main" || state.Dirty {
		t.Errorf("clean state %+v, want main/clean", state)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = collect.ProbeGit(context.Background(), dir, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty {
		t.Errorf("state %+v, want dirty with untracked file", state)
	}
	if _, err := collect.ProbeGit(context.Background(), filepath.Join(dir, "absent"), time.Second); err == nil {
		t.Error("non-repo dir must error so callers keep log-observed branch")
	}
}
