// Package render turns a snapshot plus config into HUD lines. It is
// pure: no I/O, no clock. Unsourced snapshot fields are hidden, never
// fabricated. All externally sourced strings are sanitized against
// terminal escape injection before display.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

const sep = " │ "

// ANSI styles used when colour is enabled. The sanitizer guarantees the
// only escape sequences in rendered output are these.
const (
	styleReset   = "\x1b[0m"
	styleModel   = "\x1b[1;36m" // bold cyan
	styleBranch  = "\x1b[32m"   // green
	styleDirty   = "\x1b[33m"   // yellow
	styleGreen   = "\x1b[32m"
	styleYellow  = "\x1b[33m"
	styleRed     = "\x1b[31m"
	styleBoldRed = "\x1b[1;31m"
	styleDim     = "\x1b[2m"
)

// Options carries runtime-resolved rendering choices (the CLI resolves
// colour/width from flags, env and the terminal; the renderer stays pure).
type Options struct {
	// Color enables ANSI styling.
	Color bool
	// Width bounds each line in display columns; 0 falls back to
	// cfg.Width, and 0 there means unbounded.
	Width int
	// LongRunning, when > 0, renders a running tool whose elapsed
	// time reaches it in bold red instead of yellow (watch mode's
	// long-running highlight). Zero disables it, keeping print's
	// output byte-identical.
	LongRunning time.Duration
}

// segment is one piece of a line. sep precedes it ("" for the first
// visible piece or a piece glued to its predecessor). When path is set,
// the text after pathPrefix runes shrinks from the left — keeping the
// meaningful tail of a path — before a line is truncated from the right.
type segment struct {
	text       string
	style      string
	sep        string
	path       bool
	pathPrefix int // runes of text that are not the shrinkable path
}

// Lines renders the HUD for s under cfg without colour: two default
// lines (each only as far as sourced data reaches) plus optional
// tools/agents/todos lines when present and enabled, in cfg order.
func Lines(s snapshot.Snapshot, cfg config.Config) []string {
	return LinesWithOptions(s, cfg, Options{})
}

// LinesWithOptions renders the HUD honouring colour and width options.
func LinesWithOptions(s snapshot.Snapshot, cfg config.Config, opts Options) []string {
	width := opts.Width
	if width <= 0 {
		width = cfg.Width
	}
	built := map[string][]segment{}
	if l := mainLine(s, cfg); len(l) > 0 {
		built["main"] = l
	}
	if l := statusLine(s, cfg); len(l) > 0 {
		built["status"] = l
	}
	if cfg.ShowTools && len(s.Tools) > 0 {
		built["tools"] = toolsLine(s.Tools, opts.LongRunning)
	}
	if cfg.ShowAgents && len(s.Agents) > 0 {
		built["agents"] = agentsLine(s.Agents)
	}
	if cfg.ShowTodos && s.Todos != nil && s.Todos.Total > 0 {
		built["todos"] = todosLine(*s.Todos)
	}
	var out []string
	for _, name := range cfg.OrderedLines() {
		segs, ok := built[name]
		if !ok {
			continue
		}
		out = append(out, renderLine(segs, width, opts.Color))
	}
	return out
}

// String renders Lines joined with newlines (no trailing newline).
func String(s snapshot.Snapshot, cfg config.Config) string {
	return strings.Join(Lines(s, cfg), "\n")
}

// StringWithOptions renders LinesWithOptions joined with newlines.
func StringWithOptions(s snapshot.Snapshot, cfg config.Config, opts Options) string {
	return strings.Join(LinesWithOptions(s, cfg, opts), "\n")
}

func mainLine(s snapshot.Snapshot, cfg config.Config) []segment {
	var segs []segment
	if cfg.ShowModel && s.Model != "" {
		label := Sanitize(s.Model)
		if s.Effort != "" {
			label += " · " + Sanitize(s.Effort)
		}
		segs = append(segs, segment{text: "[" + label + "]", style: styleModel})
	}
	joiner := ""
	if len(segs) > 0 {
		joiner = sep
	}
	if cfg.ShowWorkspace && s.Workspace != "" {
		ws := Sanitize(s.Workspace)
		segs = append(segs, segment{text: ws, sep: joiner, path: strings.Contains(ws, "/")})
		joiner = " "
	}
	if cfg.ShowBranch && s.Branch != "" {
		dirty := ""
		style := styleBranch
		if s.Dirty {
			dirty = "*"
			style = styleDirty
		}
		segs = append(segs, segment{
			text:  "git:(" + Sanitize(s.Branch) + dirty + ")",
			style: style,
			sep:   joiner,
		})
	}
	return segs
}

