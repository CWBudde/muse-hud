# Compatibility

Muse's session logs (`session.jsonl`) are not a documented stable
API, so muse-hud treats them as version-pinned input: it knows which
Muse versions it has been verified against, still attempts
collection on any other version, and degrades to fewer segments —
never guessed numbers — when shapes drift. `doctor` always states
which situation you are in.

## Support table

| Muse version | Status | Log shape | Fixture (synthetic) |
| --- | --- | --- | --- |
| 1.4.14 | **verified** | envelope `record_type/sequence/recorded_at` (µs epoch) `/payload_type/payload{kind,record\|event}`; 40 known `payload_type` values | `testdata/fixtures/synthetic-session.jsonl` (generator: `testdata/genfixture`, `make fixture`) |
| anything else | unverified — best effort | same parser; unknown `payload_type`s counted and ignored, known facts still shown | `testdata/fixtures/synthetic-session-drift.jsonl` (build `9.9.9`, a renamed tool type, a new payload family) |

The table in code is `collect.VerifiedVersions`
(`internal/collect/version.go`); the recognized payload types are
`collect.KnownPayloadTypes()` (`internal/collect/compat.go` over the
table in `derive.go`). The version judged is the one that *wrote
the log* (the session-metadata build record) when present, else
`muse --version`. An unverified version tags the snapshot
`unverified` (`"unverified": true` in `print --json`, and a note in
`doctor`); rendering is unchanged except that fields only appear
when really sourced, as always.

## What each version can and cannot show

On verified 1.4.14, some claude-hud staples do not exist in any
local surface; the segments stay hidden rather than approximated.
Each cut has a written finding in `docs/spikes/`:

- Live context % — no surface carries it for a running session
  (spike a). Only an estimated prompt-token count exists; opt-in
  `showTokens` shows it *as a count*, never converted to a percent.
- Todo list content — `write_todos` appears only as a tool name
  (spike b), so the todos line stays hidden on real sessions.
- Tool targets (file/command per tool) — not recorded (spike c);
  the tools line shows names, counts, and elapsed times only.
- Subscription allowance is real but opt-in: `print --with-usage`.

## Format drift: how it presents

Unknown `payload_type` values are counted during derivation
(`Stats.UnknownPayloadTypes`) and turned into diff-style hints by
`collect.DriftHints`: each unknown type is matched against the
known type sharing the most leading dot-segments, so a rename or a
new variant in a known family is recognizable. `doctor` prints
them under the adapters section, for example (from the synthetic
drift fixture — paths on your machine differ):

```text
muse: 9.9.9 (UNVERIFIED, from session log build — log shapes may have drifted; segments degrade, numbers are never guessed)
support: verified Muse versions: 1.4.14 (docs/compatibility.md); 40 known payload_type values
adapters:
  session.jsonl: ok — full parse, 6 envelopes, 0 skipped lines
  unknown payload_type values (ignored, format drift?): future.hologram.observed×1, runtime.future.thing×1, tool_batch.effect.begun×1
    ? future.hologram.observed (×1): new payload family this muse-hud build does not know; ignored, the segments it might feed stay hidden
    ? runtime.future.thing (×1): shares only the "runtime" family prefix with known types (e.g. "runtime.command.accepted"); ignored
    ? tool_batch.effect.begun (×1): resembles known "tool_batch.effect.started" — a rename or new variant in that family; ignored until support is verified
    unknown types are never guessed at: segments they might feed stay hidden; see docs/compatibility.md
```

In that same report, `model` and `branch` are still shown (their
record shapes did not drift) while `tools` is hidden — its feeding
type was the renamed one. `print` shows exactly those reduced
lines and still exits 0. This behaviour is pinned end to end by
`TestDeriveDriftFixtureDegradesHonestly`
(`internal/collect/compat_test.go`) and `TestDoctorDriftFixtureHints`
(`cmd/muse-hud/phase5_test.go`).

Corrupt or partial lines (a log being written can end mid-line)
are likewise non-fatal: they are skipped and counted
(`session.jsonl: ok — …, N skipped lines`), and the watch tailer
only ever consumes newline-terminated records.

## Adding a newly verified version

1. Hand-check `print --json` against `muse trace inspect` on a
   real log from the new version.
2. Extend the synthetic fixture generator if shapes changed —
   fixtures must stay synthetic; never copy real log content.
3. Add the version to `collect.VerifiedVersions`, any new payload
   types to the known table, and a row to the table above, with
   `go test ./...` green.
