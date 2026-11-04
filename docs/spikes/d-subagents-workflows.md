# Spike d — Subagent / workflow lineage

Phase 1 spike, 2026-11-04, against Muse Code 1.4.14. Method: read-only
key/enum inventory of local parent and child session logs. Nested child
logs live at `<session>/subagent/<child>/session.jsonl` beside the
parent log.

## Findings

- The parent log records a full child lifecycle under
  `subagent.control.*`: `spawn_accepted` (carries `subagent_id` and
  `role`), `child_session_bound`, `start_attested`,
  `status_updated` (with `control_status`, observed `running`),
  `runtime_observed` (with `duration_ms` and token usage),
  `result_ready`, plus `owner_state_changed` (`owner_state`). Spawn and
  resume records also carry `objective`/`prompt` texts and result records
  carry `summary`/`text` — transcript content that muse-hud must not copy
  into a HUD; the `role` field is the safe display label.
- Workflow children additionally appear as `runtime.session` tasks with
  `task_kind` `workflow.agent.native` / `workflow.run.native`. In parent
  logs these describe the same children as the `subagent.control.*`
  records (matching counts), so naively adding both double-counts.
  Inside a child's own log, only the workflow task form is present.
- `muse export` exposes lineage too (`sessions[]` with
  `root_session_id`, accepted spawns), but it settles after the fact and
  adds no live state over the JSONL records; no HUD fact needs it.

## Verdicts

- `Snapshot.Agents` (kind, label, running, elapsed): **field sourced
  from** the raw JSONL — `subagent.control.*` when present
  (label = `role`, running from `control_status`/`owner_state`, elapsed
  from `runtime_observed.duration_ms` or start-vs-now), falling back to
  `workflow.*` task records when no control records exist. Objective,
  prompt and result texts are never surfaced.
- Export-based lineage: **field cut** (adapter retained, unused) — the
  JSONL proves the same facts live.
