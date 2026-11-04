package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ExportDoc is the header subset of a `muse export` document
// (export_schema_version 1, verified on Muse 1.4.14). Export is the
// heaviest source and no HUD fact currently requires it (spikes a–d),
// so Collect never calls it by default; the adapter exists so a later
// phase can adopt an export-only fact without re-probing the format.
// Event/message contents are deliberately not decoded into the
// snapshot: muse-hud needs session facts, not transcript text.
type ExportDoc struct {
	ExportSchemaVersion int               `json:"export_schema_version"`
	Redaction           string            `json:"redaction"`
	Sessions            []ExportSession   `json:"sessions"`
	Events              []json.RawMessage `json:"events"`
}

// ExportSession is one session entry of an ExportDoc.
type ExportSession struct {
	SessionID     string `json:"session_id"`
	RootSessionID string `json:"root_session_id"`
	TurnCount     int    `json:"turn_count"`
	StepCount     int    `json:"step_count"`
}

// ParseExport parses a `muse export` JSON document.
func ParseExport(data []byte) (*ExportDoc, error) {
	var doc ExportDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse export document: %w", err)
	}
	if doc.ExportSchemaVersion != 1 {
		return &doc, fmt.Errorf("export schema version %d, only 1 verified", doc.ExportSchemaVersion)
	}
	return &doc, nil
}

// Export runs `muse export --redacted` for a session log into a
// temporary file, parses the header subset, and removes the file.
// Redacted is always passed: muse-hud needs no payload strings.
func Export(ctx context.Context, runner Runner, museBin, sessionLog string) (*ExportDoc, error) {
	if museBin == "" {
		museBin = "muse"
	}
	dir, err := os.MkdirTemp("", "muse-hud-export-")
	if err != nil {
		return nil, fmt.Errorf("export temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, "export.json")

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := runner(ctx, museBin, "export", "--session", sessionLog, "--out", out, "--redacted"); err != nil {
		return nil, fmt.Errorf("run muse export: %w", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read export file: %w", err)
	}
	return ParseExport(data)
}
