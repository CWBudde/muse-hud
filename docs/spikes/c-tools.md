# Spike c — Tool name, target, start/end pairing

Phase 1 spike, 2026-11-04, against Muse Code 1.4.14. Method: read-only
key/enum inventory of local session logs (including one still-running
session where a tool's terminal record had not landed yet), cross-checked
with `muse trace inspect` task summaries on the same logs.

## Findings

- Tool execution is durably recorded as `tool_batch.effect.started` /
  `tool_batch.effect.terminal` envelope pairs. The started record carries
  `tool_name`, `call_id`, `effect_id`, `model_call_index` and the envelope
  `recorded_at` (microseconds). The terminal record repeats
  `call_id`/`effect_id` and adds `outcome.kind` (observed: `completed`,
  `failed`); it does **not** repeat `tool_name`. Pairing key: `effect_id`,
  with `call_id` as fallback. A started record without a terminal is a
  running tool — confirmed on a live session mid-tool-call.
- Counts agree with `muse trace inspect`: its `payload_counts` match the
  raw envelope counts, and its `task_summaries` with `category: tool`
  (`task_kind: tool.<name>`) show the same tools with states/terminals.
- **No target:** neither the effect records nor the `runtime.session`
  task `proposed` event (`{kind, task_id, task_kind}` only) carries the
  file/command a tool acted on. Task `status` events carry a free-text
  `message` and `details` — descriptive prose, not a stable target field —
  and surfacing it would pipe transcript-adjacent text into a status bar.
- Elapsed time for a running tool follows from `recorded_at` of its
  started record versus the collection time (or the last record for an
  ended session).

## Verdicts

- `Snapshot.Tools[].Name`, `Running`, `Count`, `Failed`, `Elapsed`:
  **field sourced from** the raw JSONL
  (`tool_batch.effect.started/terminal`, paired by `effect_id`);
  `muse trace inspect` task summaries are the fallback fill only when
  the log yields no tools (`print --with-trace`).
- `Snapshot.Tools[].Target` (file/command per tool): **field cut** on
  1.4.14 — no stable source. The field stays empty from real logs; only
  synthetic fixtures populate it, and the renderer shows only the tool
  name then.
