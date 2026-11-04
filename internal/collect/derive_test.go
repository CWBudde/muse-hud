package collect_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// fixtureNow is 10 minutes after the synthetic fixture's last record
// (base 1762000000000000 µs + 15 s of records).
var fixtureNow = time.UnixMicro(1762000000000000).Add(10*time.Minute + 15*time.Second)

func deriveFixture(t *testing.T) (snapshot.Snapshot, collect.Stats) {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "fixtures", "synthetic-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	snap, stats, err := collect.Derive(f, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	return snap, stats
}

func TestDeriveSyntheticFixture(t *testing.T) {
	snap, stats := deriveFixture(t)

	if snap.Model != "synthetic-model-1" {
		t.Errorf("model %q", snap.Model)
	}
	if snap.Effort != "high" {
		t.Errorf("effort %q, want high (routing_receipt)", snap.Effort)
	}
	if snap.Workspace != "example-workspace" || snap.WorkspaceRoot != "/example/example-workspace" {
		t.Errorf("workspace %q %q", snap.Workspace, snap.WorkspaceRoot)
	}
	if snap.Branch != "main" || !snap.Dirty {
		t.Errorf("branch %q dirty=%v", snap.Branch, snap.Dirty)
	}
	if snap.SessionID != "synthetic-session-0001" {
		t.Errorf("session id %q", snap.SessionID)
	}
	if snap.Age <= 9*time.Minute || snap.Age > 11*time.Minute {
		t.Errorf("age %v", snap.Age)
	}
	if snap.ContextPercent != nil {
		t.Errorf("context percent must stay nil (cut, spike a), got %d", *snap.ContextPercent)
	}
	if snap.EstimatedPromptTokens == nil || *snap.EstimatedPromptTokens != 42000 {
		t.Errorf("estimated prompt tokens %v", snap.EstimatedPromptTokens)
	}
	if snap.Todos != nil {
		t.Errorf("todos must stay nil (cut, spike b), got %+v", *snap.Todos)
	}
	if snap.ApprovalsPending != 1 {
		t.Errorf("approvals pending %d, want 1", snap.ApprovalsPending)
	}
	for _, group := range []string{"model", "effort", "workspace", "branch", "age", "tools", "agents", "approvals", "context"} {
		if !snap.Sourced(group) {
			t.Errorf("group %q not sourced", group)
		}
	}
	if snap.Sources["branch"] != "jsonl" {
		t.Errorf("branch source %q before git probe", snap.Sources["branch"])
	}

	// Tools: read_file running (no terminal), bash finished ×1.
	if len(snap.Tools) != 2 {
		t.Fatalf("tools %+v", snap.Tools)
	}
	if snap.Tools[0].Name != "read_file" || !snap.Tools[0].Running || snap.Tools[0].Elapsed <= 0 {
		t.Errorf("running tool %+v", snap.Tools[0])
	}
	if snap.Tools[0].Target != "" {
		t.Errorf("target must be empty: no source carries one, got %q", snap.Tools[0].Target)
	}
	if snap.Tools[1].Name != "bash" || snap.Tools[1].Count != 1 || snap.Tools[1].Running || snap.Tools[1].Failed {
		t.Errorf("finished tool %+v", snap.Tools[1])
	}

	// Agents: subagent.control lineage wins over the workflow task.
	if len(snap.Agents) != 1 || snap.Agents[0].Kind != "subagent" || snap.Agents[0].Label != "explore" || !snap.Agents[0].Running {
		t.Errorf("agents %+v", snap.Agents)
	}

	if stats.UnknownPayloadTypes["future.example.observed"] != 1 {
		t.Errorf("unknown payload types %v", stats.UnknownPayloadTypes)
	}
	if stats.PayloadCounts["tool_batch.effect.started"] != 2 {
		t.Errorf("payload counts %v", stats.PayloadCounts)
	}
}

