# Integrations

Recipes for using `muse-hud` from prompts, status bars, side panes,
and scripts. Every command below was run exactly as written from a
clean shell (`bash --noprofile --norc`) at the repository root on
2026-11-04 with Muse 1.4.14 present; session-dependent values
(model, branch, tools) vary on your machine, the shapes do not.
`--stub` recipes are byte-stable everywhere.

Build once, from the repository root:

```sh
make build
```

All recipes call `./build/muse-hud`. If you copy the binary
elsewhere, substitute that path; the flags are unchanged.

## 1. Try the layout without a session

```sh
./build/muse-hud print --stub
```

Prints the hard-coded stub snapshot (model/workspace/tools/todos
lines). No Muse session, config, or `git` repo is needed.

## 2. Shell prompt (bash)

`print` always exits 0 and prints nothing when no session is found,
so it is safe inside a prompt:

```sh
hud_prompt() { ./build/muse-hud print --plain --width 80 | head -n 1; }
PS1='$(hud_prompt)\n\$ '
```

This puts the first HUD line (model · effort, workspace, branch)
above each prompt in a shell started from the repository root. Drop
`| head -n 1` for the full two-plus lines. Note that `print` runs on
every prompt redraw; on a very large repository the fixed `git`
dirty probe dominates its time (see the README's performance note).

## 3. tmux status bar

```sh
tmux set -g status-interval 2
tmux set -g status-right '#(./build/muse-hud print --plain --width 60 | head -n 1)'
```

The bar then re-runs the one-shot line every 2 seconds. tmux runs
`#(...)` commands from the server's working directory, so use an
absolute path to the binary here if your tmux server was not started
in this repository.

## 4. Side-pane watch

For a live pane beside Muse, `watch` tails the newest session and
redraws on change (keys: `q` quit, `p` pause, `t`/`a`/`d` toggles):

```sh
./build/muse-hud watch
```

Follow the newest session regardless of workspace:

```sh
./build/muse-hud watch --last
```

Bounded form — render two frames and exit (smoke tests, scripting):

```sh
./build/muse-hud watch --frames 2 --interval 250ms --plain
```

## 5. Scripting with `--json`

`--json` emits one snapshot object (`schemaVersion: 1`); unsourced
fields are `null` or absent, never invented. With `jq`:

```sh
./build/muse-hud print --json | jq -r '"\(.model // "?") \(.branch // "-")"'
./build/muse-hud print --json | jq '[.tools[]? | select(.running == true)] | length'
./build/muse-hud print --json | jq -e '.contextPercent // "unavailable"'
```

The first prints `<model> <branch>`, the second the number of
currently running tools, the third shows the honest `null` for live
context % on Muse 1.4.14 (no local surface provides it; see
`docs/spikes/a-context-usage.md`).

No session is not an error — `print` still exits 0 with empty lines,
which is what prompt/status-bar callers rely on:

```sh
./build/muse-hud print --muse-home /nonexistent --plain; echo "exit=$?"
```

## 6. Config file and kill switch

Point at a config explicitly (same JSON as
`~/.config/muse-hud/config.json`):

```sh
printf '%s\n' '{"width": 60, "plain": true}' > /tmp/muse-hud-config.json
./build/muse-hud print --stub --config /tmp/muse-hud-config.json
```

Disable the HUD without removing it from your prompt/bar:

```sh
MUSE_HUD_DISABLE=1 ./build/muse-hud print; echo "exit=$?"
```

## 7. When a segment is missing

```sh
./build/muse-hud doctor
```

`doctor` reports the Muse version, chosen session, adapter results,
and per-segment why each is shown or hidden.

## What there is not (Phase 4 findings)

- **No custom in-TUI statusline.** Muse 1.4.14 exposes no
  statusLine/status-command hook for custom content in its CLI help
  or settings keys — re-checked in
  `docs/spikes/h-in-tui-status-hook.md`. Its built-in HUD does have
  per-segment toggles (`tui.hud.model`, `.effort`, `.permissions`,
  `.context`, `.branch`, `.tokens`, `.cost`, `.quota` in Muse's
  `settings.json`; spike h, 2026-11-07 correction) — worth enabling
  for live context/quota, which muse-hud cannot source externally —
  but it renders only Muse's own segments. The recipes above remain
  muse-hud's integration surface.
- **No Muse plugin.** A minimal hook bundle validates, but hooks add
  no rendering surface and no freshness win worth running code inside
  Muse; the spike scaffold is kept uninstalled under
  `docs/spikes/plugin-probe-bundle/` with the verdict in
  `docs/spikes/f-plugin-hooks.md`.
- **MSP is promising but unbuilt.** `muse serve`'s stable schema has
  lease-free read/subscribe methods (`session/read`,
  `view/subscribe`, context/todo notifications) — research only, see
  `docs/spikes/g-msp.md`.
