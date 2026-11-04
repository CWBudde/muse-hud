package collect

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// Stats summarizes a log scan for diagnostics (doctor).
type Stats struct {
	Envelopes           int
	Skipped             int // corrupt/partial lines
	PayloadCounts       map[string]int
	UnknownPayloadTypes map[string]int // payload types dispatch does not know
	FileBytes           int64          // log size when derived from a file
	ScannedBytes        int64          // bytes actually read (<= FileBytes)
	Bounded             bool           // head+tail window used, not a full parse
}

// knownPayloadTypes are the payload types observed on Muse 1.4.14.
// Dispatch handles a subset; the rest are recognized and ignored.
// Anything else is reported via Stats.UnknownPayloadTypes so format
// drift is visible instead of silently misread.
var knownPayloadTypes = map[string]bool{
	"async.owner.attempt_admitted":             true,
	"command.invoked":                          true,
	"context.compact.manual.outcome":           true,
	"context.compact.manual.target":            true,
	"reminder.cleanup_effect.started":          true,
	"reminder.cleanup_effect.terminal":         true,
	"run.model.configured":                     true,
	"runtime.command.accepted":                 true,
	"runtime.command_intake.received":          true,
	"runtime.command_intake.settled":           true,
	"runtime.retained_fact":                    true,
	"runtime.session":                          true,
	"runtime.session.capture_selection":        true,
	"runtime.session.metadata":                 true,
	"runtime.session.route_facts":              true,
	"runtime.session.task":                     true,
	"runtime.session.task_source_committed":    true,
	"runtime.session_handoff.goal_safety":      true,
	"runtime.session_start.intake_v3":          true,
	"runtime.user_intent.accepted":             true,
	"runtime.user_intent.materialized":         true,
	"session.end":                              true,
	"session.opened.observed":                  true,
	"session.resumed":                          true,
	"session.started":                          true,
	"session.startup_phases.observed":          true,
	"session.workspace_branch.observed":        true,
	"subagent.control.attempt_admitted":        true,
	"subagent.control.child_session_bound":     true,
	"subagent.control.owner_command_recorded":  true,
	"subagent.control.owner_state_changed":     true,
	"subagent.control.result_ready":            true,
	"subagent.control.resume_context_recorded": true,
	"subagent.control.runtime_observed":        true,
	"subagent.control.spawn_accepted":          true,
	"subagent.control.start_attested":          true,
	"subagent.control.status_updated":          true,
	"subagent.control.wait_parked":             true,
	"tool_batch.effect.started":                true,
	"tool_batch.effect.terminal":               true,
}

type toolRun struct {
	name   string
	target string
	start  time.Time
}

type toolDone struct {
	name     string
	count    int
	failed   bool
	lastDone time.Time
}

type agentState struct {
	kind    string
	label   string
	running bool
	start   time.Time
	last    time.Time
	elapsed time.Duration // reported duration (subagent runtime_observed)
}

// Deriver folds envelopes into a Snapshot. Feed with Add in log order,
// then call Snapshot. Only facts a record actually carries are set;
// every populated group records its source.
type Deriver struct {
	stats Stats

	first, last time.Time
	ended       bool

	sessionID     string
	model, effort string
	workspaceRoot string
	branch        string
	branchDirty   *bool
	buildVersion  string

	estimatedPromptTokens *int64

	runningTools  map[string]*toolRun // by effect_id, fallback call_id
	toolOrder     []string            // insertion order of runningTools keys
	finishedTools map[string]*toolDone

	subagents     map[string]*agentState
	subOrder      []string
	workflowTasks map[string]*agentState
	wfOrder       []string

	approvals    map[string]bool
	sawApproval  bool
	sawTools     bool
	sawModelDone bool
}

// NewDeriver returns an empty Deriver.
func NewDeriver() *Deriver {
	return &Deriver{
		stats: Stats{
			PayloadCounts:       map[string]int{},
			UnknownPayloadTypes: map[string]int{},
		},
		runningTools:  map[string]*toolRun{},
		finishedTools: map[string]*toolDone{},
		subagents:     map[string]*agentState{},
		workflowTasks: map[string]*agentState{},
		approvals:     map[string]bool{},
	}
}

// Derive streams a whole log from r into a Snapshot.
func Derive(r io.Reader, now time.Time) (snapshot.Snapshot, Stats, error) {
	d := NewDeriver()
	skipped, err := scanLines(r, d)
	d.stats.Skipped = skipped
	return d.Snapshot(now), d.Stats(), err
}

