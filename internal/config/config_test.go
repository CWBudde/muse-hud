package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CWBudde/muse-hud/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, config.Default()) {
		t.Errorf("got %+v, want defaults %+v", cfg, config.Default())
	}
}

func TestLoadValidAndInvalid(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, `{"plain":true,"width":80,"showTools":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Plain || cfg.Width != 80 || cfg.ShowTools {
		t.Errorf("got %+v", cfg)
	}
	// Unset toggles keep their defaults.
	if !cfg.ShowAgents || !cfg.ShowModel || cfg.ShowTokens {
		t.Errorf("defaults not retained: %+v", cfg)
	}
	if cfg.ContextWarnPercent != 70 || cfg.ContextCriticalPercent != 90 {
		t.Errorf("threshold defaults lost: %+v", cfg)
	}

	cfg, err = config.Load(writeConfig(t, `{"width":-1}`))
	if err == nil {
		t.Error("negative width accepted")
	}
	if !reflect.DeepEqual(cfg, config.Default()) {
		t.Errorf("bad config got %+v, want defaults", cfg)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"not json":        `{`,
		"unknown key":     `{"showToolz":true}`,
		"trailing data":   `{"width":80} {"width":90}`,
		"width too large": `{"width":1001}`,
		"warn zero":       `{"contextWarnPercent":0}`,
		"critical < warn": `{"contextWarnPercent":80,"contextCriticalPercent":50}`,
		"unknown line":    `{"lineOrder":["main","bogus"]}`,
		"duplicate line":  `{"lineOrder":["tools","tools"]}`,
		"type mismatch":   `{"width":"wide"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Load(writeConfig(t, body))
			if err == nil {
				t.Fatalf("invalid config %q accepted", body)
			}
			if !reflect.DeepEqual(cfg, config.Default()) {
				t.Errorf("got %+v, want defaults on error", cfg)
			}
		})
	}
}

func TestLineOrder(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, `{"lineOrder":["todos","main"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"todos", "main", "status", "tools", "agents"}
	if got := cfg.OrderedLines(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (unlisted lines follow in default order)", got, want)
	}
	if got := config.Default().OrderedLines(); !reflect.DeepEqual(got, config.KnownLines) {
		t.Errorf("default order %v, want %v", got, config.KnownLines)
	}
}

func TestDisabledEnv(t *testing.T) {
	if config.Disabled() {
		t.Fatal("disabled without env")
	}
	t.Setenv(config.DisableEnv, "1")
	if !config.Disabled() {
		t.Error("MUSE_HUD_DISABLE=1 not honoured")
	}
	t.Setenv(config.DisableEnv, "0")
	if config.Disabled() {
		t.Error("MUSE_HUD_DISABLE=0 must not disable")
	}
}
