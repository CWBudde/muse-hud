package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

func TestLongRunningHighlightOnlyWithOption(t *testing.T) {
	snap := snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Tools:         []snapshot.Tool{{Name: "bash", Running: true, Elapsed: 2 * time.Minute}},
		Sources:       map[string]snapshot.Source{"tools": "jsonl"},
	}
	cfg := config.Default()

	plain := render.StringWithOptions(snap, cfg, render.Options{Color: true})
	if strings.Contains(plain, "\x1b[1;31m") {
		t.Errorf("bold red without LongRunning option (print must stay byte-identical): %q", plain)
	}
	if !strings.Contains(plain, "\x1b[33m") {
		t.Errorf("running tool should be yellow by default: %q", plain)
	}

	long := render.StringWithOptions(snap, cfg, render.Options{Color: true, LongRunning: 30 * time.Second})
	if !strings.Contains(long, "\x1b[1;31m") {
		t.Errorf("long-running tool should be bold red: %q", long)
	}

	// Below the threshold: still yellow.
	snap.Tools[0].Elapsed = 10 * time.Second
	short := render.StringWithOptions(snap, cfg, render.Options{Color: true, LongRunning: 30 * time.Second})
	if strings.Contains(short, "\x1b[1;31m") || !strings.Contains(short, "\x1b[33m") {
		t.Errorf("short-running tool should stay yellow: %q", short)
	}
}