func TestDeriveModelCompletedWithStringModelField(t *testing.T) {
	// Regression (found by the Phase 1 hand-check on a real log):
	// model_completed.model is a bare string, and decoding it as an
	// object used to drop estimated_prompt_tokens with it.
	in := `{"record_type":"event","sequence":1,"recorded_at":1762000000000000,"payload_type":"runtime.session","payload":{"kind":"run","event":{"kind":"model_completed","model":"synthetic-model-1","estimated_prompt_tokens":37573,"finish_reason":"stop"}}}` + "\n"
	snap, _, err := collect.Derive(strings.NewReader(in), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EstimatedPromptTokens == nil || *snap.EstimatedPromptTokens != 37573 {
		t.Errorf("estimated prompt tokens %v, want 37573", snap.EstimatedPromptTokens)
	}
	if snap.ContextPercent != nil {
		t.Error("an estimate must never become a percentage")
	}
}

func TestDeriveEmptyAndCorruptLogs(t *testing.T) {
	cases := map[string]string{
		"empty":   "",
		"blank":   "\n\n",
		"corrupt": "not json\n{\"half\": \n",
		"partial-final-line": `{"record_type":"event","sequence":1,"recorded_at":1762000000000000,"payload_type":"run.model.configured","payload":{"kind":"run_model","record":{"model_id":"m1"}}}
{"record_type":"event","sequence":2,"payload_typ`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			snap, stats, err := collect.Derive(strings.NewReader(in), fixtureNow)
			if err != nil {
				t.Fatal(err)
			}
			if name == "partial-final-line" {
				if snap.Model != "m1" || stats.Skipped != 1 {
					t.Errorf("model %q skipped %d, want m1/1", snap.Model, stats.Skipped)
				}
				return
			}
			if snap.Model != "" || len(snap.Tools) != 0 || snap.ContextPercent != nil {
				t.Errorf("fabricated data from %s log: %+v", name, snap)
			}
		})
	}
}

func TestDeriveToolFailureAndApprovalResolution(t *testing.T) {
	in := strings.Join([]string{
		`{"record_type":"event","sequence":1,"recorded_at":1762000000000000,"payload_type":"tool_batch.effect.started","payload":{"kind":"tool_batch_effect","record":{"kind":"started","tool_name":"bash","call_id":"c1","effect_id":"e1"}}}`,
		`{"record_type":"event","sequence":2,"recorded_at":1762000001000000,"payload_type":"tool_batch.effect.terminal","payload":{"kind":"tool_batch_effect","record":{"kind":"terminal","call_id":"c1","effect_id":"e1","outcome":{"kind":"failed"}}}}`,
		`{"record_type":"event","sequence":3,"recorded_at":1762000002000000,"payload_type":"runtime.session","payload":{"kind":"approval","event":{"kind":"requested","pending_action_id":"a1"}}}`,
		`{"record_type":"event","sequence":4,"recorded_at":1762000003000000,"payload_type":"runtime.session","payload":{"kind":"approval","event":{"kind":"decision_applied","pending_action_id":"a1"}}}`,
		`{"record_type":"event","sequence":5,"recorded_at":1762000004000000,"payload_type":"session.end","payload":{"kind":"session_end","record":{}}}`,
	}, "\n")
	snap, _, err := collect.Derive(strings.NewReader(in), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Tools) != 1 || !snap.Tools[0].Failed || snap.Tools[0].Count != 1 {
		t.Errorf("tools %+v, want one failed bash", snap.Tools)
	}
	if snap.ApprovalsPending != 0 || !snap.Sourced("approvals") {
		t.Errorf("approvals %d sourced=%v, want resolved 0 but sourced", snap.ApprovalsPending, snap.Sourced("approvals"))
	}
	if !snap.Ended {
		t.Error("session.end not recorded")
	}
	// Ended sessions age to the last record, not to now.
	if snap.Age != 4*time.Second {
		t.Errorf("ended age %v, want 4s", snap.Age)
	}
}
