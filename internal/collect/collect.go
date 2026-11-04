// Package collect reads Muse's local data surfaces (session logs, CLI
// projections) and fixed-argument git probes, and turns them into a
// snapshot. It is read-only and offline by construction.
//
// Phase 1: session discovery, a Muse version probe with a support
// table, adapters for trace/usage/export, the raw JSONL deriver, and
// git branch/dirty probes. See docs/spikes/ for which surface sources
// (or cuts) each snapshot field.
package collect

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// Options controls Collect. The zero value collects the newest session
// for Workspace (or the newest overall when Workspace is empty) from
// the default Muse home, using only the raw log and git probes.
type Options struct {
	MuseHome    string
	File        string
	SessionID   string
	Last        bool
	Workspace   string
	Now         time.Time // zero means time.Now()
	MuseBin     string    // default "muse"
	Runner      Runner    // default ExecRunner (tests inject fixtures)
	GitTimeout  time.Duration
	SkipGit     bool
	SkipVersion bool
	UseTrace    bool // also run trace inspect; fills tools only if the log had none
	UseUsage    bool // also run usage --json for the allowance segment
	FullScan    bool // parse the whole log instead of the bounded head+tail window
	HeadBytes   int64
	TailBytes   int64
}

// Result is a collected snapshot plus how it was obtained.
type Result struct {
	Snapshot       snapshot.Snapshot
	Session        Session
	Support        Support
	Stats          Stats
	Trace          *TraceReport // set when UseTrace ran (even on partial logs)
	Warnings       []string     // non-fatal adapter/version problems, for doctor
	FellBackToLast bool         // workspace matched nothing; newest overall was used
}

// Collect discovers a session, derives a snapshot from its log, then
// refines branch/dirty with live git probes in the session workspace.
// Missing optional surfaces degrade to fewer sourced fields, never to
// fabricated ones.
func Collect(ctx context.Context, opts Options) (Result, error) {
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	var res Result

	probeVersion := func() {
		if opts.SkipVersion {
			return
		}
		support, err := ProbeVersion(ctx, runner, opts.MuseBin)
		if err != nil {
			res.Warnings = append(res.Warnings, err.Error())
			return
		}
		support.Source = "muse --version"
		res.Support = support
	}

	session, fellBack, err := discover(opts)
	if err != nil {
		probeVersion() // doctor still reports the binary version
		return res, err
	}
	res.Session = session
	res.FellBackToLast = fellBack

	f, err := os.Open(session.Path)
	if err != nil {
		return res, fmt.Errorf("open session log: %w", err)
	}
	var snap snapshot.Snapshot
	var stats Stats
	var derr error
	if opts.FullScan {
		snap, stats, derr = Derive(f, now)
	} else {
		snap, stats, derr = DeriveFile(f, now, opts.HeadBytes, opts.TailBytes)
	}
	_ = f.Close()
	if derr != nil {
		res.Warnings = append(res.Warnings, derr.Error())
	}
	if stats.Bounded {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"log is %d bytes: derived from a bounded head+tail window (%d bytes scanned); tool/agent/approval state covers the tail window only (use --full-scan for whole-log state)",
			stats.FileBytes, stats.ScannedBytes,
		))
	}
	res.Stats = stats
	if snap.SessionID == "" {
		snap.SessionID = session.ID
	}
	// Support is assessed against the version that wrote the log when
	// the log carries it (session metadata build): that is the shape
	// being parsed, and it spares a `muse --version` subprocess on
	// every print (measured p95 ~74 ms — over the whole HUD budget).
	// The binary probe remains the fallback for logs without it.
	if !opts.SkipVersion {
		if v := ParseVersion(snap.MuseVersion); snap.MuseVersion != "" && v.Version != "" {
			v.Source = "session log build"
			res.Support = v
		} else {
			probeVersion()
			if snap.MuseVersion == "" {
				snap.MuseVersion = res.Support.Version
			}
		}
	}
	snap.Unverified = !res.Support.Verified

	if !opts.SkipGit {
		dir := snap.WorkspaceRoot
		if dir == "" {
			dir = opts.Workspace
		}
		if dir != "" {
			if gs, gerr := ProbeGit(ctx, dir, opts.GitTimeout); gerr == nil && gs.Branch != "" {
				snap.Branch = gs.Branch
				snap.Dirty = gs.Dirty
				if snap.Sources == nil {
					snap.Sources = map[string]snapshot.Source{}
				}
				snap.Sources["branch"] = "git"
			} else if gerr != nil {
				res.Warnings = append(res.Warnings, gerr.Error())
			}
		}
	}

	if opts.UseTrace {
		if rep, terr := TraceInspect(ctx, runner, opts.MuseBin, session.Path); terr == nil {
			res.Trace = rep
			if len(snap.Tools) == 0 {
				if tools := toolsFromTrace(rep); len(tools) > 0 {
					snap.Tools = tools
					snap.Sources["tools"] = "trace"
				}
			}
		} else {
			res.Warnings = append(res.Warnings, terr.Error())
		}
	}

	if opts.UseUsage {
		if rep, uerr := Usage(ctx, runner, opts.MuseBin); uerr == nil {
			if s := FormatUsage(rep); s != "" {
				snap.Usage = s
				snap.Sources["usage"] = "usage"
			}
		} else {
			res.Warnings = append(res.Warnings, uerr.Error())
		}
	}

	res.Snapshot = snap
	return res, nil
}

