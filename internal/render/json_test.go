package render_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

func TestJSONShapeHonestNulls(t *testing.T) {
	s := snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Model:         "m1",
		Age:           90 * time.Second,
		Tools:         []snapshot.Tool{{Name: "bash", Running: true, Elapsed: 5 * time.Second}},
		Sources:       map[string]snapshot.Source{"model": "jsonl", "tools": "jsonl"},
	}
	out, err := render.JSON(s)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["schemaVersion"] != float64(snapshot.SchemaVersion) || doc["model"] != "m1" || doc["ageSeconds"] != float64(90) {
		t.Errorf("doc %v", doc)
	}
	if v, present := doc["contextPercent"]; !present || v != nil {
		t.Errorf("contextPercent must be present and null, got %v present=%v", v, present)
	}
	if _, present := doc["branch"]; present {
		t.Error("unsourced branch must be omitted")
	}
	tools, _ := doc["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools %v", doc["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "bash" || tool["running"] != true || tool["elapsedSeconds"] != float64(5) {
		t.Errorf("tool %v", tool)
	}
}
