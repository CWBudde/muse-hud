package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

func stubSnapshot() snapshot.Snapshot {
	return snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Model:         "stub-model", Effort: "high",
		Workspace: "example-workspace", Branch: "main", Dirty: true,
		Age: 12 * time.Minute,
		Tools: []snapshot.Tool{
			{Name: "bash", Target: "go test ./...", Running: true},
			{Name: "read_file", Count: 3},
		},
		Todos: &snapshot.Todos{Current: "Write the plan", Done: 1, Total: 3},
	}
}

func TestStubGolden(t *testing.T) {
	got := render.String(stubSnapshot(), config.Default())
	want := "[stub-model · high] │ example-workspace git:(main*)\n" +
		"⏱ 12m\n" +
		"◐ bash: go test ./... | ✓ read_file ×3\n" +
		"▸ Write the plan (1/3)"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestGoldenLineCombinations pins every line combination the Phase 2
// renderer can emit from sourced data.
func TestGoldenLineCombinations(t *testing.T) {
	pct := func(n int) *int { return &n }
	tokens := func(n int64) *int64 { return &n }
	cases := []struct {
		name string
		snap snapshot.Snapshot
		want string
	}{
		{"empty", snapshot.Snapshot{}, ""},
		{"model only", snapshot.Snapshot{Model: "m"}, "[m]"},
		{"model and effort", snapshot.Snapshot{Model: "m", Effort: "low"}, "[m · low]"},
		{"effort without model is hidden", snapshot.Snapshot{Effort: "low"}, ""},
		{"workspace only", snapshot.Snapshot{Workspace: "ws"}, "ws"},
		{"branch clean", snapshot.Snapshot{Branch: "dev"}, "git:(dev)"},
		{"branch dirty", snapshot.Snapshot{Branch: "dev", Dirty: true}, "git:(dev*)"},
		{"workspace and branch", snapshot.Snapshot{Workspace: "ws", Branch: "dev"}, "ws git:(dev)"},
		{"age only", snapshot.Snapshot{Age: 90 * time.Second}, "⏱ 1m"},
		{"usage only", snapshot.Snapshot{Usage: "Usage 28% window"}, "Usage 28% window"},
		{"context sourced", snapshot.Snapshot{ContextPercent: pct(45)}, "Context ████░░░░░░ 45%"},
		{"context 0 sourced", snapshot.Snapshot{ContextPercent: pct(0)}, "Context ░░░░░░░░░░ 0%"},
		{"context 100 sourced", snapshot.Snapshot{ContextPercent: pct(100)}, "Context ██████████ 100%"},
		{"tokens hidden by default", snapshot.Snapshot{EstimatedPromptTokens: tokens(48774)}, ""},
		{"approvals one", snapshot.Snapshot{ApprovalsPending: 1}, "⚠ 1 approval pending"},
		{"approvals many", snapshot.Snapshot{ApprovalsPending: 3}, "⚠ 3 approvals pending"},
		{"tools running elapsed", snapshot.Snapshot{
			Tools: []snapshot.Tool{{Name: "bash", Running: true, Elapsed: 75 * time.Second}},
		}, "◐ bash (1m)"},
		{"tools failed", snapshot.Snapshot{
			Tools: []snapshot.Tool{{Name: "bash", Failed: true, Count: 2}},
		}, "✗ bash ×2"},
		{"tools target", snapshot.Snapshot{
			Tools: []snapshot.Tool{{Name: "read_file", Target: "internal/x.go"}},
		}, "✓ read_file: internal/x.go"},
		{"agents running and done", snapshot.Snapshot{
			Agents: []snapshot.Agent{
				{Kind: "subagent", Label: "explore", Running: true, Elapsed: 2*time.Minute + 15*time.Second},
				{Kind: "workflow", Label: "workflow.agent.native"},
			},
		}, "◐ subagent explore (2m) | ✓ workflow workflow.agent.native"},
		{"todos", snapshot.Snapshot{
			Todos: &snapshot.Todos{Current: "Fix auth", Done: 2, Total: 5},
		}, "▸ Fix auth (2/5)"},
		{"todos total zero hidden", snapshot.Snapshot{
			Todos: &snapshot.Todos{Current: "Fix auth"},
		}, ""},
		{"every line", snapshot.Snapshot{
			Model: "m", Effort: "high", Workspace: "ws", Branch: "main",
			ContextPercent: pct(45), Usage: "Usage 28% window",
			ApprovalsPending: 1, Age: 12 * time.Minute,
			Tools:  []snapshot.Tool{{Name: "bash", Running: true}},
			Agents: []snapshot.Agent{{Kind: "subagent", Label: "explore", Running: true}},
			Todos:  &snapshot.Todos{Current: "Fix auth", Done: 2, Total: 5},
		}, "[m · high] │ ws git:(main)\n" +
			"Context ████░░░░░░ 45% │ Usage 28% window │ ⚠ 1 approval pending │ ⏱ 12m\n" +
			"◐ bash\n" +
			"◐ subagent explore\n" +
			"▸ Fix auth (2/5)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := render.String(tc.snap, config.Default()); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestMissingDataHidesSegments(t *testing.T) {
	if got := render.String(snapshot.Snapshot{}, config.Default()); got != "" {
		t.Errorf("empty snapshot rendered %q, want empty", got)
	}
	s := snapshot.Snapshot{Model: "m"}
	got := render.String(s, config.Default())
	if got != "[m]" {
		t.Errorf("got %q, want %q", got, "[m]")
	}
	if strings.Contains(got, "Context") || strings.Contains(got, "%") {
		t.Errorf("fabricated context in %q", got)
	}
}

func TestSegmentToggles(t *testing.T) {
	full := stubSnapshot()
	cases := []struct {
		name  string
		apply func(*config.Config)
		want  string
	}{
		{
			"tools off", func(c *config.Config) { c.ShowTools = false },
			"[stub-model · high] │ example-workspace git:(main*)\n⏱ 12m\n▸ Write the plan (1/3)",
		},
		{
			"todos off", func(c *config.Config) { c.ShowTodos = false },
			"[stub-model · high] │ example-workspace git:(main*)\n⏱ 12m\n◐ bash: go test ./... | ✓ read_file ×3",
		},
		{
			"model off", func(c *config.Config) { c.ShowModel = false },
			"example-workspace git:(main*)\n⏱ 12m\n◐ bash: go test ./... | ✓ read_file ×3\n▸ Write the plan (1/3)",
		},
		{
			"workspace off", func(c *config.Config) { c.ShowWorkspace = false },
			"[stub-model · high] │ git:(main*)\n⏱ 12m\n◐ bash: go test ./... | ✓ read_file ×3\n▸ Write the plan (1/3)",
		},
		{
			"branch off", func(c *config.Config) { c.ShowBranch = false },
			"[stub-model · high] │ example-workspace\n⏱ 12m\n◐ bash: go test ./... | ✓ read_file ×3\n▸ Write the plan (1/3)",
		},
		{
			"age off removes status line", func(c *config.Config) { c.ShowAge = false },
			"[stub-model · high] │ example-workspace git:(main*)\n◐ bash: go test ./... | ✓ read_file ×3\n▸ Write the plan (1/3)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			tc.apply(&cfg)
			if got := render.String(full, cfg); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestLineOrderConfig(t *testing.T) {
	cfg := config.Default()
	cfg.LineOrder = []string{"todos", "tools", "main", "status"}
	got := render.String(stubSnapshot(), cfg)
	want := "▸ Write the plan (1/3)\n" +
		"◐ bash: go test ./... | ✓ read_file ×3\n" +
		"[stub-model · high] │ example-workspace git:(main*)\n" +
		"⏱ 12m"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTokensSegmentOptIn(t *testing.T) {
	n := int64(48774)
	s := snapshot.Snapshot{Age: time.Minute, EstimatedPromptTokens: &n}
	if got := render.String(s, config.Default()); strings.Contains(got, "tokens") {
		t.Errorf("tokens shown by default: %q", got)
	}
	cfg := config.Default()
	cfg.ShowTokens = true
	want := "≈48.8k prompt tokens (est.) │ ⏱ 1m"
	if got := render.String(s, cfg); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContextOnlyWhenSourced(t *testing.T) {
	pct := 45
	s := snapshot.Snapshot{Model: "m", ContextPercent: &pct}
	got := render.String(s, config.Default())
	if !strings.Contains(got, "Context ████░░░░░░ 45%") {
		t.Errorf("got %q, want context bar", got)
	}
}

func TestColorThresholdsOnlyForSourcedContext(t *testing.T) {
	color := render.Options{Color: true}
	for pct, code := range map[int]string{10: "\x1b[32m", 69: "\x1b[32m", 70: "\x1b[33m", 89: "\x1b[33m", 90: "\x1b[31m", 100: "\x1b[31m"} {
		p := pct
		got := render.StringWithOptions(snapshot.Snapshot{ContextPercent: &p}, config.Default(), color)
		if !strings.Contains(got, code+"Context") || !strings.HasSuffix(got, "\x1b[0m") {
			t.Errorf("pct %d: got %q, want style %q prefix and reset suffix", pct, got, code)
		}
	}
	// Plain rendering never carries ANSI, and an unsourced context is
	// never coloured into existence.
	plain := render.StringWithOptions(snapshot.Snapshot{ContextPercent: pctPtr(95)}, config.Default(), render.Options{})
	if strings.Contains(plain, "\x1b") {
		t.Errorf("plain output has ANSI: %q", plain)
	}
	unsourced := render.StringWithOptions(snapshot.Snapshot{Model: "m"}, config.Default(), color)
	if strings.Contains(unsourced, "Context") {
		t.Errorf("fabricated context: %q", unsourced)
	}
	// cfg.Plain has no effect on the pure renderer colour switch (the
	// CLI folds it into Options), but custom thresholds do apply.
	cfg := config.Default()
	cfg.ContextWarnPercent, cfg.ContextCriticalPercent = 50, 60
	got := render.StringWithOptions(snapshot.Snapshot{ContextPercent: pctPtr(55)}, cfg, color)
	if !strings.Contains(got, "\x1b[33mContext") {
		t.Errorf("custom thresholds ignored: %q", got)
	}
}

func pctPtr(n int) *int { return &n }

func TestColorModelBranchAndTools(t *testing.T) {
	got := render.StringWithOptions(stubSnapshot(), config.Default(), render.Options{Color: true})
	if !strings.Contains(got, "\x1b[1;36m[stub-model · high]\x1b[0m") {
		t.Errorf("model not styled: %q", got)
	}
	if !strings.Contains(got, "\x1b[33mgit:(main*)\x1b[0m") {
		t.Errorf("dirty branch not yellow: %q", got)
	}
	if strings.Contains(stripANSI(got), "\x1b") {
		t.Errorf("unstripped ANSI remains")
	}
	if stripANSI(got) != render.String(stubSnapshot(), config.Default()) {
		t.Errorf("colour changed the visible text:\n%q", stripANSI(got))
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // past the final 'm'
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestSanitizeStripsEscapes(t *testing.T) {
	cases := map[string]string{
		"ma\x1b[31min\nnext\x07":                    "main next",
		"fea\x1b]8;;http://x\x07ture\x1b]8;;\x07/x": "feature/x",
		"br\x1b[1;31manch":                          "branch",
		"a\x1bPb\x1b\\c":                            "ac",
		"tab\tand  spaces\r\n":                      "tab and spaces",
		"bidi\u202eoverride\u202c":                  "bidioverride",
		"zero\u200bwidth":                           "zerowidth",
		"c1\u009bcontrol":                           "c1control",
		"del\x7fhere":                               "delhere",
		"trailing esc\x1b":                          "trailing esc",
		"unterminated \x1b[31":                      "unterminated",
		"normal-branch_1.2/x":                       "normal-branch_1.2/x",
	}
	for in, want := range cases {
		if got := render.Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizedBranchCannotInjectIntoLine(t *testing.T) {
	s := snapshot.Snapshot{Model: "m\x1b[31m", Branch: "main\x1b]0;pwn\x07"}
	got := render.StringWithOptions(s, config.Default(), render.Options{Color: true})
	if strings.Contains(got, "pwn") || strings.Contains(got, "\x07") {
		t.Errorf("injection survived: %q", got)
	}
	if n := strings.Count(got, "\x1b[31m"); n != 0 {
		t.Errorf("injected style present %d times: %q", n, got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("extra line injected: %q", got)
	}
}

func TestWidthTruncation(t *testing.T) {
	cfg := config.Default()
	cfg.Width = 10
	s := snapshot.Snapshot{Model: "a-very-long-model-name"}
	got := render.String(s, cfg)
	if len([]rune(got)) != 10 || !strings.HasSuffix(got, "…") {
		t.Errorf("got %q (runes=%d), want 10 runes ending …", got, len([]rune(got)))
	}
}

func TestWidthPathTruncatesFromLeft(t *testing.T) {
	cfg := config.Default()
	cfg.Width = 40
	s := snapshot.Snapshot{Tools: []snapshot.Tool{
		{Name: "read_file", Target: "internal/very/deeply/nested/package/file.go"},
	}}
	got := render.String(s, cfg)
	if !strings.Contains(got, "…/") || !strings.Contains(got, "file.go") {
		t.Fatalf("path not left-truncated keeping the tail: %q", got)
	}
	if strings.Contains(got, "internal/very") {
		t.Errorf("left of path retained: %q", got)
	}
	if w := len([]rune(got)); w > 40 {
		t.Errorf("width %d > 40: %q", w, got)
	}
	if !strings.HasPrefix(got, "✓ read_file: …/") {
		t.Errorf("unexpected shape: %q", got)
	}
}

func TestWidthCountsVisibleColumnsWithColor(t *testing.T) {
	cfg := config.Default()
	opts := render.Options{Color: true, Width: 12}
	got := render.StringWithOptions(stubSnapshot(), cfg, opts)
	for _, l := range strings.Split(got, "\n") {
		if w := len([]rune(stripANSI(l))); w > 12 {
			t.Errorf("visible width %d > 12: %q", w, l)
		}
	}
	if !strings.Contains(got, "…\x1b[0m") {
		t.Errorf("truncated coloured line not reset-terminated: %q", got)
	}
}

func TestWidthZeroUnboundedAndTinyWidths(t *testing.T) {
	s := snapshot.Snapshot{Model: strings.Repeat("x", 200)}
	if got := render.String(s, config.Default()); len([]rune(got)) != 202 {
		t.Errorf("unbounded width truncated: %d runes", len([]rune(got)))
	}
	cfg := config.Default()
	cfg.Width = 1
	if got := render.String(s, cfg); got != "…" {
		t.Errorf("width 1 got %q, want …", got)
	}
}
