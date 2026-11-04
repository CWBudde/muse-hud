package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// cmdDoctor prints a diagnostic report: Muse version, sessions dir,
// the chosen session, which adapters worked, and why each hidden
// segment is hidden. Exit code is 1 when no session could be collected
// (the report still prints), 0 otherwise, 2 on flag errors.
func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	var sf sessionFlags
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addSessionFlags(fs, &sf)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "muse-hud doctor: %v\n", err)
		return 2
	}

	lc := loadConfigFull(sf.config)
	res, collectErr := collect.Collect(context.Background(), sf.collectOptions())
	fmt.Fprint(stdout, formatDoctor(sf, lc, res, collectErr))
	if collectErr != nil {
		return 1
	}
	return 0
}

func formatDoctor(sf sessionFlags, lc loadedConfig, res collect.Result, collectErr error) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("muse-hud doctor (muse-hud %s)", Version)

	// Muse version.
	switch {
	case res.Support.Version == "":
		line("muse: version unknown (probe failed; collection still attempted, snapshot unverified)")
	case res.Support.Verified:
		line("muse: %s (verified, from %s)", res.Support.Version, res.Support.Source)
	default:
		line("muse: %s (UNVERIFIED, from %s — log shapes may have drifted; segments degrade, numbers are never guessed)", res.Support.Version, res.Support.Source)
	}
	line("support: verified Muse versions: %s (docs/compatibility.md); %d known payload_type values",
		strings.Join(collect.VerifiedVersionList(), ", "), len(collect.KnownPayloadTypes()))

	// Disabled state + config.
	if config.Disabled() {
		line("disabled: %s=1 — print emits nothing; unset it to re-enable", config.DisableEnv)
	}
	switch {
	case lc.err != nil:
		line("config: %s INVALID — using defaults (%v)", lc.path, lc.err)
	case lc.exists:
		line("config: %s (loaded)", lc.path)
	default:
		line("config: %s (missing — using defaults)", lc.path)
	}

	// Sessions dir + chosen session.
	if dir, err := collect.SessionsDir(sf.museHome); err == nil {
		state := "exists"
		if _, serr := os.Stat(dir); serr != nil {
			state = "MISSING"
		}
		line("sessions dir: %s (%s)", render.Sanitize(dir), state)
	}
	if collectErr != nil {
		line("session: NONE — %v", collectErr)
	} else {
		s := res.Session
		note := ""
		if res.FellBackToLast {
			note = " (no session matched the workspace; newest overall used)"
		}
		if res.Snapshot.Ended {
			note += " (ended)"
		}
		line("session: %s%s", render.Sanitize(s.ID), note)
		line("  file: %s (%d bytes, modified %s)", render.Sanitize(s.Path), s.Size,
			time.UnixMilli(s.ModTimeUnixMs).Format(time.RFC3339))
	}

	// Adapters.
	line("adapters:")
	if collectErr != nil {
		line("  session.jsonl: not read (no session)")
	} else {
		st := res.Stats
		scan := "full parse"
		if st.Bounded {
			scan = fmt.Sprintf("bounded head+tail window (%d of %d bytes)", st.ScannedBytes, st.FileBytes)
		}
		line("  session.jsonl: ok — %s, %d envelopes, %d skipped lines", scan, st.Envelopes, st.Skipped)
		if unknown := sortedKeys(st.UnknownPayloadTypes); len(unknown) > 0 {
			line("  unknown payload_type values (ignored, format drift?): %s", strings.Join(unknown, ", "))
			for _, h := range collect.DriftHints(st.UnknownPayloadTypes) {
				line("    ? %s (×%d): %s", render.Sanitize(h.Unknown), h.Count, render.Sanitize(h.Hint))
			}
			line("    unknown types are never guessed at: segments they might feed stay hidden; see docs/compatibility.md")
		}
	}
	switch src := res.Snapshot.Sources["branch"]; {
	case src == "git":
		line("  git probes: ok (branch/dirty from git)")
	case res.Snapshot.Branch != "":
		line("  git probes: no live result — branch from the session log (%s)", src)
	default:
		line("  git probes: no branch (not a git workspace, or probe failed — see warnings)")
	}
	switch {
	case !sf.withTrace:
		line("  muse trace inspect: not run (opt-in --with-trace)")
	case res.Trace != nil:
		line("  muse trace inspect: ok (load_status=%s)", render.Sanitize(res.Trace.LoadStatus))
	default:
		line("  muse trace inspect: failed (see warnings)")
	}
	switch {
	case !sf.withUsage:
		line("  muse usage --json: not run (opt-in --with-usage)")
	case res.Snapshot.Sourced("usage"):
		line("  muse usage --json: ok")
	default:
		line("  muse usage --json: ran but carried no allowance percentages (see warnings)")
	}
	line("  muse export: never run by print/doctor (heaviest surface; no HUD fact needs it)")

	// Segments.
	line("segments:")
	for _, seg := range segmentReports(res.Snapshot, lc.cfg, sf) {
		line("  %s", seg)
	}

	if len(res.Warnings) > 0 {
		line("warnings:")
		for _, w := range res.Warnings {
			line("  - %s", render.Sanitize(w))
		}
	}

	preview := render.String(res.Snapshot, lc.cfg)
	line("print preview (plain):")
	if collectErr != nil || preview == "" {
		line("  (no lines — every segment hidden; see above)")
	} else {
		for _, l := range strings.Split(preview, "\n") {
			line("  %s", l)
		}
	}
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, fmt.Sprintf("%s×%d", render.Sanitize(k), m[k]))
	}
	sort.Strings(keys)
	return keys
}

