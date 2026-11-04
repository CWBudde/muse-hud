package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TraceReport is the subset of `muse trace inspect --format json`
// muse-hud consumes (verified against Muse 1.4.14). Unknown fields
// are ignored. Usage stays raw: when a projection fully loads it
// carries cumulative token totals, on partial logs it can be null —
// either way it is not a live context percentage, so no snapshot
// field is derived from it (spike a).
type TraceReport struct {
	SchemaVersion int    `json:"schema_version"`
	LoadStatus    string `json:"load_status"`
	Summary       struct {
		RunRecords    int `json:"run_records"`
		TaskRecords   int `json:"task_records"`
		PayloadCounts []struct {
			PayloadType string `json:"payload_type"`
			Count       int    `json:"count"`
		} `json:"payload_counts"`
		Usage             json.RawMessage `json:"usage"`
		ProjectionError   *string         `json:"projection_error"`
		AssistantTextSumm json.RawMessage `json:"assistant_text_summary"`
		TaskSummaries     []TraceTask     `json:"task_summaries"`
	} `json:"summary"`
}

// TraceTask is one task summary from a TraceReport.
type TraceTask struct {
	TaskID   string  `json:"task_id"`
	TaskKind string  `json:"task_kind"`
	Category string  `json:"category"`
	State    string  `json:"state"`
	Terminal *string `json:"terminal"`
}

// ToolTasks returns the task summaries in the tool category.
func (r *TraceReport) ToolTasks() []TraceTask {
	var out []TraceTask
	for _, t := range r.Summary.TaskSummaries {
		if t.Category == "tool" {
			out = append(out, t)
		}
	}
	return out
}

// PayloadCount returns the counted envelopes for a payload type (0 if
// absent).
func (r *TraceReport) PayloadCount(payloadType string) int {
	for _, pc := range r.Summary.PayloadCounts {
		if pc.PayloadType == payloadType {
			return pc.Count
		}
	}
	return 0
}

// HasUsage reports whether summary.usage is present and non-null.
func (r *TraceReport) HasUsage() bool {
	return len(r.Summary.Usage) > 0 && string(r.Summary.Usage) != "null"
}

// ParseTrace parses `muse trace inspect --format json` output.
func ParseTrace(data []byte) (*TraceReport, error) {
	var rep TraceReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("parse trace inspect output: %w", err)
	}
	return &rep, nil
}

// TraceInspect runs trace inspect on a session log. Muse exits 1 on a
// partial (still-running) log while still emitting the JSON report, so
// a parseable report is returned even when the runner reported an
// error; the error then describes the partial projection only.
func TraceInspect(ctx context.Context, runner Runner, museBin, sessionLog string) (*TraceReport, error) {
	if museBin == "" {
		museBin = "muse"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, runErr := runner(ctx, museBin, "trace", "inspect", "--session-log", sessionLog, "--format", "json")
	rep, err := ParseTrace(out)
	if err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("%w (runner: %v)", err, runErr)
		}
		return nil, err
	}
	return rep, nil
}
