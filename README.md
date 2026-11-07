# muse-hud

A heads-up display for Muse, in the spirit of
[claude-hud](https://github.com/jarrodwatts/claude-hud) for Claude Code: model
and effort, workspace and git branch, context/usage where Muse exposes it,
running tools, subagents/workflows, and todo progress — kept visible without
modifying the Muse binary.

> **Honest note:** Muse exposes no statusLine/status-command hook for
> custom content, so this is **not** an in-TUI Muse statusline. Muse does
> have a built-in HUD with per-segment toggles (`tui.hud.model`,
> `.effort`, `.permissions`, `.context`, `.branch`, `.tokens`, `.cost`,
> `.quota` in Muse's `settings.json`); it renders only Muse's own
> segments — including live context and quota, which muse-hud cannot
> source externally — and accepts no custom lines
> (`docs/spikes/h-in-tui-status-hook.md`, 2026-11-07 correction).
> `muse-hud` is the companion binary for the rest (running tools,
> agents, session age): run `muse-hud print` from a status bar, shell
> prompt, or script, or `muse-hud watch` in a side terminal pane. It
> reads Muse's local session data read-only and offline.

Status: **0.1.0** (see `CHANGELOG.md`). Verified Muse version:
**1.4.14** (`docs/compatibility.md`).

## What it looks like

`print --stub` renders a hard-coded snapshot — the same layout a live
session produces, byte-stable on every machine:

```text
$ muse-hud print --stub
[stub-model · high] │ example-workspace git:(main*)
⏱ 12m
◐ bash: go test ./... | ✓ read_file ×3
▸ Write the plan (1/3)
```

On a live session the values come from the newest session log for
the workspace; segments without a real source are simply absent
(the shape below is real, the values are illustrative):

```text
$ muse-hud print
[<model> · high] │ <workspace> git:(main*)
⏱ 38m
✓ bash ×9 | ✓ web_fetch ×5
◐ subagent workflow-agent (12m)
```

`watch` draws the same lines with a header and attention cues:

```text
muse-hud watch — session <session-id> · age 38m
[<model> · high] │ <workspace> git:(main*)
⚠ 1 approval pending │ ⏱ 38m
◐ bash (45s) | ✓ read_file ×3
attention: ⚠ 1 approval pending
attention: ⏳ long-running: bash (45s)
q quit · p pause · t tools · a agents · d todos
```

And `print --json` emits the snapshot itself for scripts
(`docs/integrations.md` has `jq` recipes); unsourced values are
omitted, `contextPercent` is an honest `null`:

```json
{
  "schemaVersion": 1,
  "museVersion": "1.4.14",
  "model": "<model>",
  "workspace": "<workspace>",
  "branch": "main",
  "dirty": true,
  "contextPercent": null,
  "sources": { "branch": "git", "model": "jsonl", "workspace": "jsonl" }
}
```

Colour appears only on a TTY (or with `--color`); `--plain`,
`NO_COLOR`, `TERM=dumb` and piped output are plain. Lines fit
`--width`, the config width, or the terminal width; paths truncate
from the left. All externally sourced strings (branch names,
tool/agent labels, targets) are sanitized against terminal escape
injection (`docs/security.md`).

## Install and walkthrough

Requires Go (see `go.mod`). From a clone of this repo
(`github.com/CWBudde/muse-hud`), run from the repo root:

```sh
tmpbin=$(mktemp -d)
GOBIN="$tmpbin" go install ./cmd/muse-hud
"$tmpbin/muse-hud" version
```

prints `muse-hud 0.1.0`. Then, still from the repo root:

```sh
"$tmpbin/muse-hud" doctor  # what was found, why a segment is hidden
"$tmpbin/muse-hud" print   # the HUD lines once (always exits 0)
"$tmpbin/muse-hud" watch   # live side-pane view, q quits
```

With a Muse session present, `doctor` exits 0, `print` shows the
model/branch lines, and `watch` follows the newest session.
Without any session, `doctor` prints the same report with
`session: NONE` and exits 1, `print` prints nothing (exit 0), and
`watch` shows a waiting frame. For a deterministic bounded smoke
run that works with no Muse session at all:

```sh
"$tmpbin/muse-hud" print --stub
"$tmpbin/muse-hud" doctor --file testdata/fixtures/synthetic-session.jsonl
"$tmpbin/muse-hud" watch --file testdata/fixtures/synthetic-session.jsonl --frames 2 --plain
```

To keep the binary around, `go install ./cmd/muse-hud` without the
`GOBIN` override installs into your Go bin directory. Prefer not to
install? `make build` produces `./build/muse-hud`; every command
below works with that path unchanged.

## Usage

```text
muse-hud print [flags]   print the HUD lines once (always exits 0)
muse-hud watch [flags]   follow the newest session live in this terminal
muse-hud doctor [flags]  explain sources and hidden segments
muse-hud version         print the muse-hud version
```

`print` collects a snapshot from the newest Muse session log for
the current workspace (discovery under
`~/.local/share/muse/sessions`), refines branch/dirty with fixed
`git` probes, and renders two default lines plus optional
tools/agents/todos lines — or the snapshot with `--json`. Shared
session selection: `--session <id>`, `--file <session.jsonl>`,
`--last` (newest overall), `--workspace <dir>`, and `--muse-home`
for tests. `muse-hud help` lists every flag.

`watch` tails the active `session.jsonl` incrementally —
offset-tracked, newline-terminated lines only, surviving
truncation/rotation — re-discovers the newest session every 2 s and
switches to it when a newer one appears, and redraws the same lines
as `print`. The header shows the session id and age. Attention
cues appear for the states the logs actually prove: pending
approvals, failed tool batches, tools running past `--long-running`
(default 30 s), and background agents the watch itself saw finish.
Keys: `q` quit, `p` pause/resume (freezes the view), `t`/`a`/`d`
toggle the tools/agents/todos lines. Polling only, no file-notify
dependency; idle CPU is ~zero (one `stat` per tick, throttled
discovery/git: 0.01 s measured over a 10 s idle run). `watch` never
spawns `muse` subprocesses; the only external surface is the same
fixed `git` probes as `print`, throttled to every 5 s.

Recipes for shell prompts, tmux, and `--json` scripting live in
`docs/integrations.md`, each tested exactly as written.

## Troubleshooting: run `doctor`

`doctor` is the answer to "the HUD is blank" or "a segment is
missing": it reports the Muse version and whether it is verified,
the config state, the sessions dir and chosen session, which
adapters worked, unknown `payload_type`s with drift hints, a
shown/hidden verdict with a reason per segment, and a plain preview
of what `print` would emit. Excerpt (paths shortened):

```text
$ muse-hud doctor
muse-hud doctor (muse-hud 0.1.0)
muse: 1.4.14 (verified, from session log build)
support: verified Muse versions: 1.4.14 (docs/compatibility.md); 40 known payload_type values
config: <user config dir>/muse-hud/config.json (missing — using defaults)
sessions dir: <muse home>/sessions (exists)
session: <session-id>
adapters:
  session.jsonl: ok — bounded head+tail window (… of … bytes), … envelopes, 0 skipped lines
  git probes: ok (branch/dirty from git)
segments:
  model: shown (<model>, source jsonl)
  branch: shown (main, source git)
  context: hidden — no Muse surface provides a live context percentage (…)
  usage: hidden — allowance is opt-in; pass --with-usage to probe 'muse usage --json'
  …
print preview (plain):
  [<model> · high] │ <workspace> git:(main*)
```

Common verdicts and what to do:

| `doctor` says | Meaning / fix |
| --- | --- |
| `session: NONE` (exit 1) | No Muse session found for this workspace. Start `muse` here, or pass `--last`, `--session`, or `--file`. |
| `disabled: MUSE_HUD_DISABLE=1` | The silence switch is set; unset it. `print` emits nothing while set. |
| `config: … INVALID — using defaults` | Your config failed validation; the error follows. `print` silently uses defaults until it is fixed. |
| `muse: X.Y.Z (UNVERIFIED …)` + unknown `payload_type` hints | Your Muse is newer than the support table. Known segments still show; unknown types are ignored, never guessed. See `docs/compatibility.md`. |
| `context: hidden — no Muse surface…` | Expected on 1.4.14: no live context % exists. `showTokens` shows the raw estimate instead. |
| `todos: hidden — todo list content…` | Expected: todo content is in no local surface (spike b). |
| `usage: hidden — allowance is opt-in` | Pass `--with-usage` to probe `muse usage --json`. |
| branch from `jsonl` instead of `git` | The git probes found no live branch (not a git workspace, or a probe warning below); the log-observed branch is used. |
| `N skipped lines` | Corrupt/partial log lines were ignored; a few on a live log are normal. |

## Config reference

`~/.config/muse-hud/config.json` (via Go's `os.UserConfigDir()`),
or `--config <path>`. A missing file means defaults; an invalid
file falls back to defaults in `print`/`watch` and is explained by
`doctor` — errors never appear on the HUD lines. Unknown keys are
rejected. `MUSE_HUD_DISABLE=1` makes `print` and `watch` emit
nothing and exit 0; `doctor` still reports.

```json
{
  "plain": false,
  "width": 0,
  "showModel": true,
  "showWorkspace": true,
  "showBranch": true,
  "showContext": true,
  "showUsage": true,
  "showAge": true,
  "showApprovals": true,
  "showTokens": false,
  "showTools": true,
  "showAgents": true,
  "showTodos": true,
  "lineOrder": ["main", "status", "tools", "agents", "todos"],
  "contextWarnPercent": 70,
  "contextCriticalPercent": 90
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `plain` | `false` | Disable ANSI colour (same effect as `--plain`; `NO_COLOR` and non-TTY output are always plain, `--color` never overrides `plain`). |
| `width` | `0` | Column budget per line; `0` fits the terminal. Must be 0–1000. `--width` wins. |
| `showModel` | `true` | `[model · effort]` segment (effort rides with it). |
| `showWorkspace` | `true` | Workspace name segment. |
| `showBranch` | `true` | `git:(branch*)` segment (`*` = dirty). |
| `showContext` | `true` | Context percentage — only ever shown if a real percentage is sourced (never on 1.4.14). |
| `showTokens` | `false` | Estimated prompt-token count, shown as a count. Off by default: the estimate has no verified context-window denominator and is never converted to a percent. |
| `showUsage` | `true` | Subscription allowance segment (data only with `--with-usage`). |
| `showAge` | `true` | Session age segment (`⏱ …`). |
| `showApprovals` | `true` | Pending-approvals warning segment. |
| `showTools` | `true` | The tools line. |
| `showAgents` | `true` | The agents line. |
| `showTodos` | `true` | The todos line (stays hidden on real 1.4.14 sessions — no source). |
| `lineOrder` | all five, in order | Reorders lines (`main`, `status`, `tools`, `agents`, `todos`); unlisted lines follow in default order. Duplicates and unknown names are rejected. |
| `contextWarnPercent` | `70` | Yellow threshold when a real percentage is sourced; 1–100. |
| `contextCriticalPercent` | `90` | Red threshold; 1–100, must be ≥ `contextWarnPercent`. |

A toggle can only hide a segment, never fabricate one: unsourced
data stays hidden regardless of config.

## Known limits

On Muse 1.4.14, each with a written finding in `docs/spikes/`:
live context % does not exist in any local surface; todo list
content is not in the logs, so the todos line stays hidden on real
sessions; tool targets are not recorded. Subscription allowance is
opt-in via `print --with-usage`. Unknown Muse versions are still
read but tagged unverified, and unknown `payload_type`s are ignored
and reported by `doctor` — see `docs/compatibility.md`. Missing
data hides a segment; the HUD never fabricates numbers.

Large logs are read through a bounded head+tail window (256 KiB
head + 2 MiB tail by default); `--full-scan` parses the whole log.
Verified on an ~11 MiB synthetic fixture log: `print` p95 35 ms
bounded. On a large live repository, `git status` for the dirty
check can dominate the total.

## Design

Three layers, mirroring claude-hud's collector/logic split (Go
standard library only, single static binary):

- `internal/collect` — reads Muse's files/commands, probes `git`
  with fixed arguments only.
- `internal/snapshot` — one normalized, version-tagged view of
  "what is happening now"; every field records its source.
- `internal/render` — pure functions from snapshot + config to
  lines (colour, width fitting, sanitizing).
- `internal/watch` — the watch model: tailing, session switching,
  pause/toggles, attention cues; terminal-free, tested without a
  TTY. `cmd/muse-hud` adds only terminal I/O.

The HUD is read-only and offline: no network, no credential reads,
no writes — the audited surface is in `docs/security.md`.

## Build and test (developers)

```sh
make build   # go build ./... and build/muse-hud
make test    # go test ./...
make lint    # go vet ./... + gofmt check
make run     # go run ./cmd/muse-hud print
```

Fuzz targets and their commands are listed in `docs/security.md`.
`PLAN.md` is the roadmap and records which gates were run.

## License

MIT — see `LICENSE`. Clean-room implementation: behaviour and
layout ideas from claude-hud, no copied code.
