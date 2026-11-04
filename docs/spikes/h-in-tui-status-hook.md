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
  or anywhere else.
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