// scanLines feeds the deriver line by line via its lazy fast path:
// envelopes whose payload type the deriver ignores are never fully
// decoded (their payload is neither copied nor re-parsed). That is
// what keeps a bounded print inside its time budget on logs dominated
// by record types the HUD does not consume.
func scanLines(r io.Reader, d *Deriver) (skipped int, err error) {
	err = forEachRawLine(r, func(line []byte) {
		if len(trimSpaceBytes(line)) == 0 {
			return
		}
		if !d.AddLine(line) {
			skipped++
		}
	})
	return skipped, err
}

// relevantPayloadType reports whether dispatch consumes the payload of
// this type; everything else needs only stats and timestamps.
func relevantPayloadType(payloadType string) bool {
	switch payloadType {
	case "run.model.configured", "runtime.session.metadata",
		"session.workspace_branch.observed", "session.started",
		"session.opened.observed", "runtime.session_start.intake_v3",
		"runtime.command_intake.received", "session.end",
		"tool_batch.effect.started", "tool_batch.effect.terminal",
		"runtime.session":
		return true
	}
	return strings.HasPrefix(payloadType, "subagent.control.")
}

// AddLine folds one raw log line into the derivation, fully decoding
// it only when its payload type is relevant. It returns false for
// corrupt/partial lines (no parseable envelope header).
func (d *Deriver) AddLine(line []byte) bool {
	var meta struct {
		RecordType  string `json:"record_type"`
		RecordedAt  int64  `json:"recorded_at"`
		PayloadType string `json:"payload_type"`
	}
	if err := json.Unmarshal(line, &meta); err != nil || meta.RecordType == "" {
		return false
	}
	at := epochToTime(meta.RecordedAt)
	d.noteMeta(meta.PayloadType, at)
	if !relevantPayloadType(meta.PayloadType) {
		return true
	}
	env, ok := parseEnvelopeLine(line)
	if !ok {
		return true // header parsed; a malformed payload is not fatal
	}
	d.dispatch(env, at)
	return true
}

// Stats returns the scan statistics gathered so far.
func (d *Deriver) Stats() Stats { return d.stats }

// Add folds one envelope into the derivation. Unknown payload types
// are counted and ignored, never fatal.
func (d *Deriver) Add(env Envelope) {
	at := env.RecordedAtTime()
	d.noteMeta(env.PayloadType, at)
	d.dispatch(env, at)
}

// noteMeta records scan statistics and the first/last timestamps.
func (d *Deriver) noteMeta(payloadType string, at time.Time) {
	d.stats.Envelopes++
	if payloadType != "" {
		d.stats.PayloadCounts[payloadType]++
		if !knownPayloadTypes[payloadType] {
			d.stats.UnknownPayloadTypes[payloadType]++
		}
	}
	if !at.IsZero() {
		if d.first.IsZero() || at.Before(d.first) {
			d.first = at
		}
		if at.After(d.last) {
			d.last = at
		}
	}
}

