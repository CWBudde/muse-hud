// Package config loads and validates muse-hud's configuration file.
// A missing file yields defaults; a bad file yields defaults plus an
// error for doctor to show — the HUD lines themselves never carry errors.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DisableEnv silences `print` entirely when set to "1": print emits
// nothing and exits 0. doctor still runs (and reports the state).
const DisableEnv = "MUSE_HUD_DISABLE"

// Disabled reports whether the environment asks muse-hud to stay silent.
func Disabled() bool { return os.Getenv(DisableEnv) == "1" }

// Known line names for Config.LineOrder, in default order.
var KnownLines = []string{"main", "status", "tools", "agents", "todos"}

// Config holds render-facing settings.
type Config struct {
	// Plain disables ANSI colour.
	Plain bool `json:"plain"`
	// Width bounds line length in columns; 0 means fit the terminal
	// (print resolves it; unbounded when there is no terminal).
	Width int `json:"width"`

	// Segment toggles. A segment also stays hidden when its data is
	// unsourced — toggles can only hide, never fabricate.
	ShowModel     bool `json:"showModel"`
	ShowWorkspace bool `json:"showWorkspace"`
	ShowBranch    bool `json:"showBranch"`
	ShowContext   bool `json:"showContext"`
	ShowUsage     bool `json:"showUsage"`
	ShowAge       bool `json:"showAge"`
	ShowApprovals bool `json:"showApprovals"`
	// ShowTokens shows the estimated prompt-token count. Off by
	// default: it is a numerator without a verified context-window
	// denominator (docs/spikes/a-context-usage.md), so it must never
	// be mistaken for a context percentage.
	ShowTokens bool `json:"showTokens"`
	ShowTools  bool `json:"showTools"`
	ShowAgents bool `json:"showAgents"`
	ShowTodos  bool `json:"showTodos"`

	// LineOrder orders the HUD lines (see KnownLines). Lines not
	// listed follow in default order; order never hides a line.
	LineOrder []string `json:"lineOrder"`

	// Context colour thresholds (percent). Only ever applied when a
	// real context percentage was sourced; on Muse 1.4.14 none is
	// (field cut in spike a), so they are inert there.
	ContextWarnPercent     int `json:"contextWarnPercent"`
	ContextCriticalPercent int `json:"contextCriticalPercent"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		ShowModel:              true,
		ShowWorkspace:          true,
		ShowBranch:             true,
		ShowContext:            true,
		ShowUsage:              true,
		ShowAge:                true,
		ShowApprovals:          true,
		ShowTools:              true,
		ShowAgents:             true,
		ShowTodos:              true,
		ContextWarnPercent:     70,
		ContextCriticalPercent: 90,
	}
}

// Path returns the default config file path:
// <os.UserConfigDir()>/muse-hud/config.json.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "muse-hud", "config.json"), nil
}

// Load reads and validates the config at path. A missing file yields
// defaults and no error; a malformed or invalid file yields defaults
// and the error (for doctor — print uses the defaults silently).
// Unknown keys are rejected so typos surface instead of being ignored.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	parsed, err := Parse(data)
	if err != nil {
		return Default(), fmt.Errorf("parse config %s: %w", path, err)
	}
	return parsed, nil
}

// Parse parses and validates config JSON bytes over the defaults.
// Unknown keys are rejected, as is trailing data after the document.
// On error it returns Default() plus the error.
func Parse(data []byte) (Config, error) {
	cfg := Default()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Default(), err
	}
	if dec.More() {
		return Default(), errors.New("trailing data after JSON document")
	}
	if err := cfg.Validate(); err != nil {
		return Default(), err
	}
	return cfg, nil
}

// Validate reports whether cfg is usable.
func (c Config) Validate() error {
	if c.Width < 0 || c.Width > 1000 {
		return fmt.Errorf("config: width must be 0..1000, got %d", c.Width)
	}
	if c.ContextWarnPercent < 1 || c.ContextWarnPercent > 100 {
		return fmt.Errorf("config: contextWarnPercent must be 1..100, got %d", c.ContextWarnPercent)
	}
	if c.ContextCriticalPercent < 1 || c.ContextCriticalPercent > 100 {
		return fmt.Errorf("config: contextCriticalPercent must be 1..100, got %d", c.ContextCriticalPercent)
	}
	if c.ContextCriticalPercent < c.ContextWarnPercent {
		return fmt.Errorf("config: contextCriticalPercent (%d) must be >= contextWarnPercent (%d)",
			c.ContextCriticalPercent, c.ContextWarnPercent)
	}
	known := map[string]bool{}
	for _, name := range KnownLines {
		known[name] = true
	}
	seen := map[string]bool{}
	for _, name := range c.LineOrder {
		if !known[name] {
			return fmt.Errorf("config: unknown line %q in lineOrder (known: main, status, tools, agents, todos)", name)
		}
		if seen[name] {
			return fmt.Errorf("config: line %q listed twice in lineOrder", name)
		}
		seen[name] = true
	}
	return nil
}

// OrderedLines returns the line names in render order: LineOrder first,
// then any unlisted lines in default order.
func (c Config) OrderedLines() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(KnownLines))
	for _, name := range c.LineOrder {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, name := range KnownLines {
		if !seen[name] {
			out = append(out, name)
		}
	}
	return out
}