func statusLine(s snapshot.Snapshot, cfg config.Config) []segment {
	var segs []segment
	add := func(sg segment) {
		if len(segs) > 0 {
			sg.sep = sep
		}
		segs = append(segs, sg)
	}
	if cfg.ShowContext && s.ContextPercent != nil {
		pct := *s.ContextPercent
		add(segment{
			text:  fmt.Sprintf("Context %s %d%%", contextBar(pct), pct),
			style: contextStyle(pct, cfg),
		})
	}
	if cfg.ShowTokens && s.EstimatedPromptTokens != nil {
		add(segment{
			text:  "≈" + formatTokens(*s.EstimatedPromptTokens) + " prompt tokens (est.)",
			style: styleDim,
		})
	}
	if cfg.ShowUsage && s.Usage != "" {
		add(segment{text: Sanitize(s.Usage)})
	}
	if cfg.ShowApprovals && s.ApprovalsPending > 0 {
		word := "approvals"
		if s.ApprovalsPending == 1 {
			word = "approval"
		}
		add(segment{
			text:  fmt.Sprintf("⚠ %d %s pending", s.ApprovalsPending, word),
			style: styleYellow,
		})
	}
	if cfg.ShowAge && s.Age > 0 {
		add(segment{text: "⏱ " + formatAge(s.Age)})
	}
	return segs
}

// contextStyle maps a sourced percentage to green/yellow/red by the
// configured thresholds. It is only called with a real percentage.
func contextStyle(pct int, cfg config.Config) string {
	switch {
	case pct >= cfg.ContextCriticalPercent:
		return styleRed
	case pct >= cfg.ContextWarnPercent:
		return styleYellow
	default:
		return styleGreen
	}
}

func toolsLine(tools []snapshot.Tool, longRunning time.Duration) []segment {
	segs := make([]segment, 0, len(tools))
	for _, t := range tools {
		var b strings.Builder
		var style string
		switch {
		case t.Running && longRunning > 0 && t.Elapsed >= longRunning:
			b.WriteString("◐ ")
			style = styleBoldRed
		case t.Running:
			b.WriteString("◐ ")
			style = styleYellow
		case t.Failed:
			b.WriteString("✗ ")
			style = styleRed
		default:
			b.WriteString("✓ ")
			style = styleGreen
		}
		b.WriteString(Sanitize(t.Name))
		if t.Count > 1 {
			fmt.Fprintf(&b, " ×%d", t.Count)
		}
		if t.Running && t.Elapsed > 0 {
			fmt.Fprintf(&b, " (%s)", formatAge(t.Elapsed))
		}
		sg := segment{text: b.String(), style: style}
		if target := Sanitize(t.Target); target != "" {
			sg.pathPrefix = len([]rune(sg.text + ": "))
			sg.text += ": " + target
			sg.path = strings.Contains(target, "/")
		}
		if len(segs) > 0 {
			sg.sep = " | "
		}
		segs = append(segs, sg)
	}
	return segs
}

func agentsLine(agents []snapshot.Agent) []segment {
	segs := make([]segment, 0, len(agents))
	for _, a := range agents {
		var b strings.Builder
		style := styleGreen
		if a.Running {
			b.WriteString("◐ ")
			style = styleYellow
		} else {
			b.WriteString("✓ ")
		}
		if a.Kind != "" {
			b.WriteString(Sanitize(a.Kind))
			b.WriteString(" ")
		}
		b.WriteString(Sanitize(a.Label))
		if a.Elapsed > 0 {
			fmt.Fprintf(&b, " (%s)", formatAge(a.Elapsed))
		}
		sg := segment{text: b.String(), style: style}
		if len(segs) > 0 {
			sg.sep = " | "
		}
		segs = append(segs, sg)
	}
	return segs
}

func todosLine(t snapshot.Todos) []segment {
	return []segment{{
		text: fmt.Sprintf("▸ %s (%d/%d)", Sanitize(t.Current), t.Done, t.Total),
	}}
}

// contextBar renders a 10-cell bar for a 0–100 percentage.
func contextBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct / 10
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

// formatAge renders a compact duration: 45s, 12m, 1h05m.
func formatAge(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
