package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func watchFixture(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session.jsonl")
}

func TestWatchFramesBoundedPiped(t *testing.T) {
	t.Setenv("MUSE_HUD_DISABLE", "")
	var out, errBuf bytes.Buffer
	code := cmdWatch([]string{
		"--file", watchFixture(t),
		"--frames", "2",
		"--interval", "100ms",
		"--plain",
	}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	s := out.String()
	if !strings.Contains(s, "muse-hud watch") || !strings.Contains(s, "synthetic-session-0001") {
		t.Errorf("header missing session id:\n%s", s)
	}
	if !strings.Contains(s, "[synthetic-model-1") {
		t.Errorf("HUD lines missing:\n%s", s)
	}
	if strings.Contains(s, "\x1b[H") || strings.Contains(s, "\x1b[2J") {
		t.Errorf("piped watch must not emit clear escapes:\n%q", s)
	}
	if got := strings.Count(s, "q quit"); got != 2 {
		t.Errorf("frames: got %d help footers, want 2 (one per frame):\n%s", got, s)
	}
}

func TestWatchQuitKey(t *testing.T) {
	t.Setenv("MUSE_HUD_DISABLE", "")
	var out, errBuf bytes.Buffer
	// No --frames: only the q key (already buffered in stdin) ends it.
	code := cmdWatch([]string{
		"--file", watchFixture(t),
		"--interval", "100ms",
		"--plain",
	}, strings.NewReader("q"), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "muse-hud watch") {
		t.Errorf("no frame drawn before quit:\n%s", out.String())
	}
}

func TestWatchBadFlagsExit2(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := cmdWatch([]string{"--interval", "1ms"}, strings.NewReader(""), &out, &errBuf); code != 2 {
		t.Errorf("too-fast interval: exit %d, want 2", code)
	}
	if code := cmdWatch([]string{"--nope"}, strings.NewReader(""), &out, &errBuf); code != 2 {
		t.Errorf("unknown flag: exit %d, want 2", code)
	}
}

func TestWatchDisabledEnv(t *testing.T) {
	t.Setenv("MUSE_HUD_DISABLE", "1")
	var out, errBuf bytes.Buffer
	code := cmdWatch([]string{"--file", watchFixture(t), "--frames", "1"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if out.Len() != 0 {
		t.Errorf("disabled watch printed frames:\n%s", out.String())
	}
	if !strings.Contains(errBuf.String(), "MUSE_HUD_DISABLE") {
		t.Errorf("disabled watch should explain on stderr, got %q", errBuf.String())
	}
}

func TestWatchWaitingForMissingFile(t *testing.T) {
	t.Setenv("MUSE_HUD_DISABLE", "")
	var out, errBuf bytes.Buffer
	missing := filepath.Join(t.TempDir(), "session.jsonl")
	code := cmdWatch([]string{
		"--file", missing, "--frames", "1", "--plain",
	}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "waiting for a Muse session") {
		t.Errorf("want waiting frame, got:\n%s", out.String())
	}
}

func TestWatchHelpListsKeysAndFlags(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"help"}, &out, &errBuf); code != 0 {
		t.Fatalf("help exit %d", code)
	}
	for _, want := range []string{"watch [flags]", "--frames", "--interval", "q quit"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q", want)
		}
	}
}
