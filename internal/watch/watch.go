// Package watch is the terminal-free model behind `muse-hud watch`:
// it owns the tailer, session (re)discovery, the throttled git refresh,
// pause and segment toggles, and the attention cues, and renders frames
// through the Phase 2 renderer. The cmd layer only does terminal I/O
// and key plumbing, so the whole loop is testable without a TTY.
package watch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/CWBudde/muse-hud/internal/collect"
	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// Default thresholds/intervals for the watch loop. Discovery walks the
// sessions dir and git spawns subprocesses, so both are throttled well
// below the redraw rate; the tail poll itself is one stat per tick.
const (
	DefaultDiscoverInterval = 2 * time.Second
	DefaultGitInterval      = 5 * time.Second
	DefaultLongRunning      = 30 * time.Second
	finishedCueTTL          = 30 * time.Second
)

// Options configures a Model. The zero value watches the newest
// session for Collect.Workspace (via collect discovery) with default
// intervals. Discover is injectable for tests.
type Options struct {
	// Collect selects the session (MuseHome/File/SessionID/Last/
	// Workspace) and bounds the seed window.
	Collect collect.Options
	// Discover resolves the current session; nil uses
	// collect.ResolveSession(Collect).
	Discover func() (collect.Session, error)
	// Config is the initial render config; toggles mutate a copy.
	Config config.Config

	DiscoverInterval time.Duration
	GitInterval      time.Duration
	LongRunning      time.Duration
	// SkipGit disables the git branch/dirty refresh (tests, --file on
	// synthetic logs outside any repo still probes only when the log
	// itself names a workspace that exists).
	SkipGit bool
}

// Cue is one attention item: a state Phase 1 proved observable
// (long-running/failed tools, pending approvals) or a transition the
// watch itself observed (a running agent finishing between polls).
type Cue struct {
	Kind string // "long-running" | "failed" | "approvals" | "finished"
	Text string
}

// Model is the watch state machine. It is single-threaded: the cmd
// loop calls HandleKey and Update from one goroutine.
type Model struct {
	opts     Options
	discover func() (collect.Session, error)
	cfg      config.Config

	tailer  *collect.Tailer
	session collect.Session
	snap    snapshot.Snapshot
	waiting string // non-empty: no session yet (or it vanished)
	paused  bool

	lastDiscover time.Time
	lastGit      time.Time
	git          collect.GitState
	gitSourced   bool

	runningAgents int
	finishedCues  []timedCue
	switches      int
}

type timedCue struct {
	cue Cue
	at  time.Time
}

// New returns a Model. No I/O happens until the first Update.
func New(opts Options) *Model {
	if opts.DiscoverInterval <= 0 {
		opts.DiscoverInterval = DefaultDiscoverInterval
	}
	if opts.GitInterval <= 0 {
		opts.GitInterval = DefaultGitInterval
	}
	if opts.LongRunning <= 0 {
		opts.LongRunning = DefaultLongRunning
	}
	discover := opts.Discover
	if discover == nil {
		collectOpts := opts.Collect
		discover = func() (collect.Session, error) {
			s, _, err := collect.ResolveSession(collectOpts)
			return s, err
		}
	}
	return &Model{opts: opts, discover: discover, cfg: opts.Config, runningAgents: -1}
}

// Paused reports the pause state.
func (m *Model) Paused() bool { return m.paused }

// Session returns the session currently followed (zero while waiting).
func (m *Model) Session() collect.Session { return m.session }

// Snapshot returns the latest derived snapshot (frozen while paused).
func (m *Model) Snapshot() snapshot.Snapshot { return m.snap }

// Config returns the effective render config (after key toggles).
func (m *Model) Config() config.Config { return m.cfg }

// Switches counts session switches to a newer session.
func (m *Model) Switches() int { return m.switches }

// HandleKey applies one key byte: q quit, p pause/resume, and the
// segment-line toggles t (tools), a (agents), d (todos). It reports
// whether watch should quit. Unknown keys are ignored.
func (m *Model) HandleKey(key byte) (quit bool) {
	switch key {
	case 'q', 3: // q or Ctrl-C (raw mode delivers ^C as a byte)
		return true
	case 'p':
		m.paused = !m.paused
	case 't':
		m.cfg.ShowTools = !m.cfg.ShowTools
	case 'a':
		m.cfg.ShowAgents = !m.cfg.ShowAgents
	case 'd':
		m.cfg.ShowTodos = !m.cfg.ShowTodos
	}
	return false
}

