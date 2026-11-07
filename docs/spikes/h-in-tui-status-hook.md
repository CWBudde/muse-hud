# Spike h — In-TUI status hook re-check

Phase 4 re-check, 2026-11-04, Muse 1.4.14
(`Muse Code 1.4.14 (1.4.14-R7309.1)` — the same build PLAN.md's
context section verified; no newer Muse is installed here).

Question: has a statusLine/status-command hook appeared that would
let muse-hud render inside the Muse TUI (a would-be Phase 4b)?

## Checks run

- `muse --help`: full command/option list read; `grep -i
  'status|hud|hook'` over it and over `muse plugins|schema|serve|
  trace|usage --help` finds no status-line or status-command option.
  (`muse config status` exists but reports enterprise-config plane
  state — verified: it prints generation/sources, unrelated to a TUI
  status surface. `muse exec --json` emits JSONL events for a
  headless run's own output, not a TUI hook.)
- Settings keys of `~/.config/muse/settings.json` (key names only,
  no values read into this repo): `schema_version`, `provider`,
  `model`, `model_reasoning_efforts.*`, `tui` (sole subkey:
  `foreign_context_notice_shown`), `contributor_mode_enabled`,
  `contributor_mode_disclosure_acknowledged`, `permissions.*`.
  No `statusLine`, `status`, or command-hook key exists under `tui`
  or anywhere else. (This inventories keys *set* in that file, not
  keys *supported* — see Correction below.)
- Plugin capability families (spike f) contain no status/render
  surface, and MSP (spike g) is a client protocol for external
  hosts, not a TUI extension point.

## Verdict

**No in-TUI status hook exists in Muse 1.4.14.** The central
design constraint from PLAN.md's context section stands unchanged,
so **no Phase 4b is added**. The integration surface remains
`print` (prompt/status-bar/scripting) and `watch` (side pane), per
`docs/integrations.md`. Re-run this check on Muse version bumps:
it is three greps and a key listing.

## Correction (2026-11-07, same Muse build 1.4.14-R7309.1)

Muse **does** have a built-in, configurable TUI HUD — per-segment
toggles, not a custom-content hook. The verdict above stands for a
statusLine-style hook, but "no status surface" readings of it (and
the README note it fed) were overstated. Evidence, all run locally:

- The real binary (`muse` on PATH is a launcher script exec'ing a
  versioned `muse-bin-*` ELF beside it) contains `struct TuiSettings`
  with first field `hud`, a `TuiHudSettings` struct, a
  `tui.hud.toggle` action id, and a settings catalog describing the
  segments: Model ("Current model name"), Effort, Working directory,
  Permissions ("Effective permission profile; YOLO always stays
  visible"), Context ("Routine context usage; warnings always stay
  visible"), Branch ("Last observed branch or commit"), Tokens
  ("Cumulative session tokens, including completed subagents"),
  estimated cost ("for metered billing, when prices are available"),
  Quota ("Subscription usage: current window and week").
- Live probes (`muse exec --provider echo --no-session-log -c
  'tui.hud.<key>=true' 'hi'`): accepted boolean keys are `model`,
  `effort`, `permissions`, `context`, `branch`, `tokens`, `cost`,
  `quota`; the object form `tui.hud={"model":false}` is also
  accepted. Controls: `tui.hud.nonsense` is refused as an unknown
  key, `tui.hud.context="on"` as a type mismatch, and `tui.hud.toggle`,
  `tui.hud.cwd`, `tui.hud.directory`, `tui.hud.working_directory` as
  unknown — the working-directory segment has no toggle key found.
- The binary's `statusLine`/`status_line` strings are a per-tool-call
  wire field (alongside `statusPhase`, `statusProgress`,
  `statusStep`, `statusAttempt`, `statusHealth`, `statusChildren`),
  not a settings key; the original hook conclusion is unaffected.
- Not probed: per-segment defaults when a key is absent, and whether
  the interactive `/settings` panel exposes the same toggles (catalog
  wording suggests settings rows; that is a user-side TUI control,
  unverified from here).
- Method lesson: the original check inventoried keys *set* in one
  user's `settings.json`. Absence there proves nothing about
  supported keys. Probe support directly — `-c` refuses unknown keys
  by name — before concluding a key does not exist.

Impact on muse-hud: unchanged architecture (no Phase 4b; nothing
custom can be rendered into Muse's bar). The built-in HUD covers
exactly the segments external sourcing could not (live context,
quota, tokens/cost — spike a), so the two are complementary:
built-in toggles for Muse's own segments, `print`/`watch` for
running tools, agents, and session age.
