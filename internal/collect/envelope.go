package collect

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Envelope is one line of a Muse session.jsonl as observed on Muse
// 1.4.14: an event record wrapping a typed payload. Unknown fields and
// payload types are preserved/ignored, never fatal. Some lines carry no
// payload_type at all (retained-frame headers); those parse with an
// empty PayloadType and are ignored by dispatch.
type Envelope struct {
	RecordType  string          `json:"record_type"`
	Sequence    int64           `json:"sequence"`
	RecordedAt  int64           `json:"recorded_at"`
	PayloadType string          `json:"payload_type"`
	Payload     json.RawMessage `json:"payload"`
}

// Payload is the common shape inside an Envelope: a kind plus either a
// record (most payload types) or an event (runtime.session task/run/
// approval records).
type Payload struct {
	Kind   string          `json:"kind"`
	Record json.RawMessage `json:"record"`
	Event  json.RawMessage `json:"event"`
}

// DecodePayload unwraps the envelope's payload, if present.
func (e Envelope) DecodePayload() (Payload, error) {
	var p Payload
	if len(e.Payload) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return p, fmt.Errorf("payload of %q: %w", e.PayloadType, err)
	}
	return p, nil
}

// RecordedAtTime converts the envelope timestamp to a time.Time. Real
// 1.4.14 logs use microseconds since the epoch; the magnitude is
// detected so synthetic fixtures in milliseconds also parse.
func (e Envelope) RecordedAtTime() time.Time {
	return epochToTime(e.RecordedAt)
}

func epochToTime(v int64) time.Time {
	switch {
	case v <= 0:
		return time.Time{}
	case v >= 1e18: // nanoseconds
		return time.Unix(0, v).UTC()
	case v >= 1e15: // microseconds (observed in real logs)
		return time.UnixMicro(v).UTC()
	case v >= 1e12: // milliseconds
		return time.UnixMilli(v).UTC()
	default: // seconds
		return time.Unix(v, 0).UTC()
	}
}

// parseEnvelopeLine parses one raw line; ok is false for blank/corrupt
// lines and lines without a record_type.
func parseEnvelopeLine(line []byte) (Envelope, bool) {
	if len(line) == 0 {
		return Envelope{}, false
	}
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil || env.RecordType == "" {
		return Envelope{}, false
	}
	return env, true
}

// ForEachEnvelope streams newline-delimited envelopes from r, invoking
// fn per parsed envelope. Blank lines are ignored; corrupt or partial
// lines (a log being written can end mid-line) are counted as skipped.
// Lines may be arbitrarily long (model-input traces), so this uses a
// Reader, not a capped Scanner. Memory stays bounded: envelopes are
// not retained.
func ForEachEnvelope(r io.Reader, fn func(Envelope)) (skipped int, err error) {
	err = forEachRawLine(r, func(line []byte) {
		if env, ok := parseEnvelopeLine(line); ok {
			fn(env)
		} else if len(trimSpaceBytes(line)) > 0 {
			skipped++
		}
	})
	return skipped, err
}

// forEachRawLine streams newline-delimited raw lines from r (including
// a final unterminated line). Lines may be arbitrarily long, so this
// uses a Reader, not a capped Scanner.
func forEachRawLine(r io.Reader, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			fn(line)
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return fmt.Errorf("scan session log: %w", rerr)
		}
	}
}

func trimSpaceBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

// ScanEnvelopes reads all envelopes from r. Prefer ForEachEnvelope for
// real logs (they can exceed 100 MiB); this exists for fixtures/tests.
func ScanEnvelopes(r io.Reader) (envelopes []Envelope, skipped int, err error) {
	skipped, err = ForEachEnvelope(r, func(env Envelope) {
		envelopes = append(envelopes, env)
	})
	return envelopes, skipped, err
}

// extractWorkspaceRoot pulls workspace_root out of the payload shapes
// that carry it near the start of a log: workspace_branch and metadata
// records, and the session-metadata copies inside command-intake and
// session-start payloads.
func extractWorkspaceRoot(env Envelope) (string, bool) {
	var p struct {
		Record struct {
			WorkspaceRoot   string `json:"workspace_root"`
			SessionMetadata struct {
				WorkspaceRoot string `json:"workspace_root"`
			} `json:"session_metadata"`
			Command struct {
				SessionMetadata struct {
					WorkspaceRoot string `json:"workspace_root"`
				} `json:"session_metadata"`
			} `json:"command"`
		} `json:"record"`
		Payload struct {
			SessionMetadata struct {
				WorkspaceRoot string `json:"workspace_root"`
			} `json:"session_metadata"`
		} `json:"payload"`
	}
	if len(env.Payload) == 0 || json.Unmarshal(env.Payload, &p) != nil {
		return "", false
	}
	for _, root := range []string{
		p.Record.WorkspaceRoot,
		p.Record.SessionMetadata.WorkspaceRoot,
		p.Record.Command.SessionMetadata.WorkspaceRoot,
		p.Payload.SessionMetadata.WorkspaceRoot,
	} {
		if root != "" {
			return root, true
		}
	}
	return "", false
}
