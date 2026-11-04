# AGENTS.md

Guidance for AI coding agents in this repository. `CLAUDE.md` imports it.

## What this is

`muse-hud` is a read-only heads-up display companion binary for Muse.
`PLAN.md` is the roadmap and source of truth for what is done.

## Rules

- **Go only, standard library only.** Any external dependency needs a spike
  note in `docs/spikes/` before it is adopted.
- **Read-only and offline by construction:** no network, no writes outside
  muse-hud's own cache/config, no reading credential files, only
  fixed-argument `git` probes (no shell).
- **Never commit real Muse session data.** Fixtures are synthetic or
  mechanically redacted only. Never write real session transcripts, real
  session ids, or paths under `$HOME` into the repo. Reading local Muse logs
  read-only to learn shapes is fine; copying their content is not.
- **Missing data hides a segment; never fabricate numbers.** Every snapshot
  field records its source; the renderer hides unsourced fields.
- **Evidence before ticking:** tick a `PLAN.md` box only for work actually
  run and verified, with the gate commands green.
- Do not commit, push, or tag unless the user asks. The repo lives at
  `github.com/CWBudde/muse-hud` (public); the Go module path matches.

## Commands

```sh
make build   # go build ./... and build/muse-hud
make test    # go test ./...
make lint    # go vet ./... plus gofmt check
make run     # go run ./cmd/muse-hud print
```

## Layout

```text
cmd/muse-hud/       CLI entry: print | watch | doctor | version
internal/collect/   session discovery, Muse adapters, JSONL scanning, git probes
internal/snapshot/  normalized Snapshot types and derivation
internal/render/    lines, colours, width/truncation, --json output
internal/config/    config load/validate (os.UserConfigDir()/muse-hud/config.json)
testdata/           synthetic fixture generator and fixtures
```