// discover resolves the session per Options, falling back from an
// unmatched workspace to the newest session overall so a HUD in a
// fresh terminal still shows the live session (its workspace name is
// sourced from the log, never assumed to be the cwd).
func discover(opts Options) (Session, bool, error) {
	switch {
	case opts.File != "" || opts.SessionID != "" || opts.Last:
		s, err := Find(FindOptions{MuseHome: opts.MuseHome, File: opts.File, SessionID: opts.SessionID, Last: opts.Last})
		return s, false, err
	case opts.Workspace != "":
		s, err := Find(FindOptions{MuseHome: opts.MuseHome, Workspace: opts.Workspace})
		if err == nil {
			return s, false, nil
		}
		s, lerr := Find(FindOptions{MuseHome: opts.MuseHome, Last: true})
		if lerr != nil {
			return Session{}, false, err
		}
		return s, true, nil
	default:
		s, err := Find(FindOptions{MuseHome: opts.MuseHome, Last: true})
		return s, false, err
	}
}

// toolsFromTrace aggregates trace tool task summaries into snapshot
// tools, used only when the raw log yielded none (e.g. a future log
// shape without tool_batch records).
func toolsFromTrace(rep *TraceReport) []snapshot.Tool {
	counts := map[string]int{}
	failed := map[string]bool{}
	var order []string
	var tools []snapshot.Tool
	for _, t := range rep.ToolTasks() {
		name := t.TaskKind
		if len(name) > len("tool.") && name[:len("tool.")] == "tool." {
			name = name[len("tool."):]
		}
		if t.Terminal == nil && (t.State == "active" || t.State == "running") {
			tools = append(tools, snapshot.Tool{Name: name, Running: true})
			continue
		}
		if counts[name] == 0 {
			order = append(order, name)
		}
		counts[name]++
		if t.Terminal != nil {
			failed[name] = *t.Terminal == "failed" || *t.Terminal == "error"
		}
	}
	for _, name := range order {
		tools = append(tools, snapshot.Tool{Name: name, Count: counts[name], Failed: failed[name]})
	}
	return tools
}

// StubSnapshot returns a deterministic, hard-coded snapshot used by
// `print --stub` to prove the render pipeline without live data. Every
// value is labelled as stub-sourced so it can never be mistaken for a
// real session.
func StubSnapshot() snapshot.Snapshot {
	return snapshot.Snapshot{
		SchemaVersion: snapshot.SchemaVersion,
		Model:         "stub-model",
		Effort:        "high",
		Workspace:     "example-workspace",
		Branch:        "main",
		Dirty:         true,
		Age:           12 * time.Minute,
		Tools: []snapshot.Tool{
			{Name: "bash", Target: "go test ./...", Running: true},
			{Name: "read_file", Count: 3},
		},
		Todos: &snapshot.Todos{Current: "Write the plan", Done: 1, Total: 3},
		Sources: map[string]snapshot.Source{
			"model":     "stub",
			"workspace": "stub",
			"branch":    "stub",
			"age":       "stub",
			"tools":     "stub",
			"todos":     "stub",
		},
	}
}
