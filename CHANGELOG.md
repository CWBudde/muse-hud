# Changelog

All notable changes to muse-hud. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [Semantic Versioning](https://semver.org/). No git tags or
published releases exist yet — the GitHub owner is still an open
question (see `PLAN.md`), so 0.1.0 is the version the binary
reports, not a tag.

## [0.1.0] — 2026-11-04 (untagged)

First hardened build: Phases 0–5 of `PLAN.md`.

### Added

- `print`: one-shot HUD lines (model · effort, workspace, git
  branch/dirty; usage/approvals/age; optional tools/agents/todos
  lines), `--json` snapshot output (schema v1), `--stub`,
  `--plain`/`--color`/`--width`, config file with validation, and
  `MUSE_HUD_DISABLE=1` (silent, exit 0). Always exits 0.
- `watch`: live side-pane view tailing the newest `session.jsonl`
  (offset-tracked, newline-terminated lines only, truncation/
  rotation/session-switch safe), attention cues for pending
  approvals, failed batches, and long-running tools, keys `q`/`p`/
  `t`/`a`/`d`, bounded `--frames` mode for smoke tests.
- `doctor`: Muse version + support verdict, sessions dir and
  chosen session, per-adapter status, and a per-segment
  shown/hidden explanation with a plain `print` preview.
- Collection from Muse 1.4.14 session logs with a bounded
  head+tail window for large logs (`--full-scan` for whole-log
  state), fixed-argument `git` probes, and opt-in
  `muse usage --json` / `muse trace inspect` adapters.
- Integration recipes (`docs/integrations.md`): shell prompt, tmux
  status bar, side-pane watch, `--json` scripting — each run
  exactly as written.
- Security and compatibility hardening (Phase 5):
  `docs/security.md` (read-only allowlist, fixed subprocess table,
  no network, no credential reads, no extra-cmd feature),
  `docs/compatibility.md`, diff-style unknown-`payload_type` hints in
  `doctor` (closest known type per unknown), a synthetic
  format-drift fixture (`synthetic-session-drift.jsonl`), and fuzz
  targets for the JSONL/config/adapter parsers and the
  terminal-escape sanitizer. Each fuzz target ran 10 s with no
  crashers (`FuzzDerive` ≈182k execs, `FuzzAddLine` ≈268k,
  `FuzzParseAdapters` ≈558k, `FuzzParse` ≈722k, `FuzzSanitize`
  ≈373k).

### Known limitations (verified on Muse 1.4.14, see `docs/spikes/`)

- No live context percentage exists in any local surface; the
  estimated prompt-token count is shown only as a count behind
  `showTokens`, never as a percent.
- Todo list content and per-tool targets are not recorded in the
  logs; those segments stay hidden on real sessions.
- Not an in-TUI statusline: Muse exposes no statusLine hook
  (re-checked in Phase 4). MSP's subscribe surface could carry
  context/todos but does not attach to TUI-driven sessions yet.
- Linux only. Module path: `github.com/CWBudde/muse-hud`.
