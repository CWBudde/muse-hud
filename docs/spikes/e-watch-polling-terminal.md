# Spike e — Watch mode: polling vs. file-notify, and stdlib terminal handling

Phase 3 spike, 2026-11-04, Muse 1.4.14 machine. Question from
PLAN.md Phase 3: does `watch` need a file-notify dependency or a TUI
library, or do polling and the standard library suffice?

## Polling

`watch` polls on one code path (`internal/collect.Tailer.Poll` behind a
ticker): one `stat` per interval (default 500 ms, `--interval`
100 ms..10 s), re-discovery of the newest session throttled to 2 s, git
probes throttled to 5 s. New bytes are read from the tracked offset and
only newline-terminated lines are fed to the deriver (a record Muse is
mid-way through writing is buffered, never half-parsed). Truncation and
rotation (path now naming a different file) re-seed the derivation.

Measured here (synthetic fixture log, built binary, scripted probe):

- Append → visible in a piped `watch` frame: **249 ms at a 250 ms
  interval** — i.e. at most one missed tick plus a sub-millisecond
  poll+render, as designed. At the 500 ms default the bound is ~500 ms;
  the integration test in `internal/watch` asserts the model reflects
  appended tool-start/tool-end/approval events within a 1 s deadline.
- Idle CPU, 10 s `watch` on a quiet log (500 ms interval):
  **user 0.00 s + sys 0.01 s, ~4.3 MiB RSS**. A stat per tick and a
  throttled directory walk are lost in the noise; nothing a notify
  watcher would measurably improve (its event would only shave the
  sub-tick latency, already < ~0.5 s at default).
- A file-notify dependency (e.g. fsnotify) would also have to handle
  the same offset/partial-line/truncation logic after every event, and
  add watch-descriptor lifecycle bugs (rotation re-arms) of its own.

**Verdict: polling only. No file-notify dependency.** Revisit only if
sub-250 ms visibility ever becomes a requirement.

## Terminal handling

Standard library only proved sufficient on Linux (v1 platform):

- Raw single-key input via TCGETS/TCSETS ioctls on `syscall.Termios`
  (no echo/canonical/ISIG; Ctrl-C arrives as byte 3 and is a clean
  quit), restored on every exit path; line-mode stdin bytes still work
  when stdin is not a TTY.
- Redraw: cursor hide/show + home-and-clear per frame on a TTY; piped
  output gets no clear escapes and emits frames only on change
  (`--frames N` emits exactly N separated frames for smoke runs).
- Terminal width re-probed per frame via TIOCGWINSZ (already used by
  `print`), so resizes need no SIGWINCH plumbing.
- A scripted PTY probe verified: clear escapes present, `p` shows
  PAUSED, `t` drops the tools line from the final frame, `q` exits 0
  with the cursor restored.

**Verdict: no TUI library.** The frame is a handful of text lines; a
full TUI framework would own rendering and input for no user-visible
win. macOS termios constants differ from Linux's — out of scope for v1
(PLAN.md assumption), to be handled by build-tagged files if macOS is
ever promised.

## Not proven here (manual, Phase 3 gate)

- The 10-minute idle observation and the human side-by-side judgement
  ("looks right next to Muse while a long `go test` and a subagent
  run") are manual checks on this machine. What is proven instead:
  10 s idle CPU ~0.01 s (extrapolated negligible), the synthetic
  integration test, the PTY probe above, and a bounded `--frames` run
  against the live parent session showing its model/branch/tools and
  the running subagent (this very watch implementation's agent).
