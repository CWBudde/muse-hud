// Package snapshot defines the normalized, version-tagged view of what a
// Muse session is doing now. Collection fills it; rendering only reads it.
// A zero/empty field means "not sourced": renderers must hide it rather
// than fabricate a value.
package snapshot

import "time"

// SchemaVersion tags the Snapshot shape so consumers (e.g. --json output)
// can detect changes.
const SchemaVersion = 1

// Source names where a field's value came from (e.g. "stub", "jsonl",
// "trace", "usage", "git"). Empty means unsourced.
type Source string

// Tool is one tool call, running or recently finished.
type Tool struct {
	Name    string
	Target  string // empty when no source carries a target (real logs: none)
	Running bool
	Failed  bool // latest terminal outcome for this name was a failure
	Count   int  // aggregated finished calls of this name; 0 means 1
	Elapsed time.Duration
}

// Agent is one running or recent subagent/workflow child.
type Agent struct {
	Kind    string // "subagent", "workflow", …
	Label   string // role or task kind; never transcript/objective text
	Running bool
	Elapsed time.Duration
}

// Todos is the current todo list progress.
type Todos struct {
	Current string
	Done    int
	Total   int
}

// Snapshot is the single input to rendering.
type Snapshot struct {
	SchemaVersion int

	SessionID   string
	MuseVersion string // version that wrote the log, else the probed binary version

	Model         string
	Effort        string
	Workspace     string // display name (base of the workspace root)
	WorkspaceRoot string
	Branch        string
	Dirty         bool

	// ContextPercent is nil unless a real source provided it. No
	// Muse 1.4.14 surface does (docs/spikes/a-context-usage.md), so
	// it stays nil and the segment stays hidden.
	ContextPercent *int
	// EstimatedPromptTokens is the last model_completed estimate from
	// the raw log: a numerator without a verified denominator. It is
	// never converted into a percentage.
	EstimatedPromptTokens *int64
	// Usage is a preformatted, sourced usage string; empty hides it.
	Usage string
	// Age is the session age; 0 hides it.
	Age time.Duration
	// Ended is true when the log carries a session end record.
	Ended bool

	Tools  []Tool
	Agents []Agent
	// Todos stays nil on Muse 1.4.14: write_todos is visible only as
	// a tool name, its list content is in no local surface
	// (docs/spikes/b-todos-goals-background.md). Field cut, not faked.
	Todos *Todos

	// ApprovalsPending counts approval requests without an applied
	// decision. Sourced only when the log carries approval records.
	ApprovalsPending int

	// Sources records the origin of each populated group, keyed by
	// "model", "effort", "workspace", "branch", "context", "usage",
	// "age", "tools", "agents", "todos", "approvals".
	Sources map[string]Source

	// Unverified is true when the Muse version that produced the data
	// is not in the support table.
	Unverified bool
}

// Sourced reports whether the named group has a recorded source.
func (s Snapshot) Sourced(group string) bool {
	return s.Sources[group] != ""
}
