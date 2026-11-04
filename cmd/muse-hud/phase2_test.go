package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hermeticEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("COLUMNS", "")
	t.Setenv("TERM", "xterm")
}

func TestPrintDisabledEnv(t *testing.T) {
	hermeticEnv(t)
	t.Setenv("MUSE_HUD_DISABLE", "1")
	var out, errBuf bytes.Buffer
	if code := run([]string{"print", "--stub"}, &out, &errBuf); code != 0 {
		t.Fatalf("disabled print exit %d, want 0", code)
	}
	if out.Len() != 0 {
		t.Errorf("disabled print emitted %q", out.String())
	}
	var jout bytes.Buffer
	if code := run([]string{"print", "--json", "--stub"}, &jout, &errBuf); code != 0 || jout.Len() != 0 {
		t.Errorf("disabled --json: exit %d, out %q", code, jout.String())
	}
	// Doctor still reports (and names the disabled state).
	var dout bytes.Buffer
	_ = run([]string{"doctor", "--file", fixturePath()}, &dout, &errBuf)
	if !strings.Contains(dout.String(), "MUSE_HUD_DISABLE=1") {
		t.Errorf("doctor does not report disabled state:\n%s", dout.String())
	}
}

func TestPrintColorFlags(t *testing.T) {
	hermeticEnv(t)
	runPrint := func(args ...string) string {
		var out, errBuf bytes.Buffer
		if code := run(append([]string{"print", "--stub"}, args...), &out, &errBuf); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errBuf.String())
		}
		return out.String()
	}
	if got := runPrint(); strings.Contains(got, "\x1b") {
		t.Errorf("non-TTY output must be plain, got %q", got)
	}
	if got := runPrint("--color"); !strings.Contains(got, "\x1b[1;36m") {
		t.Errorf("--color must force ANSI, got %q", got)
	}
	if got := runPrint("--color", "--plain"); strings.Contains(got, "\x1b") {
		t.Errorf("--plain must win over --color, got %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := runPrint("--color"); strings.Contains(got, "\x1b") {
		t.Errorf("NO_COLOR must win over --color, got %q", got)
	}
}

func TestPrintWidthFlag(t *testing.T) {
	hermeticEnv(t)
	var out, errBuf bytes.Buffer
	if code := run([]string{"print", "--stub", "--width", "20"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if n := len([]rune(l)); n > 20 {
			t.Errorf("line width %d > 20: %q", n, l)
		}
	}
}

func TestPrintConfigToggles(t *testing.T) {
	hermeticEnv(t)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"showTools":false,"showTodos":false,"width":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"print", "--stub", "--config", cfgPath}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "[stub-model · high] │ example-workspace git:(main*)\n⏱ 12m\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestBadConfigFallsBackSilentlyInPrintButDoctorExplains(t *testing.T) {
	hermeticEnv(t)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"width":-5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"print", "--stub", "--config", cfgPath}, &out, &errBuf); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "[stub-model") {
		t.Errorf("bad config must fall back to defaults, got %q", out.String())
	}
	if strings.Contains(out.String(), "config") || strings.Contains(out.String(), "width must") {
		t.Errorf("config error leaked into HUD lines: %q", out.String())
	}
	var dout bytes.Buffer
	if code := run([]string{"doctor", "--file", fixturePath(), "--config", cfgPath}, &dout, &errBuf); code != 0 {
		t.Fatalf("doctor exit %d, stderr %q", code, errBuf.String())
	}
	if !strings.Contains(dout.String(), "INVALID") || !strings.Contains(dout.String(), "width must") {
		t.Errorf("doctor does not explain bad config:\n%s", dout.String())
	}
}

func TestDoctorFixtureReport(t *testing.T) {
	hermeticEnv(t)
	var out, errBuf bytes.Buffer
	if code := run([]string{"doctor", "--file", fixturePath()}, &out, &errBuf); code != 0 {
		t.Fatalf("doctor exit %d, stderr %q\n%s", code, errBuf.String(), out.String())
	}
	got := out.String()
	for _, want := range []string{
		"muse-hud doctor",
		"muse: ",
		"sessions dir: ",
		"session: ",
		"session.jsonl: ok",
		"muse trace inspect: not run (opt-in --with-trace)",
		"model: shown (synthetic-model-1, source jsonl)",
		"branch: shown (main",
		"context: hidden — no Muse surface provides a live context percentage",
		"usage: hidden — allowance is opt-in",
		"tools: shown (2 listed, 1 running, source jsonl)",
		"agents: shown (1 listed, 1 running, source jsonl)",
		"todos: hidden — todo list content is in no local Muse surface",
		"print preview (plain):",
		"[synthetic-model-1 · high] │ example-workspace git:(main*)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("doctor to a pipe must be plain:\n%q", got)
	}
}

func TestDoctorNoSessionExitsOne(t *testing.T) {
	hermeticEnv(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"doctor", "--muse-home", t.TempDir()}, &out, &errBuf)
	if code != 1 {
		t.Fatalf("doctor exit %d, want 1 when no session\n%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"session: NONE", "model: hidden", "(no lines"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
	var badOut bytes.Buffer
	if code := run([]string{"doctor", "--nonsense"}, &badOut, &errBuf); code != 2 {
		t.Errorf("doctor bad flag exit %d, want 2", code)
	}
}
