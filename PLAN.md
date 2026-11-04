# muse-hud — Plan

## Goal

Build `muse-hud`: a heads-up display for Muse, in the spirit of [claude-hud](https://github.com/jarrodwatts/claude-hud) for Claude Code.
claude-hud keeps model, project, git branch, context usage, rate limits, running tools, agents, and todo progress visible in two lines below the input.
`muse-hud` shows the Muse equivalents — model and effort, workspace and branch, context/usage where Muse exposes it, running tools and background work, subagents/workflows, todos and goals — without needing a modified Muse binary.

This file lives in the new repo at `../muse-hud` (freshly `git init`-ed on branch `main`, not yet committed; commit/push only when asked).
Tick boxes only with evidence actually run.

## Success Criteria

- [ ] Running `muse-hud print` in any workspace with a live or recent Muse session prints the two default HUD lines (model/workspace/branch; context or usage where available) in under 50 ms and always exits 0.
- [x] Optional tool, agent, and todo lines appear only when the session data really contains them; missing data hides a segment, never fabricates it.
- [ ] `muse-hud watch` in a side terminal pane follows the newest session live: a tool starting or finishing in Muse is visible within about a second.
- [x] A Muse version whose log format drifts degrades to fewer segments plus a clear `doctor` explanation — never a panic, a blank screen, or wrong numbers presented as fact.
- [x] The HUD is read-only and offline: no network, no writes outside its own cache/config, no reading of credential files, and only fixed `git` probes.
- [ ] `go test ./...`, `go vet ./...`, and a golden-fixture replay pass from a clean clone using only documented commands.

## Context (verified 2026-11-04)

- claude-hud (inspected: README, ARCHITECTURE.md, package.json, full file tree) is a Claude Code **statusLine plugin**: TypeScript on Node.js >= 22.18, fed JSON on stdin by Claude Code (`model`, `context_window`, `transcript_path`), parsing the session transcript JSONL and probing `git`/`jj`, rendering two lines by default plus optional tools/agents/todos lines, configured via `~/.claude/plugins/claude-hud/config.json`, and locked down with Node's permission model plus a separate collector process for the few fixed external commands. License: MIT.
- Muse here is **Muse Code 1.4.14** (`muse --version`). Its settings keys (`~/.config/muse/settings.json`: provider, model, reasoning efforts, `tui`, permissions) and CLI help expose **no statusLine/status-command hook**, so a literal in-TUI statusline port is not available through a documented surface. This is the central design constraint; Phase 4 re-checks it via the plugin system.
- Verified Muse data surfaces on this machine:
  - Session logs at `~/.local/share/muse/sessions/YYYY/MM/DD/<session-id>/session.jsonl`: one JSON object per line with envelope `record_type: event`, `sequence`, `recorded_at`, `payload_type`, and `payload {kind, record}`. Observed `payload_type` values include `runtime.session` (kinds `task`, `run`), `tool_batch.effect.started/terminal`, `run.model.configured`, `session.started`, and `session.workspace_branch.observed`. Nested records are not a documented stable API — treat as version-pinned input.
  - `muse trace inspect --session-log <file> --format json` projects a log into a summary (payload counts, task summaries, usage field). On a still-running session its `usage` was `null` and the load was partial, so live context % is an open spike, not a promise.
  - `muse export --session <id|path>|--last [--redacted]` emits one JSON document with messages, tool calls/results, approvals, model ids, and subagent lineage (offline, local files only).
  - `muse usage --json` (subscription allowance), `muse plugins …` (bundles with hooks; `validate` works without installing), `muse schema generate-json-schema|generate-ts` (the MSP wire schema for `muse serve`), and `muse session-message` for peer sessions.
  - `~/.local/share/muse/session-index.db` exists but is an internal index: do not build on it.
  - Whether `write_todos` state, goal progress, and per-tool names/targets are recoverable live (vs. only after a task settles) is unproven; Phase 1 spikes must answer this from real logs before any UI promises them.
- Toolchain on this machine: Go 1.26.8. User preference (standing): backend/tooling in Go.

## Approach

Build a small standalone Go binary with a strict split between **collection** (reads Muse's files/commands, probes `git`), **snapshot** (one normalized, version-tagged view of "what is happening now"), and **render** (pure functions from snapshot + config to lines). This mirrors claude-hud's collector/logic split without copying its TypeScript.
Two front ends share that core: `print` (one-shot, for status bars and shell prompts that re-run a command, and for scripting via `--json`) and `watch` (a live side-pane view tailing the newest session log).
Prefer the most stable input that carries each fact (`muse trace inspect` JSON / `muse export` / `muse usage --json` where they suffice) and use raw `session.jsonl` tailing only for liveness, behind a version check.
Start with Go standard library only; any external dependency needs a spike note in this plan before it is adopted.

### Key decisions

- **Language: Go, single static binary** (user preference; Go 1.26.8 verified locally). Rejected: TypeScript/Node like claude-hud — its runtime buys nothing once there is no statusLine stdin contract to honour, and a single binary is easier to install next to `muse`.
- **Form factor: companion binary first** (`print` + `watch`). Rejected for now: patching/forking Muse (no source in scope) and a web dashboard (heavier, later backlog). A Muse plugin is a Phase 4 spike, shipped only if hooks expose data or lifecycle the files do not.
- **Data source order:** documented CLI projections first, raw JSONL tail second, internal index never. Every snapshot field records its source; the renderer hides unsourced fields.
- **Naming:** repo and binary `muse-hud`; GitHub owner `CWBudde` (public repo), Go module path `github.com/CWBudde/muse-hud`.
- **License: MIT** (same as claude-hud; clean-room implementation — behaviour and layout ideas only, no copied code).

### Layout (from Phase 0)

```
cmd/muse-hud/          CLI entry: print | watch | doctor | version
internal/collect/      session discovery, trace/export/usage adapters, JSONL tailer, git probes
internal/snapshot/     normalized Snapshot types + derivation (tools, agents, todos, usage)
internal/render/       lines, colours, width/truncation, --json output
internal/config/       config file load/validate (~/.config/muse-hud/config.json via os.UserConfigDir)
testdata/fixtures/     synthetic/redacted session logs + golden outputs (never real transcripts)
```

Default render (Muse wording, claude-hud shape):

```
[muse-wonder-full-1.0 · high] │ Historyline-1914-1918-remake git:(main*)
Context █████░░░░░ 45% │ Usage … (only when sourced) │ ⏱ 12m
◐ bash: go test ./... | ✓ read_file ×3          ← tools (optional)
◐ subagent explore: Finding auth code (2m 15s)  ← agents (optional)
▸ Fix authentication bug (2/5)                  ← todos (optional)
```

### Non-goals (until re-planned)

- No modifying, wrapping, or screen-scraping the Muse TUI; no sending input to Muse.
- No network calls, telemetry, or account/billing scraping beyond `muse usage --json` output the user already has.
- No Windows support in v1 (Linux first, matching the verified machine; macOS paths noted as an assumption to test, not promised).
- No cost accounting, multi-session fleet view, `jj` support, or translations in v1 (backlog, Phase 6 ideas only).

## Steps

### Phase 0 — Basic setup

- [x] Create repo `../muse-hud` (`git init -b main`) and this `PLAN.md`.
- [x] `README.md` (what it is, honest "not a Muse statusline" note, build/run), `LICENSE` (MIT), `.gitignore` (Go + `build/`), `AGENTS.md` + `CLAUDE.md` import (Go-only, evidence-before-ticking, never commit real transcripts).
- [x] Go module skeleton: layout above, `cmd/muse-hud` with `version` and a stub `print` that renders a hard-coded snapshot through `internal/render` (proves the pipeline shape, no data yet).
- [x] One synthetic fixture generator (Go, under `testdata/`) that writes a fake `session.jsonl` with the observed envelope — the seed every later test replays.
- [x] `justfile` or `Makefile` with `build`, `test`, `lint` (= `go vet ./...` + formatting check), `run`; CI workflow running the same on push.
- [x] Confirm GitHub owner/name and Go module path, create the remote, first commit (owner `CWBudde`, public repo, module `github.com/CWBudde/muse-hud`; done 2026-11-04 on user request).
- Gate: clean clone → documented `build` succeeds, `muse-hud version` and stub `print` produce stable output, `go test ./...` green.

### Phase 1 — Data core (collect → snapshot)

- [x] Session discovery: newest session for a workspace (and `--session <id>`, `--file <session.jsonl>`, `--last` overrides), honouring the `~/.local/share/muse/sessions/YYYY/MM/DD/<id>/` layout; a `--muse-home` override for tests.
- [x] Version probe: parse `muse --version`; maintain a small support table (verified: 1.4.14). Unknown versions still attempt collection but tag the snapshot `unverified`.
- [x] Adapters, each independently testable against fixtures: `trace inspect --format json`, raw JSONL scanner (envelope + `payload_type` dispatch, unknown types ignored), `usage --json`, `export` (only where a fact is unavailable elsewhere — it is the heaviest source).
- [x] Spikes with written findings in `docs/spikes/`: (a) live context/usage source — finished vs. running sessions, `trace` usage field, `export` contents; (b) todos/goals/background-work visibility in logs; (c) tool name + target + start/end pairing from `tool_batch.effect.*` and task records; (d) subagent/workflow lineage. Each spike ends in "field sourced from X" or "field cut".
- [x] `snapshot.Snapshot` derivation: model/effort, workspace/branch, session age, tools (running with elapsed, recent counts), agents, todos (current + n/m), approvals pending, usage/context if sourced. Git branch/dirty state from fixed `git` probes in the session workspace (timeout-bounded, no shell).
- Gate: `muse-hud print --json` on this machine's real current session yields a snapshot whose model, branch, and tool states match a hand-check against `muse trace inspect` on the same log; fixture tests cover partial/corrupt/empty logs.

### Phase 2 — One-shot renderer (`print`)

- [x] Two default lines + optional tools/agents/todos lines; segment toggles and order in config; `--plain`/`NO_COLOR` and non-TTY automatically drop ANSI; colour thresholds for context (green → yellow → red) only if Phase 1 sourced real percentages.
- [x] Width handling: fit to `--width`/terminal width, truncate from the left of paths, sanitize all externally sourced strings (branch names, file targets, task text) against terminal escape injection.
- [x] `doctor` command: reports Muse version, sessions dir, chosen session, which adapters worked, and why a segment is hidden — the answer to "the HUD is blank".
- [x] Config file with validation and `MUSE_HUD_DISABLE=1` (print nothing, exit 0); bad config falls back to defaults with the error shown by `doctor`, never on the HUD lines.
- Gate: golden snapshot tests for every line combination; `print` p95 < 50 ms against a 10 MB fixture log (bounded tail read, not full parse); manual run beside a live `muse` session shows correct model/branch/tools.

### Phase 3 — Live watch mode

- [x] Tail the active `session.jsonl` incrementally (offset-tracked; survive truncation/rotation and session switch to a newer session dir); poll fallback with the same code path — a file-notify dependency only if a spike shows polling is measurably worse.
- [x] Redraw loop (250 ms–1 s) with the Phase 2 renderer; header shows session id/age; keys: `q` quit, `p` pause, segment toggles. Standard-library terminal handling first; a TUI library only via spike.
- [x] Attention cues: highlight long-running tools, failed tool batches, pending approvals, and finished background work (only states Phase 1 proved observable).
- Gate: side-by-side with a live session — start a long `go test`, spawn a subagent, update todos in Muse; each change appears in `watch` within about a second; idle CPU stays negligible over 10 minutes.

### Phase 4 — Integrations

- [x] Recipes in `docs/integrations.md`: status-bar/prompt one-liners using `print`, side-pane `watch`, `--json` for custom scripts. Each recipe tested exactly as written.
- [x] Muse plugin spike: scaffold a minimal bundle, `muse plugins validate` it, and test whether plugin hooks provide session/tool events fresher than log tailing. Ship a plugin only with a written win; otherwise record the finding and drop it.
- [x] MSP spike (research only): export `muse schema` and check whether `muse serve` exposes a subscribe/status surface a HUD could consume without owning the session. Record findings; no commitment to build on it.
- [x] Re-check the in-TUI question against the then-current `muse --help`/settings: if a status hook has appeared, add a Phase 4b to render through it reusing `internal/render` unchanged. (2026-11-04: none exists in 1.4.14, `docs/spikes/h-in-tui-status-hook.md` — no Phase 4b.)
- Gate: each shipped integration works from its doc alone on a clean shell; spikes either graduate with tests or are closed with findings in `docs/spikes/`.

### Phase 5 — Hardening and release

- [x] Security pass, claude-hud-inspired: read-only by construction, no network, no credential paths, fixed-argument `git` only, no `extra-cmd`-style feature unless behind an explicit opt-in env flag and called out as code execution; fuzz the JSONL/config parsers. (`docs/security.md`; fuzz targets in `internal/collect`, `internal/config`, `internal/render`, each run 10 s with no crashers.)
- [x] Compatibility: support table per Muse version, `doctor` diff hints on unknown `payload_type`s, and a fixture captured (synthetically) per supported log shape. (`docs/compatibility.md`, `collect.DriftHints`, `testdata/fixtures/synthetic-session-drift.jsonl` beside the verified 1.4.14 fixture.)
- [x] Docs: README with screenshot-style text examples, config reference, troubleshooting via `doctor`; `CHANGELOG.md`. (README final pass + `CHANGELOG.md` 0.1.0, untagged.)
- [ ] Release: versioned tags, `go install` instructions verified from a clean environment, prebuilt Linux binary attached if the repo is public by then. (`go install` into a temp `GOBIN` verified from the repo and from a fresh copy, installed binary passing version/doctor/print/watch smoke. Owner decided and repo public as of 2026-11-04; versioned tags and a prebuilt binary release are still outstanding.)
- Gate: full check suite green; a fresh-clone walkthrough (install → `doctor` → `print` → `watch`) completed by following only the README. (Walkthrough run from the README in-repo and from a fresh file copy — a literal `git clone` is impossible until the first commit exists; bounded `watch --frames` used for the watch step.)

### Phase 6 — Backlog (not committed)

- [ ] Ideas only, each needs its own plan amendment: cost/usage pacing, multi-session overview (peer sessions via `session-message`), web view, `jj`, themes/translations, macOS/Windows.

## Validation Plan

- Per phase: its gate above is the definition of done; phases do not start on a red gate.
- Standing checks from Phase 0 on: `go build ./...`, `go test ./...`, `go vet ./...`, formatting check — via the repo's single `build`/`test`/`lint` entry points, locally and in CI.
- Highest-risk check (Phase 1): the JSON-snapshot hand-verification against `muse trace inspect` on a real log. If the nested `runtime.session` records do not yield tool/todo facts, Phase 2's optional lines shrink to what `trace`/`export` prove — the plan already treats those segments as conditional for exactly this reason.
- Privacy check before every commit: fixtures are synthetic or mechanically redacted; grep the diff for real session ids, paths under `$HOME`, and transcript text.
- Manual checks that cannot be automated: side-by-side liveness (Phase 3 gate) and README-only walkthrough (Phase 5 gate) on this machine with Muse 1.4.14.

## Risks / Open Questions

- Risk: Muse log internals change without notice. Mitigation: version probe + support table, prefer CLI projections, degrade loudly in `doctor` and silently (fewer segments) in `print`.
- Risk: live context % may not exist in any local surface (on the inspected running session, `trace` reported `usage: null`). Mitigation: Phase 1 spike decides; the HUD must remain useful — and honest — without it.
- Risk: reading a log Muse is actively writing sees partial final lines. Mitigation: line-buffered tailer that only consumes newline-terminated records; corrupt-line fixtures.
- Risk: terminal escape injection via branch/file/task strings rendered into a status bar. Mitigation: sanitizer in `internal/render` with dedicated tests (claude-hud's terminal filter is the model).
- Answered (2026-11-04): GitHub owner is the private account `CWBudde`, public repo; Go module path `github.com/CWBudde/muse-hud`.
- Answered (Phase 4 spikes, 2026-11-04): plugin hooks expose no status/render surface and no win over log tailing (`docs/spikes/f-plugin-hooks.md`); MSP's stable schema does expose lease-free read/subscribe incl. context usage and todos, but only for sessions loaded on the connected host, and no daemon hosts the interactive TUI's session on this machine (`docs/spikes/g-msp.md`). Revisit MSP if daemon-hosted sessions become the default.
- Assumption (reversible): Linux-only v1 paths (`~/.local/share/muse`, `~/.config/muse-hud`), resolved via Go's `os.UserHomeDir`/`os.UserConfigDir` and overridable for tests.

## Sources

- [claude-hud repo](https://github.com/jarrodwatts/claude-hud)
- [claude-hud README](https://raw.githubusercontent.com/jarrodwatts/claude-hud/main/README.md)
- [claude-hud ARCHITECTURE](https://raw.githubusercontent.com/jarrodwatts/claude-hud/main/ARCHITECTURE.md)
- [claude-hud package.json](https://raw.githubusercontent.com/jarrodwatts/claude-hud/main/package.json)
- [claude-hud file tree](https://api.github.com/repos/jarrodwatts/claude-hud/git/trees/main?recursive=1)
- Local evidence (this machine, 2026-11-04): `muse --version` → Muse Code 1.4.14; `muse --help`, `muse plugins --help`, `muse trace inspect --help`, `muse export --help`, `muse usage --help`, `muse schema --help`; key inventory of `~/.config/muse/settings.json`; envelope/`payload_type` inventory of the current `session.jsonl`; `go version` → go1.26.8.