func (d *Deriver) dispatch(env Envelope, at time.Time) {
	payload, err := env.DecodePayload()
	if err != nil {
		return
	}
	switch env.PayloadType {
	case "run.model.configured":
		var rec struct {
			ModelID         string `json:"model_id"`
			DisplayLabel    string `json:"display_label"`
			Effort          string `json:"effort"`
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if json.Unmarshal(payload.Record, &rec) == nil {
			if rec.ModelID != "" {
				d.model = rec.ModelID
			} else if rec.DisplayLabel != "" {
				d.model = rec.DisplayLabel
			}
			if rec.Effort != "" {
				d.effort = rec.Effort
			} else if rec.ReasoningEffort != "" {
				d.effort = rec.ReasoningEffort
			}
		}
	case "runtime.session.metadata":
		var rec struct {
			ModelID       string `json:"model_id"`
			WorkspaceRoot string `json:"workspace_root"`
			Build         struct {
				Semver string `json:"semver"`
			} `json:"build"`
		}
		if json.Unmarshal(payload.Record, &rec) == nil {
			if d.model == "" && rec.ModelID != "" {
				d.model = rec.ModelID
			}
			if rec.WorkspaceRoot != "" {
				d.workspaceRoot = rec.WorkspaceRoot
			}
			if rec.Build.Semver != "" {
				d.buildVersion = rec.Build.Semver
			}
		}
	case "session.workspace_branch.observed":
		var rec struct {
			WorkspaceRoot string          `json:"workspace_root"`
			Reference     json.RawMessage `json:"reference"`
			Dirty         *bool           `json:"dirty"`
		}
		if json.Unmarshal(payload.Record, &rec) == nil {
			if rec.WorkspaceRoot != "" {
				d.workspaceRoot = rec.WorkspaceRoot
			}
			if name := referenceName(rec.Reference); name != "" {
				d.branch = name
			}
			if rec.Dirty != nil {
				d.branchDirty = rec.Dirty
			}
		}
	case "session.started", "session.opened.observed":
		var rec struct {
			SessionID     string `json:"session_id"`
			WorkspaceRoot string `json:"workspace_root"`
		}
		if json.Unmarshal(payload.Record, &rec) == nil {
			if rec.SessionID != "" {
				d.sessionID = rec.SessionID
			}
			if rec.WorkspaceRoot != "" && d.workspaceRoot == "" {
				d.workspaceRoot = rec.WorkspaceRoot
			}
		}
		if d.workspaceRoot == "" {
			if root, ok := extractWorkspaceRoot(env); ok {
				d.workspaceRoot = root
			}
		}
	case "runtime.session_start.intake_v3", "runtime.command_intake.received":
		if d.workspaceRoot == "" {
			if root, ok := extractWorkspaceRoot(env); ok {
				d.workspaceRoot = root
			}
		}
	case "session.end":
		d.ended = true
	case "tool_batch.effect.started":
		d.sawTools = true
		var rec toolEffectRecord
		if json.Unmarshal(payload.Record, &rec) == nil {
			name := rec.ToolName
			if name == "" {
				name = rec.Tool
			}
			key := rec.key()
			if key != "" && name != "" {
				if _, dup := d.runningTools[key]; !dup {
					d.toolOrder = append(d.toolOrder, key)
				}
				d.runningTools[key] = &toolRun{name: name, target: rec.Target, start: at}
			}
		}
	case "tool_batch.effect.terminal":
		d.sawTools = true
		var rec toolEffectRecord
		if json.Unmarshal(payload.Record, &rec) == nil {
			key := rec.key()
			run, ok := d.runningTools[key]
			if ok {
				delete(d.runningTools, key)
			} else if alt := rec.altKey(key); alt != "" {
				run, ok = d.runningTools[alt]
				if ok {
					delete(d.runningTools, alt)
				}
			}
			if ok && run != nil {
				done := d.finishedTools[run.name]
				if done == nil {
					done = &toolDone{name: run.name}
					d.finishedTools[run.name] = done
				}
				done.count++
				done.failed = outcomeFailed(rec.Outcome)
				done.lastDone = at
			}
		}
	case "runtime.session":
		d.addRuntimeSession(payload, at)
	default:
		if strings.HasPrefix(env.PayloadType, "subagent.control.") {
			d.addSubagentControl(env.PayloadType, payload, at)
		}
	}
}

type toolEffectRecord struct {
	ToolName string          `json:"tool_name"`
	Tool     string          `json:"tool"`
	Target   string          `json:"target"`
	CallID   string          `json:"call_id"`
	EffectID string          `json:"effect_id"`
	Outcome  json.RawMessage `json:"outcome"`
}

func (r toolEffectRecord) key() string {
	if r.EffectID != "" {
		return "effect:" + r.EffectID
	}
	if r.CallID != "" {
		return "call:" + r.CallID
	}
	return ""
}

func (r toolEffectRecord) altKey(primary string) string {
	for _, k := range []string{"call:" + r.CallID, "effect:" + r.EffectID} {
		if k != primary && !strings.HasSuffix(k, ":") {
			return k
		}
	}
	return ""
}

// outcomeFailed reports whether a terminal outcome ({"kind":…} or a
// bare string in older/synthetic shapes) is a failure.
func outcomeFailed(raw json.RawMessage) bool {
	kind := outcomeKind(raw)
	return kind != "" && kind != "completed" && kind != "success" && kind != "ok"
}

func outcomeKind(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Kind
	}
	return ""
}

// referenceName accepts the real shape {"kind":"branch","name":…}
// and a bare string (synthetic fixtures).
func referenceName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Name
	}
	return ""
}

func (d *Deriver) addRuntimeSession(payload Payload, at time.Time) {
	if len(payload.Event) == 0 {
		return
	}
	var head struct {
		Kind     string `json:"kind"`
		TaskID   string `json:"task_id"`
		TaskKind string `json:"task_kind"`
	}
	if json.Unmarshal(payload.Event, &head) != nil {
		return
	}
	switch payload.Kind {
	case "run":
		d.addRunEvent(payload.Event, head.Kind)
	case "task":
		d.addTaskEvent(head.Kind, head.TaskID, head.TaskKind, at)
	case "approval":
		d.addApprovalEvent(payload.Event, head.Kind)
	}
}