// Update advances the model: (re)discover the session when due, poll
// the tailer, refresh git when due, and rebuild the snapshot. While
// paused nothing is polled and the rendered snapshot stays frozen.
// The returned bool reports whether anything visible may have changed.
func (m *Model) Update(ctx context.Context, now time.Time) bool {
	if m.paused {
		return false
	}
	changed := false
	if m.tailer == nil || now.Sub(m.lastDiscover) >= m.opts.DiscoverInterval {
		m.lastDiscover = now
		changed = m.rediscover(now) || changed
	}
	if m.tailer == nil {
		return changed
	}
	if polled, err := m.tailer.Poll(); err != nil {
		// The file vanished mid-watch (rotation gap): keep the last
		// snapshot on screen, note the state, and let the next
		// discovery pick up whatever replaces it.
		if m.waiting == "" {
			m.waiting = "session log unavailable, waiting…"
			changed = true
		}
	} else {
		if m.waiting != "" {
			m.waiting = ""
			changed = true
		}
		changed = polled || changed
	}
	m.refreshGit(ctx, now)
	m.rebuild(now)
	return changed
}

// rediscover resolves the current session and switches the tailer when
// discovery names a different log that is at least as fresh as the one
// being followed (a newer session in a side pane must win, an older
// fallback must not flap the view backwards).
func (m *Model) rediscover(now time.Time) bool {
	sess, err := m.discover()
	if err != nil {
		if m.tailer == nil {
			m.waiting = "waiting for a Muse session…"
			return true
		}
		return false
	}
	if m.tailer != nil && sess.Path == m.session.Path {
		m.session = sess
		return false
	}
	if m.tailer != nil && sess.ModTimeUnixMs > 0 {
		if fi, serr := os.Stat(m.session.Path); serr == nil &&
			sess.ModTimeUnixMs < fi.ModTime().UnixMilli() {
			return false // candidate is staler than the followed log
		}
	}
	tailer, err := collect.NewTailer(sess.Path, m.opts.Collect.HeadBytes, m.opts.Collect.TailBytes)
	if err != nil {
		if m.tailer == nil {
			m.waiting = "waiting for a Muse session…"
			return true
		}
		return false
	}
	if m.tailer != nil {
		m.switches++
	}
	m.tailer = tailer
	m.session = sess
	m.waiting = ""
	m.gitSourced = false
	m.lastGit = time.Time{}
	m.runningAgents = -1
	m.finishedCues = nil
	m.rebuild(now)
	return true
}

func (m *Model) refreshGit(ctx context.Context, now time.Time) {
	if m.opts.SkipGit || m.tailer == nil {
		return
	}
	if !m.lastGit.IsZero() && now.Sub(m.lastGit) < m.opts.GitInterval {
		return
	}
	m.lastGit = now
	dir := m.snap.WorkspaceRoot
	if dir == "" {
		dir = m.opts.Collect.Workspace
	}
	if dir == "" {
		return
	}
	if gs, err := collect.ProbeGit(ctx, dir, m.opts.Collect.GitTimeout); err == nil && gs.Branch != "" {
		m.git = gs
		m.gitSourced = true
	}
}

