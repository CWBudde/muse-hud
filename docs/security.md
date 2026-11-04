# Security model

`muse-hud` is a read-only, offline companion binary. This page is
the Phase 5 security pass: what the binary can touch, what it
deliberately cannot, and how to re-verify each claim. Every claim
below names the code that enforces it (paths relative to the repo
root); re-check a claim by reading that code, not by trusting this
page.

## No network

No non-test Go file imports a `net` package, and there are no HTTP
clients, sockets, or update checks anywhere in `cmd/` or
`internal/`. Go standard library only (`go.mod` has no `require`
entries). Re-check: `grep -rn '"net' --include="*.go" cmd internal`
returns nothing.

## Reads: an allowlist by construction

The binary reads exactly these things:

| What | Where | Why |
| --- | --- | --- |
| Muse session logs under `<muse home>/sessions/YYYY/MM/DD/<id>/session.jsonl` (or an explicit `--file`) | `internal/collect/discover.go`, `collect.go`, `tail.go` | the HUD's data source, opened with `os.Open` |
| Its own config file | `internal/config/config.go` (`Load` → `Parse`) | render settings; missing/bad → defaults |
| Stdout of the fixed subprocesses below | `internal/collect/runner.go`, `git.go` | version, allowance, trace, branch/dirty |
| `MUSE_HUD_DISABLE`, `NO_COLOR`, `TERM`, `COLUMNS` | `internal/config/config.go`, `cmd/muse-hud/main.go` | silence/colour/width switches |

It never reads credential or account files: no SSH keys, no Muse
settings/provider configuration, no cookies, no shell history. No
code path opens a path outside the rows above; session discovery
walks the sessions directory layout only. Transcript *text* is not
surfaced either: the deriver (`internal/collect/derive.go`) extracts
session facts (model, branch, tool names, approval counts) and
deliberately ignores message/objective/prompt payloads, and the
export adapter always passes `--redacted`.

## Writes: none on the shipping paths

`print`, `watch`, `doctor`, and `version` write only to stdout (the
terminal). No cache files, no state files, no log files. The single
exception inside the library is the unused-by-default `Export`
adapter (`internal/collect/export.go`): it creates a temporary
directory, lets `muse export --redacted` write one file into it,
reads its header subset, and removes the directory. `Collect` never
calls it (no HUD fact needs export), so no shipped command exercises
it. `os.WriteFile`/`os.OpenFile` appear only in `*_test.go` files.

## Subprocesses: fixed arguments, no shell, bounded

External commands go through two seams only, both
`exec.CommandContext` with an argument vector — never a shell, never
string interpolation of user input into a command line:

- `git -C <workspace> rev-parse --abbrev-ref HEAD`,
  `git -C <workspace> rev-parse --short HEAD`, and
  `git -C <workspace> status --porcelain`
  (`internal/collect/git.go`), each under a 500 ms default timeout
  with `GIT_OPTIONAL_LOCKS=0` so git takes no optional locks. The
  workspace path comes from the session log or `--workspace`; it is
  passed as one argv element. Retained stdout is capped at 64 KiB.
- Via `Runner` (`internal/collect/runner.go`), always against the
  `muse` binary: `muse --version`, `muse usage --json` (opt-in
  `--with-usage`), `muse trace inspect --session-log <log>
  --format json` (opt-in `--with-trace`), and the export adapter's
  `muse export --session <log> --out <tmpfile> --redacted` (never
  called by `Collect`). Each runs under its own timeout (2–15 s).

There is **no `extra-cmd`-style feature**: no config key, flag, or
environment variable makes muse-hud run a user-chosen command, and
none is planned. If one is ever added it must be behind an explicit
opt-in environment flag and documented as arbitrary code execution;
that bar is recorded here so the absence stays a decision, not an
accident.

## Terminal-escape injection

Everything on a HUD line ultimately comes from files someone else
wrote (branch names, tool names, workspace paths, payload-type
strings in `doctor`). All externally sourced strings pass through
`render.Sanitize` (`internal/render/sanitize.go`) before display:
ANSI/OSC/DCS escape sequences are removed whole, remaining C0/C1
controls and Unicode format characters (bidi overrides, zero-width
characters, BOM) are dropped, and whitespace is collapsed so a value
cannot open a new line. With colour on, the only escape sequences
in the output are muse-hud's own styles.

Pinned by `TestSanitizeStripsEscapes` and
`TestSanitizedBranchCannotInjectIntoLine`
(`internal/render/render_test.go`) and by `FuzzSanitize`
(`internal/render/fuzz_test.go`), which asserts no ESC/control/
format character survives and sanitizing is idempotent.

## Parsers under fuzzing

The untrusted inputs are the session JSONL, the CLI-projection JSON
(trace/usage/export/`--version`), and the config file. Fuzz targets
(seed corpora double as ordinary tests under `go test ./...`):

```sh
go test ./internal/collect -run x -fuzz FuzzDerive -fuzztime=10s
go test ./internal/collect -run x -fuzz FuzzAddLine -fuzztime=10s
go test ./internal/collect -run x -fuzz FuzzParseAdapters -fuzztime=10s
go test ./internal/config -run x -fuzz FuzzParse -fuzztime=10s
go test ./internal/render -run x -fuzz FuzzSanitize -fuzztime=10s
```

Beyond "never panic", the targets assert the honesty invariants: a
raw log can never produce a context percentage, a populated field
always carries a source, counts and ages never go negative, and a
rejected config resets to byte-identical defaults. Phase 5 ran each
target for 10 s with no crashers (about 180k–720k execs each; see
`CHANGELOG.md`). Malformed input degrades by design: corrupt or
partial JSONL lines are counted and skipped, unknown
`payload_type`s are ignored and reported by `doctor`
(`docs/compatibility.md`), and a bad config falls back to defaults
with the error shown by `doctor`, never on the HUD lines.

## Failure behaviour

`print` always exits 0 and prints fewer (or no) segments rather than
wrong ones; `MUSE_HUD_DISABLE=1` silences it entirely. A slow or
locked git repository cannot hang a prompt: the probes time out and
the segment falls back to the log-observed branch. Large logs are
parsed through a bounded head+tail window unless `--full-scan` is
given, so memory and latency stay bounded regardless of log size.