func (d *Deriver) addRunEvent(raw json.RawMessage, eventKind string) {
	// Decoded field-by-field: sibling fields have unstable shapes
	// (e.g. model_completed.model is a bare string, other events use
	// an object), and one shape surprise must not drop the rest.
	var ev struct {
		EstimatedPromptTokens *int64 `json:"estimated_prompt_tokens"`
		RoutingReceipt        struct {
			Effort struct {
				Resolved string `json:"resolved"`
			} `json:"effort"`
		} `json:"routing_receipt"`
		Model   json.RawMessage `json:"model"`
		Payload struct {
			ReasoningEffort string `json:"reasoning_effort"`
		} `json:"payload"`
		DeliverySnapshot struct {
			Payload struct {
				ReasoningEffort string `json:"reasoning_effort"`
			} `json:"payload"`
		} `json:"delivery_snapshot"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	modelEffort := ""
	var modelObj struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if len(ev.Model) > 0 && json.Unmarshal(ev.Model, &modelObj) == nil {
		modelEffort = modelObj.ReasoningEffort
	}
	if eventKind == "model_completed" && ev.EstimatedPromptTokens != nil {
		d.estimatedPromptTokens = ev.EstimatedPromptTokens
		d.sawModelDone = true
	}
	// Effort only from fields describing this run itself — never from
	// reminder rosters (those describe other agents).
	for _, e := range []string{
		ev.RoutingReceipt.Effort.Resolved,
		modelEffort,
		ev.Payload.ReasoningEffort,
		ev.DeliverySnapshot.Payload.ReasoningEffort,
	} {
		if e != "" {
			d.effort = e
			break
		}
	}
}

func (d *Deriver) addTaskEvent(eventKind, taskID, taskKind string, at time.Time) {
	if taskID == "" {
		return
	}
	if eventKind == "proposed" && strings.HasPrefix(taskKind, "workflow.") {
		if _, dup := d.workflowTasks[taskID]; !dup {
			d.wfOrder = append(d.wfOrder, taskID)
		}
		d.workflowTasks[taskID] = &agentState{kind: "workflow", label: taskKind, running: true, start: at, last: at}
		return
	}
	if a, ok := d.workflowTasks[taskID]; ok {
		a.last = at
		if eventKind == "completed" || eventKind == "failed" || eventKind == "cancelled" {
			a.running = false
		}
	}
}

func (d *Deriver) addApprovalEvent(raw json.RawMessage, eventKind string) {
	d.sawApproval = true
	var ev struct {
		PendingActionID string `json:"pending_action_id"`
		ToolCallID      string `json:"tool_call_id"`
		TaskID          string `json:"task_id"`
	}
	_ = json.Unmarshal(raw, &ev)
	key := ev.PendingActionID
	if key == "" {
		key = ev.ToolCallID
	}
	if key == "" {
		key = ev.TaskID
	}
	if key == "" {
		return
	}
	switch eventKind {
	case "requested":
		d.approvals[key] = true
	case "decision_applied", "decided", "resolved", "completed":
		delete(d.approvals, key)
	}
}

func (d *Deriver) addSubagentControl(payloadType string, payload Payload, at time.Time) {
	var rec struct {
		SubagentID    string `json:"subagent_id"`
		Role          string `json:"role"`
		ControlStatus string `json:"control_status"`
		OwnerState    string `json:"owner_state"`
		Status        string `json:"status"`
		DurationMs    int64  `json:"duration_ms"`
	}
	if json.Unmarshal(payload.Record, &rec) != nil || rec.SubagentID == "" {
		return
	}
	a, ok := d.subagents[rec.SubagentID]
	if !ok {
		a = &agentState{kind: "subagent", label: "subagent", start: at}
		d.subagents[rec.SubagentID] = a
		d.subOrder = append(d.subOrder, rec.SubagentID)
	}
	a.last = at
	if rec.Role != "" {
		// Role labels the child; the objective/prompt texts in these
		// records are transcript content and are never surfaced.
		a.label = rec.Role
	}
	switch {
	case payloadType == "subagent.control.result_ready":
		a.running = false
	case rec.DurationMs > 0:
		a.elapsed = time.Duration(rec.DurationMs) * time.Millisecond
		a.running = false
	case payloadType == "subagent.control.status_updated", payloadType == "subagent.control.owner_state_changed":
		status := rec.ControlStatus
		if status == "" {
			status = rec.OwnerState
		}
		a.running = status == "running" || status == "active"
	default:
		a.running = true
	}
}

// Snapshot builds the normalized snapshot as of now.
func (d *Deriver) Snapshot(now time.Time) snapshot.Snapshot {
	s := snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Sources:       map[string]snapshot.Source{},
	}
	ref := now
	if d.ended || ref.IsZero() || (!d.last.IsZero() && ref.Before(d.last)) {
		ref = d.last
	}
	if d.sessionID != "" {
		s.SessionID = d.sessionID
	}
	if d.buildVersion != "" {
		// The version that wrote this log is what the support table
		// is about; Collect prefers it over probing the binary.
		s.MuseVersion = d.buildVersion
		s.Sources["version"] = "jsonl"
	}
	if d.model != "" {
		s.Model = d.model
		s.Sources["model"] = "jsonl"
	}
	if d.effort != "" {
		s.Effort = d.effort
		s.Sources["effort"] = "jsonl"
	}
	if d.workspaceRoot != "" {
		s.WorkspaceRoot = d.workspaceRoot
		s.Workspace = filepath.Base(filepath.Clean(d.workspaceRoot))
		s.Sources["workspace"] = "jsonl"
	}
	if d.branch != "" {
		s.Branch = d.branch
		if d.branchDirty != nil {
			s.Dirty = *d.branchDirty
		}
		s.Sources["branch"] = "jsonl"
	}
	if !d.first.IsZero() && !ref.IsZero() && ref.After(d.first) {
		s.Age = ref.Sub(d.first)
		s.Sources["age"] = "jsonl"
	}
	s.Ended = d.ended
	if d.estimatedPromptTokens != nil {
		s.EstimatedPromptTokens = d.estimatedPromptTokens
		s.Sources["context"] = "jsonl"
	}
	if d.sawTools {
		s.Tools = d.toolsSnapshot(ref)
		s.Sources["tools"] = "jsonl"
	}
	if agents := d.agentsSnapshot(ref); len(agents) > 0 {
		s.Agents = agents
		s.Sources["agents"] = "jsonl"
	}
	if d.sawApproval {
		s.ApprovalsPending = len(d.approvals)
		s.Sources["approvals"] = "jsonl"
	}
	return s
}

func (d *Deriver) toolsSnapshot(ref time.Time) []snapshot.Tool {
	const maxTools = 8
	var out []snapshot.Tool
	for _, key := range d.toolOrder {
		run, ok := d.runningTools[key]
		if !ok {
			continue
		}
		t := snapshot.Tool{Name: run.name, Target: run.target, Running: true}
		if !run.start.IsZero() && ref.After(run.start) {
			t.Elapsed = ref.Sub(run.start)
		}
		out = append(out, t)
	}
	finished := make([]*toolDone, 0, len(d.finishedTools))
	for _, done := range d.finishedTools {
		finished = append(finished, done)
	}
	sort.Slice(finished, func(i, j int) bool {
		if !finished[i].lastDone.Equal(finished[j].lastDone) {
			return finished[i].lastDone.After(finished[j].lastDone)
		}
		return finished[i].name < finished[j].name
	})
	for _, done := range finished {
		out = append(out, snapshot.Tool{Name: done.name, Count: done.count, Failed: done.failed})
	}
	if len(out) > maxTools {
		out = out[:maxTools]
	}
	return out
}

func (d *Deriver) agentsSnapshot(ref time.Time) []snapshot.Agent {
	const maxAgents = 6
	var states []*agentState
	// Prefer subagent.control lineage; workflow tasks describe the
	// same children in parent logs and would double-count (spike d).
	if len(d.subagents) > 0 {
		for _, id := range d.subOrder {
			states = append(states, d.subagents[id])
		}
	} else {
		for _, id := range d.wfOrder {
			states = append(states, d.workflowTasks[id])
		}
	}
	sort.SliceStable(states, func(i, j int) bool {
		if states[i].running != states[j].running {
			return states[i].running
		}
		return states[i].last.After(states[j].last)
	})
	var out []snapshot.Agent
	for _, a := range states {
		ag := snapshot.Agent{Kind: a.kind, Label: a.label, Running: a.running}
		switch {
		case a.elapsed > 0:
			ag.Elapsed = a.elapsed
		case a.running && !a.start.IsZero() && ref.After(a.start):
			ag.Elapsed = ref.Sub(a.start)
		case !a.running && !a.start.IsZero() && a.last.After(a.start):
			ag.Elapsed = a.last.Sub(a.start)
		}
		out = append(out, ag)
	}
	if len(out) > maxAgents {
		out = out[:maxAgents]
	}
	return out
}
