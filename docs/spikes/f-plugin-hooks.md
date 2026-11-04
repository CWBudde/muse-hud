# Spike f — Muse plugin hooks vs. log tailing

Phase 4 spike, 2026-11-04, Muse 1.4.14. Question from
PLAN.md Phase 4: do Muse plugin hooks provide session/tool events
fresher than `session.jsonl` tailing, and is a plugin worth shipping?

## What was done

- Scaffolded a minimal native bundle at
  `docs/spikes/plugin-probe-bundle/` (clearly a spike: version
  `0.0.0`, "do not install" in its name/description): manifest
  `.muse-plugin/plugin.json` declaring two hooks, `pre-tool`
  (`PreToolUse`) and `post-tool` (`PostToolUse`), each a `sh` command
  running a no-op sink script that records nothing.
- Validated it **without installing**:
  `muse plugins validate docs/spikes/plugin-probe-bundle --json`
  → exit 0, `valid: true`, `diagnostics: []`, compatibility summary
  `full`. `muse plugins list --json` still shows `{"plugins": []}` —
  nothing was installed, enabled, or approved, and the hook scripts
  were never executed (`muse plugins hook test` requires an
  installed plugin, so it was not run).
- Probed which hook events the validator accepts, by validating
  one-hook variants in a throwaway copy. Accepted (exit 0,
  `valid: true`): `PreToolUse`, `PostToolUse`, `PostToolUseFailure`,
  `PermissionRequest`, `SessionStart`, `SessionEnd`,
  `UserPromptSubmit`, `Stop`, `PreCompact`, `Notification`,
  `SubagentStart`, `SubagentStop`. Rejected with
  `unsupported-hook-event`: `TodoChanged`, `TaskStarted`,
  `TaskCompleted`, `TurnStart`, `TurnEnd`, `ApprovalRequested`, and
  a nonsense name. This is a candidate probe, not an authoritative
  enumeration — unlisted events may exist.

## Assessment

- **Freshness win: marginal and unmeasured.** A synchronous
  `PreToolUse` hook fires before the tool runs, while `watch` learns
  of a tool start from the log within one 500 ms poll (measured
  249 ms end-to-end at a 250 ms interval, spike e). The theoretical
  gain is therefore sub-second on tool starts only. Measuring it
  for real would require installing and approving the plugin and
  running its hook inside the user's Muse — outside this spike's
  no-install boundary, so the gain stays a bound, not a number.
- **No rendering surface.** Hooks are command executions with a
  transient `statusMessage`; nothing in the validated capability
  families (`skills`, `commands`, `hooks`, `mcpServers`,
  `reminders`) lets a plugin draw persistent HUD lines in the TUI.
  A plugin cannot replace `print`/`watch`.
- **Missing data stays missing.** There is no validated todo-change
  event, and hook payloads are not a documented context-usage
  source, so a plugin does not resurrect the fields cut in spikes a
  and b (live context %, todo content). MSP does carry those
  (spike g).
- **Cost and posture.** A shipped hook runs code on every tool call
  inside the user's Muse (latency on the critical path, an
  install + trust-approval step, hook stdin that may carry session
  content). That contradicts muse-hud's read-only, no-code-execution
  stance for a sub-second gain the log tail already nearly matches.

**Verdict: do not ship a plugin.** No written win. The scaffold stays
in `docs/spikes/plugin-probe-bundle/` as the reusable starting point
if a future Muse adds a status/render hook event or a documented
fresh-data payload (todos, context) that log tailing cannot see.
Unexecuted install proposal, for the record only:
`muse plugins install docs/spikes/plugin-probe-bundle` — not run.
