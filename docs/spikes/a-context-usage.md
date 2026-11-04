# Spike a — Live context / usage source

Phase 1 spike, 2026-11-04, against Muse Code 1.4.14 on this machine.
Method: read-only inventory of local session logs (keys, payload types and
enum values only — no transcript content read into this repo) plus the
documented CLI projections. Running and finished sessions were both
inspected.

## Findings

- `muse trace inspect --format json` reports `summary.usage` as `null` on
  partial logs. When a projection fully loads (`load_status: loaded`),
  usage carries cumulative session token totals (`input_tokens`,
  `output_tokens`, `cached_tokens`, `reasoning_tokens`) — a cumulative
  accounting, not the current prompt size. On a still-running log it can
  exit 1 with `load_status: partial` ("tool-call batch … was not
  committed") while still emitting the full JSON report on stdout — the
  trace adapter therefore parses stdout even on runner error. There is no
  context-window size and no context percentage in the report.
- `muse export` (schema v1) emits session headers and an event stream
  mirroring the log. Per-call token counts exist inside runtime records,
  but no context-window denominator appears anywhere.
- The raw `session.jsonl` carries, in `runtime.session` run records,
  `model_completed.estimated_prompt_tokens` (an estimate for the last
  completed model call) and per-call usage token counts
  (`input_tokens`, `output_tokens`, …). It also carries context-compaction
  budget thresholds (soft/hard/target) in compaction-candidate records.
  Those thresholds are compaction policy, not a verified context-window
  size; dividing the estimate by them would fabricate a percentage.
- `muse usage --json` works offline-from-muse-hud's perspective and
  reports the **subscription allowance**, not session context:
  `allowance.window.usedPercent`, `allowance.weekly.usedPercent`, reset
  times, tier. It is a real, sourced account-usage number.

## Verdicts

- `Snapshot.ContextPercent` (live context %): **field cut** — no surface
  on 1.4.14 proves numerator and denominator for a live session. The
  field stays `nil`, `--json` emits `null`, the segment stays hidden.
- `Snapshot.EstimatedPromptTokens`: **field sourced from** the raw JSONL
  (`runtime.session` kind `run`, event `model_completed`,
  `estimated_prompt_tokens`, last one wins). Exposed in `--json` as an
  estimate with source `jsonl`; never converted to a percentage.
- `Snapshot.Usage` (allowance): **field sourced from**
  `muse usage --json`, opt-in via `print --with-usage` (it is an extra
  subprocess and an account metric, not session state).
