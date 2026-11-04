package render

import (
	"encoding/json"
	"fmt"

	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// jsonSnapshot is the stable --json DTO for a Snapshot (schema v1).
// Unsourced fields are omitted (null for contextPercent), never faked.
type jsonSnapshot struct {
	SchemaVersion         int               `json:"schemaVersion"`
	SessionID             string            `json:"sessionId,omitempty"`
	MuseVersion           string            `json:"museVersion,omitempty"`
	Unverified            bool              `json:"unverified,omitempty"`
	Model                 string            `json:"model,omitempty"`
	Effort                string            `json:"effort,omitempty"`
	Workspace             string            `json:"workspace,omitempty"`
	WorkspaceRoot         string            `json:"workspaceRoot,omitempty"`
	Branch                string            `json:"branch,omitempty"`
	Dirty                 bool              `json:"dirty,omitempty"`
	ContextPercent        *int              `json:"contextPercent"`
	EstimatedPromptTokens *int64            `json:"estimatedPromptTokens,omitempty"`
	Usage                 string            `json:"usage,omitempty"`
	AgeSeconds            int64             `json:"ageSeconds,omitempty"`
	Ended                 bool              `json:"ended,omitempty"`
	Tools                 []jsonTool        `json:"tools,omitempty"`
	Agents                []jsonAgent       `json:"agents,omitempty"`
	Todos                 *jsonTodos        `json:"todos,omitempty"`
	ApprovalsPending      int               `json:"approvalsPending,omitempty"`
	Sources               map[string]string `json:"sources"`
}

type jsonTool struct {
	Name           string `json:"name"`
	Target         string `json:"target,omitempty"`
	Running        bool   `json:"running,omitempty"`
	Failed         bool   `json:"failed,omitempty"`
	Count          int    `json:"count,omitempty"`
	ElapsedSeconds int64  `json:"elapsedSeconds,omitempty"`
}

type jsonAgent struct {
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	Running        bool   `json:"running,omitempty"`
	ElapsedSeconds int64  `json:"elapsedSeconds,omitempty"`
}

type jsonTodos struct {
	Current string `json:"current"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
}

// JSON renders the snapshot as indented JSON for `print --json`.
func JSON(s snapshot.Snapshot) ([]byte, error) {
	dto := jsonSnapshot{
		SchemaVersion:         s.SchemaVersion,
		SessionID:             s.SessionID,
		MuseVersion:           s.MuseVersion,
		Unverified:            s.Unverified,
		Model:                 Sanitize(s.Model),
		Effort:                Sanitize(s.Effort),
		Workspace:             Sanitize(s.Workspace),
		WorkspaceRoot:         s.WorkspaceRoot,
		Branch:                Sanitize(s.Branch),
		Dirty:                 s.Dirty,
		ContextPercent:        s.ContextPercent,
		EstimatedPromptTokens: s.EstimatedPromptTokens,
		Usage:                 Sanitize(s.Usage),
		Ended:                 s.Ended,
		ApprovalsPending:      s.ApprovalsPending,
		Sources:               map[string]string{},
	}
	if s.Age > 0 {
		dto.AgeSeconds = int64(s.Age.Seconds())
	}
	for _, t := range s.Tools {
		jt := jsonTool{Name: Sanitize(t.Name), Target: Sanitize(t.Target), Running: t.Running, Failed: t.Failed, Count: t.Count}
		if t.Elapsed > 0 {
			jt.ElapsedSeconds = int64(t.Elapsed.Seconds())
		}
		dto.Tools = append(dto.Tools, jt)
	}
	for _, a := range s.Agents {
		ja := jsonAgent{Kind: Sanitize(a.Kind), Label: Sanitize(a.Label), Running: a.Running}
		if a.Elapsed > 0 {
			ja.ElapsedSeconds = int64(a.Elapsed.Seconds())
		}
		dto.Agents = append(dto.Agents, ja)
	}
	if s.Todos != nil {
		dto.Todos = &jsonTodos{Current: Sanitize(s.Todos.Current), Done: s.Todos.Done, Total: s.Todos.Total}
	}
	for k, v := range s.Sources {
		dto.Sources[k] = string(v)
	}
	out, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render json: %w", err)
	}
	return out, nil
}
