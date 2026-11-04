# Spike b — Todos, goals, background work visibility

Phase 1 spike, 2026-11-04, against Muse Code 1.4.14. Method: read-only
key/enum inventory of the largest and newest local session logs, plus
`muse trace inspect` projections of the same logs. No transcript
content was copied into this repo.

## Findings

- **Todos:** `write_todos` occurs only as a task kind (`tool.write_todos`)
  and as a tool name in `tool_batch.effect.*` records. The task `proposed`
  event carries exactly `{kind, task_id, task_kind}` — no arguments. A raw
  scan of the largest logs finds no `"todos"` key at all, and trace task
  summaries carry no todo content or progress. The todo list itself (texts
  and statuses) exists in none of the durable local surfaces scanned.
  `muse export` does include tool calls in its event stream (per its own
  help), so arguments might be recoverable there — but that means parsing
  the heaviest, transcript-bearing source for a HUD line, and the
  `write_todos` payload shape inside export events is unverified. That is
  not a sound basis for a live segment.
- **Goals:** run records carry `goal_usage_attribution` events and there
  is a `runtime.session_handoff.goal_safety` record, but the observed
  goal state is `absent_store` with null id/revision/status. No goal text
  or progress counter is visible live.
- **Background work:** `session.started` carries a short `background_tasks`
  string flag, and long work shows up as ordinary tool tasks
  (e.g. `tool.monitor`, `tool.bash`). There is no durable record listing
  background jobs with states beyond the tool pairing in spike c.
- **Approvals (bonus visibility):** `runtime.session` kind `approval`
  events are enumerable: `requested` (with `pending_action_id`),
  `automated_review_started/completed`, and `decision_applied`. Requested
  minus decided is a real pending count. Approval records contain command
  text, which muse-hud never surfaces — only the count.

## Verdicts

- `Snapshot.Todos` (current + n/m): **field cut** on 1.4.14. Stays `nil`;
  a future export-arguments spike could reopen it, but nothing is
  promised or displayed meanwhile. `write_todos` invocations remain
  visible as ordinary finished tools.
- Goal progress: **field cut** — no live goal state is sourced.
- A dedicated background-work segment: **field cut** — the observable
  subset is already covered by running tools (spike c) and agents
  (spike d).
- `Snapshot.ApprovalsPending`: **field sourced from** the raw JSONL
  (`runtime.session` kind `approval`, `requested` minus
  `decision_applied`, paired by `pending_action_id`).
