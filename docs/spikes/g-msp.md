# Spike g — MSP (`muse serve`) as a HUD data source

Phase 4 spike, 2026-11-04, Muse 1.4.14. **Research only** —
no code built on this. Question: does `muse serve` (MSP, the Muse
Session Protocol) expose a subscribe/status surface a HUD could
consume without owning the session?

## Method

Exported both schema surfaces to a scratch directory (not the repo):

```sh
muse schema generate-json-schema --out <scratch>/stable
muse schema generate-json-schema --out <scratch>/exp --experimental
```

and inspected `methods`, `notifications`, and the referenced `$defs`
descriptions verbatim. Also read `muse serve --help`. No server was
started and no session was driven.

## Findings

**Yes — the stable surface is a real status/subscribe API.** Stable
methods include `initialize`, `session/list`, `session/read`,
`view/subscribe`, `view/unsubscribe`, `view/page`, `task/list`,
`approval/listPending`, and `usage/read`. Stable notifications
include `session/statusChanged`, `session/contextUsage`,
`session/tokenUsage`, `session/todoListChanged`, `session/started`,
`session/closed`, `session/branchChanged`, `session/modelChanged`,
`session/reasoningEffortChanged`, `session/goalChanged`,
`item/started`, `item/updated`, `item/completed`,
`approval/requested`/`approval/resolved`, and turn lifecycle events.

The read-only semantics a HUD needs are explicit in the schema:

- `view/subscribe`: "It never loads a session, takes no lease,
  writes nothing." Replays `(after, head]` then streams live view
  notifications; `view/unsubscribe` "does not unload the session".
- `session/read`: "no writer lease, no load, no subscription, no
  `SessionResumed` record" — but it is a point-in-time fold,
  "immediately stale if a foreign host is appending".
- `session/list`: "read-only, never touches leases" over stored
  sessions.
- `approval/listPending`: "a lease-free fold read".
- `session/statusChanged` is a command-plane broadcast, "delivered
  to every connection regardless of its view subscription set".

Two fields the logs denied us (spikes a/b) exist here as first-class
facts: `session/contextUsage` carries a provider-reported
`(windowTokens, usedTokens, pressure)` triple (the folded
`SnapshotState.contextUsage` block carries the same; "absent is
never fabricated"), and `session/todoListChanged` replaces the whole
todo list wholesale (`TodoListState` in the snapshot). The folded
`SnapshotState` also carries `effectiveModel`, `branch`, `goal`,
`approvalMode`, `activeTurn`, and every item at its latest revision —
in short, a server-computed version of muse-hud's own snapshot.

## The catch: whose host?

A subscription only streams sessions **loaded on the host you are
connected to**, and connection is the unsolved part:

- `muse serve` over stdio makes the connecting client the owner of
  a *new* host process ("The client owns this process's stdin and
  stdout"). Its `session/list`/`session/read` can fold stored logs,
  but a live session being driven by the user's interactive TUI is
  loaded in *that* process, not in a host muse-hud spawns.
  Subscribing there would at best mirror a stale fold; loading the
  session into our own host is exactly the ownership a read-only HUD
  must not take.
- `muse serve --listen` is a multi-client socket daemon, and the
  experimental surface separates *viewing* from *driving*
  (`session/requestDrive`/`session/releaseDrive`,
  `session/driveChanged`): a non-driving viewer connection is an
  intended shape. But on this machine no daemon socket was present
  in the runtime directory and the running `muse` processes are
  plain TUI invocations, so there is currently no daemon for a HUD
  to attach to — attaching would require the user to run Muse
  through the daemon (`--join`), which this spike did not test.
- The experimental surface additionally offers raw log streaming
  (`log/subscribe`, `log/record`, `log/physicalLine` — the `rawLog`
  capability), which would make MSP a transport for the same records
  `watch` already tails from disk, with the same loaded-on-this-host
  requirement ("must be loaded on this host").

## Verdict

**Promising, not buildable yet for the default setup.** If/when
interactive Muse sessions normally live in the per-user daemon, an
MSP viewer client is strictly better than log tailing: lease-free
subscribe, server-folded snapshot, real context % (used/window with
a pressure level), real todos, and push latency instead of polling.
Until then the only MSP mode available to a companion binary is
spawning its own host, which cannot observe the TUI's live session.
No commitment to build on MSP; revisit when daemon-hosted sessions
are the default or when a `muse serve --join` workflow is the
user's normal way of running Muse. A bounded stdio probe
(`initialize` + `session/list` + `session/read` against a stored
session) remains unrun — the schema answered the Phase 4 question
without starting a host.