// rebuild derives the snapshot from the tailer at now, applies the
// cached git refinement and the session-id/version bookkeeping Collect
// does for print, and tracks agent finish transitions for cues.
func (m *Model) rebuild(now time.Time) {
	if m.tailer == nil {
		return
	}
	snap := m.tailer.Snapshot(now)
	if snap.SessionID == "" {
		snap.SessionID = m.session.ID
	}
	if v := collect.ParseVersion(snap.MuseVersion); snap.MuseVersion != "" && v.Version != "" {
		snap.Unverified = !v.Verified
	} else if snap.MuseVersion == "" {
		snap.Unverified = true
	}
	if m.gitSourced {
		snap.Branch = m.git.Branch
		snap.Dirty = m.git.Dirty
		if snap.Sources == nil {
			snap.Sources = map[string]snapshot.Source{}
		}
		snap.Sources["branch"] = "git"
	}
	running := 0
	for _, a := range snap.Agents {
		if a.Running {
			running++
		}
	}
	if m.runningAgents >= 0 && running < m.runningAgents {
		// A background agent finished between polls. The snapshot
		// carries no finish timestamp, so the cue is anchored at
		// the watch's own observation time and expires.
		for _, a := range snap.Agents {
			if !a.Running {
				label := a.Label
				if a.Kind != "" {
					label = a.Kind + " " + label
				}
				m.finishedCues = append(m.finishedCues, timedCue{
					cue: Cue{Kind: "finished", Text: "✓ finished: " + label},
					at:  now,
				})
				break
			}
		}
	}
	m.runningAgents = running
	kept := m.finishedCues[:0]
	for _, fc := range m.finishedCues {
		if now.Sub(fc.at) <= finishedCueTTL {
			kept = append(kept, fc)
		}
	}
	m.finishedCues = kept
	m.snap = snap
}

// Attention returns the current attention cues, in a stable order:
// pending approvals, failed batches, long-running tools, then
// recently finished background work. Only states Phase 1 proved
// observable appear (spikes b–d): no todos/goals, no tool targets.
func (m *Model) Attention() []Cue {
	var cues []Cue
	if m.snap.ApprovalsPending > 0 {
		word := "approvals"
		if m.snap.ApprovalsPending == 1 {
			word = "approval"
		}
		cues = append(cues, Cue{Kind: "approvals", Text: fmt.Sprintf("⚠ %d %s pending", m.snap.ApprovalsPending, word)})
	}
	for _, t := range m.snap.Tools {
		if t.Failed && !t.Running {
			cues = append(cues, Cue{Kind: "failed", Text: "✗ failed: " + t.Name})
		}
	}
	for _, t := range m.snap.Tools {
		if t.Running && t.Elapsed >= m.opts.LongRunning {
			cues = append(cues, Cue{Kind: "long-running", Text: fmt.Sprintf("⏳ long-running: %s (%s)", t.Name, compactDuration(t.Elapsed))})
		}
	}
	for _, fc := range m.finishedCues {
		cues = append(cues, fc.cue)
	}
	return cues
}

// View renders one frame: header (session id + age + state), the
// Phase 2 HUD lines, any attention cues, and a key-help footer. All
// externally sourced strings pass through render.Sanitize (the HUD
// lines sanitize internally; header/cue values are sanitized here).
func (m *Model) View(opts render.Options) []string {
	var lines []string
	lines = append(lines, m.header(opts))
	if m.waiting != "" && m.tailer == nil {
		lines = append(lines, render.Sanitize(m.waiting))
	} else {
		if opts.LongRunning <= 0 {
			opts.LongRunning = m.opts.LongRunning
		}
		lines = append(lines, render.LinesWithOptions(m.snap, m.cfg, opts)...)
		for _, cue := range m.Attention() {
			lines = append(lines, "attention: "+render.Sanitize(cue.Text))
		}
		if m.waiting != "" {
			lines = append(lines, render.Sanitize(m.waiting))
		}
	}
	lines = append(lines, "q quit · p pause · t tools · a agents · d todos")
	return lines
}

func (m *Model) header(opts render.Options) string {
	var b strings.Builder
	b.WriteString("muse-hud watch")
	if id := m.snap.SessionID; id != "" {
		b.WriteString(" — session " + render.Sanitize(id))
	} else if m.session.ID != "" {
		b.WriteString(" — session " + render.Sanitize(m.session.ID))
	}
	if m.snap.Age > 0 {
		b.WriteString(" · age " + compactDuration(m.snap.Age))
	}
	if m.snap.Ended {
		b.WriteString(" · ended")
	}
	if m.snap.Unverified && m.snap.MuseVersion != "" {
		b.WriteString(" · muse " + render.Sanitize(m.snap.MuseVersion) + " (unverified)")
	}
	if m.tailer != nil && m.tailer.Bounded() {
		b.WriteString(" · bounded window")
	}
	if m.paused {
		b.WriteString(" · PAUSED")
	}
	return b.String()
}

func compactDuration(d time.Duration) string {
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
