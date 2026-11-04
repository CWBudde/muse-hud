// Command genfixture writes a synthetic Muse session.jsonl using the
// envelope and record shapes observed on Muse 1.4.14
// (record_type/sequence/recorded_at [microseconds]/payload_type/
// payload{kind,record|event}). All ids, paths, and text are invented —
// never copy real session content into fixtures. Shapes were verified
// in Phase 1 (docs/spikes/).
//
// Usage:
//
//	go run ./testdata/genfixture -out testdata/fixtures/synthetic-session.jsonl
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type envelope struct {
	RecordType  string         `json:"record_type"`
	Sequence    int64          `json:"sequence"`
	RecordedAt  int64          `json:"recorded_at"`
	PayloadType string         `json:"payload_type"`
	Payload     map[string]any `json:"payload"`
}

// baseTime is a fixed synthetic timestamp (microseconds, as in real
// 1.4.14 logs) so output is byte-stable.
const baseTime = 1762000000000000

func records() []envelope {
	type rec struct {
		payloadType string
		payload     map[string]any
	}
	raw := []rec{
		{"session.started", map[string]any{
			"kind":             "session_started",
			"background_tasks": "none",
			"command_id":       "synthetic-command-0001",
		}},
		{"runtime.session.metadata", map[string]any{
			"kind": "metadata",
			"record": map[string]any{
				"model_id":       "synthetic-model-1",
				"provider_id":    "synthetic-provider",
				"workspace_root": "/example/example-workspace",
				"build":          map[string]any{"semver": "1.4.14"},
			},
		}},
		{"run.model.configured", map[string]any{
			"kind": "run_model",
			"record": map[string]any{
				"model_id":      "synthetic-model-1",
				"display_label": "synthetic-model-1",
				"provider_id":   "synthetic-provider",
				"source":        "startup",
			},
		}},
		{"session.workspace_branch.observed", map[string]any{
			"kind": "workspace_branch",
			"record": map[string]any{
				"vcs":            "git",
				"workspace_root": "/example/example-workspace",
				"reference":      map[string]any{"kind": "branch", "name": "main"},
				"dirty":          true,
			},
		}},
		{"session.opened.observed", map[string]any{
			"kind": "session_opened",
			"record": map[string]any{
				"session_id": "synthetic-session-0001",
			},
		}},
		{"runtime.session", map[string]any{
			"kind":    "task",
			"task_id": "synthetic-task-0001",
			"event": map[string]any{
				"kind":      "proposed",
				"task_id":   "synthetic-task-0001",
				"task_kind": "tool.bash",
			},
		}},
		{"tool_batch.effect.started", map[string]any{
			"kind": "tool_batch_effect",
			"record": map[string]any{
				"kind":      "started",
				"tool_name": "bash",
				"call_id":   "synthetic-call-0001",
				"effect_id": "synthetic-effect-0001",
			},
		}},
		{"tool_batch.effect.terminal", map[string]any{
			"kind": "tool_batch_effect",
			"record": map[string]any{
				"kind":      "terminal",
				"call_id":   "synthetic-call-0001",
				"effect_id": "synthetic-effect-0001",
				"outcome":   map[string]any{"kind": "completed"},
			},
		}},
		{"runtime.session", map[string]any{
			"kind": "run",
			"event": map[string]any{
				"kind":                    "model_completed",
				"estimated_prompt_tokens": 42000,
				"finish_reason":           "stop",
			},
		}},
		{"runtime.session", map[string]any{
			"kind": "run",
			"event": map[string]any{
				"kind": "workflow_child_lifecycle",
				"routing_receipt": map[string]any{
					"effort": map[string]any{"resolved": "high", "source": "parent"},
				},
			},
		}},
		{"runtime.session", map[string]any{
			"kind":    "task",
			"task_id": "synthetic-task-0002",
			"event": map[string]any{
				"kind":      "proposed",
				"task_id":   "synthetic-task-0002",
				"task_kind": "workflow.agent.native",
			},
		}},
		{"subagent.control.spawn_accepted", map[string]any{
			"kind": "subagent_control",
			"record": map[string]any{
				"kind":        "spawn_accepted",
				"subagent_id": "synthetic-subagent-0001",
				"role":        "explore",
			},
		}},
		{"subagent.control.status_updated", map[string]any{
			"kind": "subagent_control",
			"record": map[string]any{
				"kind":           "status_updated",
				"subagent_id":    "synthetic-subagent-0001",
				"control_status": "running",
			},
		}},
		{"runtime.session", map[string]any{
			"kind": "approval",
			"event": map[string]any{
				"kind":              "requested",
				"pending_action_id": "synthetic-approval-0001",
				"tool_name":         "bash",
			},
		}},
		{"future.example.observed", map[string]any{
			"kind":   "future_example",
			"record": map[string]any{"note": "unknown payload types must be ignored"},
		}},
		{"tool_batch.effect.started", map[string]any{
			"kind": "tool_batch_effect",
			"record": map[string]any{
				"kind":      "started",
				"tool_name": "read_file",
				"call_id":   "synthetic-call-0002",
				"effect_id": "synthetic-effect-0002",
			},
		}},
	}
	out := make([]envelope, len(raw))
	for i, r := range raw {
		out[i] = envelope{
			RecordType:  "event",
			Sequence:    int64(i + 1),
			RecordedAt:  baseTime + int64(i)*1000000,
			PayloadType: r.payloadType,
			Payload:     r.payload,
		}
	}
	return out
}

// padTo inserts synthetic runtime.retained_fact filler envelopes
// before the last record until the output reaches about totalBytes,
// for bounded-scan performance fixtures (Phase 2 gate). Deterministic
// for a given totalBytes.
func padTo(recs []envelope, totalBytes int) []envelope {
	if totalBytes <= 0 || len(recs) == 0 {
		return recs
	}
	size := 0
	for _, env := range recs {
		data, _ := json.Marshal(env)
		size += len(data) + 1
	}
	filler := envelope{
		RecordType:  "event",
		PayloadType: "runtime.retained_fact",
		Payload: map[string]any{
			"kind":   "retained",
			"record": map[string]any{"note": "synthetic-padding-synthetic-padding-synthetic-padding-synthetic-padding"},
		},
	}
	data, _ := json.Marshal(filler)
	lineBytes := len(data) + 1
	out := append([]envelope{}, recs[:len(recs)-1]...)
	for size < totalBytes {
		out = append(out, filler)
		size += lineBytes
	}
	out = append(out, recs[len(recs)-1])
	for i := range out {
		out[i].Sequence = int64(i + 1)
		out[i].RecordedAt = baseTime + int64(i)*1000000
	}
	return out
}

func main() {
	outPath := flag.String("out", "", "output file (default: stdout)")
	padBytes := flag.Int("pad-bytes", 0, "pad with filler envelopes to about this many bytes")
	flag.Parse()

	w := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "genfixture:", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	for _, env := range padTo(records(), *padBytes) {
		if err := enc.Encode(env); err != nil {
			fmt.Fprintln(os.Stderr, "genfixture:", err)
			os.Exit(1)
		}
	}
}
