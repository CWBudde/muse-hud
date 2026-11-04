package config_test

import (
	"encoding/json"
	"testing"

	"github.com/CWBudde/muse-hud/internal/config"
)

// FuzzParse asserts the config parser never panics, that every
// accepted config passes Validate, and that every rejected input
// falls back to the exact defaults — a bad config must never
// half-apply (see docs/security.md).
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"plain":true,"width":80,"showTools":false}`))
	f.Add([]byte(`{"lineOrder":["todos","main"],"contextWarnPercent":50,"contextCriticalPercent":80}`))
	f.Add([]byte(`{"width":-1}`))
	f.Add([]byte(`{"unknownKey":true}`))
	f.Add([]byte(`{"lineOrder":["main","main"]}`))
	f.Add([]byte(`{"width":80} trailing`))
	f.Add([]byte(""))
	f.Add([]byte("null"))
	f.Add([]byte(`{"width":1e999}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := config.Parse(data)
		if err != nil {
			def := config.Default()
			cfgJSON, _ := json.Marshal(cfg)
			defJSON, _ := json.Marshal(def)
			if string(cfgJSON) != string(defJSON) {
				t.Errorf("rejected config not reset to defaults: %s, want %s", cfgJSON, defJSON)
			}
			return
		}
		if verr := cfg.Validate(); verr != nil {
			t.Errorf("accepted config fails Validate: %v (%+v)", verr, cfg)
		}
		lines := cfg.OrderedLines()
		if len(lines) != len(config.KnownLines) {
			t.Errorf("OrderedLines %v, want %d lines", lines, len(config.KnownLines))
		}
		seen := map[string]bool{}
		for _, name := range lines {
			if seen[name] {
				t.Errorf("OrderedLines duplicates %q: %v", name, lines)
			}
			seen[name] = true
		}
	})
}