// segmentReports explains, per HUD segment, whether it is shown and
// (when hidden) exactly why: toggled off, unsourced in the scanned
// window, or cut because Muse exposes no such fact.
func segmentReports(s snapshot.Snapshot, cfg config.Config, sf sessionFlags) []string {
	src := func(group string) string {
		if v := s.Sources[group]; v != "" {
			return ", source " + string(v)
		}
		return ""
	}
	shown := func(name, detail string) string { return fmt.Sprintf("%s: shown (%s)", name, detail) }
	hidden := func(name, why string) string { return fmt.Sprintf("%s: hidden — %s", name, why) }

	var out []string
	switch {
	case !cfg.ShowModel:
		out = append(out, hidden("model", "showModel=false in config"))
	case s.Model != "":
		detail := render.Sanitize(s.Model) + src("model")
		if s.Unverified {
			detail += ", unverified muse version"
		}
		out = append(out, shown("model", detail))
	default:
		out = append(out, hidden("model", "no model record (run.model.configured/session metadata) in the scanned window"))
	}
	switch {
	case !cfg.ShowModel:
		out = append(out, hidden("effort", "shown with the model segment, which is off"))
	case s.Effort != "":
		out = append(out, shown("effort", render.Sanitize(s.Effort)+src("effort")))
	default:
		out = append(out, hidden("effort", "no resolved effort recorded for the current run in the scanned window"))
	}
	switch {
	case !cfg.ShowWorkspace:
		out = append(out, hidden("workspace", "showWorkspace=false in config"))
	case s.Workspace != "":
		out = append(out, shown("workspace", render.Sanitize(s.Workspace)+src("workspace")))
	default:
		out = append(out, hidden("workspace", "no workspace_root in the scanned window"))
	}
	switch {
	case !cfg.ShowBranch:
		out = append(out, hidden("branch", "showBranch=false in config"))
	case s.Branch != "":
		out = append(out, shown("branch", render.Sanitize(s.Branch)+src("branch")))
	default:
		out = append(out, hidden("branch", "neither the log nor the git probes yielded a branch"))
	}
	switch {
	case !cfg.ShowContext:
		out = append(out, hidden("context", "showContext=false in config"))
	case s.ContextPercent != nil:
		out = append(out, shown("context", fmt.Sprintf("%d%%%s", *s.ContextPercent, src("context"))))
	default:
		why := "no Muse surface provides a live context percentage (field cut, docs/spikes/a-context-usage.md)"
		if s.EstimatedPromptTokens != nil {
			why += fmt.Sprintf("; estimated prompt tokens %d have no verified denominator and are never converted to %%", *s.EstimatedPromptTokens)
		}
		out = append(out, hidden("context", why))
	}
	switch {
	case !cfg.ShowUsage:
		out = append(out, hidden("usage", "showUsage=false in config"))
	case s.Usage != "":
		out = append(out, shown("usage", render.Sanitize(s.Usage)+src("usage")))
	case !sf.withUsage:
		out = append(out, hidden("usage", "allowance is opt-in; pass --with-usage to probe 'muse usage --json'"))
	default:
		out = append(out, hidden("usage", "usage report carried no allowance percentages"))
	}
	switch {
	case !cfg.ShowApprovals:
		out = append(out, hidden("approvals", "showApprovals=false in config"))
	case s.ApprovalsPending > 0:
		out = append(out, shown("approvals", fmt.Sprintf("%d pending%s", s.ApprovalsPending, src("approvals"))))
	case s.Sourced("approvals"):
		out = append(out, hidden("approvals", "0 pending (all recorded requests decided)"))
	default:
		out = append(out, hidden("approvals", "no approval records in the scanned window"))
	}
	switch {
	case !cfg.ShowAge:
		out = append(out, hidden("age", "showAge=false in config"))
	case s.Age > 0:
		out = append(out, shown("age", s.Age.Round(time.Second).String()+src("age")))
	default:
		out = append(out, hidden("age", "no usable timestamps in the scanned window"))
	}
	running := 0
	for _, t := range s.Tools {
		if t.Running {
			running++
		}
	}
	switch {
	case !cfg.ShowTools:
		out = append(out, hidden("tools", "showTools=false in config"))
	case len(s.Tools) > 0:
		out = append(out, shown("tools", fmt.Sprintf("%d listed, %d running%s", len(s.Tools), running, src("tools"))))
	case s.Sourced("tools"):
		out = append(out, hidden("tools", "tool records seen but none running or recently finished in the scanned window"))
	default:
		out = append(out, hidden("tools", "no tool_batch.effect records in the scanned window"))
	}
	runningAgents := 0
	for _, a := range s.Agents {
		if a.Running {
			runningAgents++
		}
	}
	switch {
	case !cfg.ShowAgents:
		out = append(out, hidden("agents", "showAgents=false in config"))
	case len(s.Agents) > 0:
		out = append(out, shown("agents", fmt.Sprintf("%d listed, %d running%s", len(s.Agents), runningAgents, src("agents"))))
	default:
		out = append(out, hidden("agents", "no subagent/workflow records in the scanned window"))
	}
	switch {
	case !cfg.ShowTodos:
		out = append(out, hidden("todos", "showTodos=false in config"))
	case s.Todos != nil && s.Todos.Total > 0:
		out = append(out, shown("todos", fmt.Sprintf("%d/%d%s", s.Todos.Done, s.Todos.Total, src("todos"))))
	default:
		out = append(out, hidden("todos", "todo list content is in no local Muse surface — write_todos is visible only as a tool name (field cut, docs/spikes/b-todos-goals-background.md)"))
	}
	return out
}
