package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionStable(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"version"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	if got, want := out.String(), "muse-hud "+Version+"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrintStubStableAndExitsZero(t *testing.T) {
	// Keep the test hermetic against any real user config.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var first, second, errBuf bytes.Buffer
	if code := run([]string{"print", "--stub"}, &first, &errBuf); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	if code := run([]string{"print", "--stub"}, &second, &errBuf); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if first.String() != second.String() {
		t.Errorf("print not stable:\n%q\n%q", first.String(), second.String())
	}
	want := "[stub-model · high] │ example-workspace git:(main*)\n" +
		"⏱ 12m\n" +
		"◐ bash: go test ./... | ✓ read_file ×3\n" +
		"▸ Write the plan (1/3)\n"
	if first.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", first.String(), want)
	}
}

func fixturePath() string {
	return filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session.jsonl")
}

func TestPrintFileFixtureLines(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	if code := run([]string{"print", "--file", fixturePath()}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	got := out.String()
	if !strings.Contains(got, "[synthetic-model-1 · high]") || !strings.Contains(got, "example-workspace git:(main*)") {
		t.Errorf("line one wrong:\n%s", got)
	}
	if !strings.Contains(got, "◐ read_file") || !strings.Contains(got, "✓ bash") {
		t.Errorf("tools line wrong:\n%s", got)
	}
	if !strings.Contains(got, "subagent explore") {
		t.Errorf("agents line wrong:\n%s", got)
	}
	if strings.Contains(got, "Context") || strings.Contains(got, "▸") {
		t.Errorf("fabricated context/todo segment:\n%s", got)
	}
}

func TestPrintFileFixtureJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	if code := run([]string{"print", "--json", "--file", fixturePath()}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if doc["model"] != "synthetic-model-1" || doc["branch"] != "main" || doc["sessionId"] != "synthetic-session-0001" {
		t.Errorf("json headline wrong: %v", doc)
	}
	if doc["contextPercent"] != nil {
		t.Errorf("contextPercent %v, want null", doc["contextPercent"])
	}
	if _, present := doc["todos"]; present {
		t.Errorf("todos must be absent (cut): %v", doc["todos"])
	}
	sources, _ := doc["sources"].(map[string]any)
	if sources["tools"] != "jsonl" {
		t.Errorf("sources %v", sources)
	}
}

func TestPrintNoSessionExitsZeroWithNoLines(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errBuf bytes.Buffer
	args := []string{"print", "--muse-home", t.TempDir()}
	if code := run(args, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d, want 0 even with no session", code)
	}
	if out.String() != "" {
		t.Errorf("no session must print nothing, got %q", out.String())
	}
	var jout bytes.Buffer
	if code := run([]string{"print", "--json", "--muse-home", t.TempDir()}, &jout, &errBuf); code != 0 {
		t.Fatalf("json exit %d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal(jout.Bytes(), &doc); err != nil {
		t.Fatalf("empty snapshot json invalid: %v", err)
	}
	if _, present := doc["model"]; present {
		t.Errorf("empty json fabricated a model: %v", doc)
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"nope"}, &out, &errBuf); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "unknown command") {
		t.Errorf("stderr %q", errBuf.String())
	}
}
