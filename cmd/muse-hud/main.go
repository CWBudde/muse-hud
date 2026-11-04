// Command muse-hud is a read-only heads-up display companion for Muse.
// `print` collects a snapshot from the newest Muse session log for
// the workspace (plus fixed git probes) and renders it, or emits it as
// JSON with --json. `doctor` explains what was found and why any
// segment is hidden.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"

	"github.com/CWBudde/muse-hud/internal/collect"
	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/snapshot"
)

// Version is the muse-hud release version (see CHANGELOG.md). Keep
// in sync with release tags once a remote exists.
const Version = "0.1.0"

const usage = `muse-hud — heads-up display companion for Muse (read-only, offline)

Usage:
  muse-hud print [flags]   print the HUD lines once (always exits 0)
  muse-hud watch [flags]   follow the newest session live in this terminal
  muse-hud doctor [flags]  explain sources and hidden segments
  muse-hud version         print the muse-hud version
  muse-hud help            show this help

print flags:
  --json             emit the snapshot as JSON instead of HUD lines
  --stub             render the hard-coded stub snapshot (no live data)
  --plain            no ANSI colour (also: NO_COLOR env, non-TTY output)
  --color            force ANSI colour even when output is not a TTY
  --width <n>        fit lines to n columns (default: config, else terminal)
  --config <path>    config file (default: <user config dir>/muse-hud/config.json)
  --session <id>     use this session id instead of discovery
  --file <path>      use this session.jsonl instead of discovery
  --last             use the newest session overall (ignore workspace)
  --workspace <dir>  workspace to discover for (default: current dir)
  --muse-home <dir>  muse home override (dir containing sessions/)
  --with-usage       also probe 'muse usage --json' for the allowance
  --with-trace       also run 'muse trace inspect' on the chosen log
  --full-scan        parse the whole log (default: bounded head+tail window)

watch flags: the session/config/--plain/--color/--width flags above, plus
  --interval <d>     poll/redraw interval, 100ms..10s (default 500ms)
  --frames <n>       render exactly n frames then exit (default: until q)
  --long-running <d> running-tool attention threshold (default 30s)
  Keys while watching: q quit, p pause, t/a/d toggle tools/agents/todos lines.

doctor accepts the session/config flags above (not --json/--stub/render flags).
Set MUSE_HUD_DISABLE=1 to make print emit nothing (doctor still reports).
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a subcommand and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "muse-hud %s\n", Version)
		return 0
	case "print":
		return cmdPrint(args[1:], stdout, stderr)
	case "watch":
		return cmdWatch(args[1:], os.Stdin, stdout, stderr)
	case "doctor":
		return cmdDoctor(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "muse-hud: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// sessionFlags selects which session to collect; shared by print/doctor.
type sessionFlags struct {
	session   string
	file      string
	last      bool
	workspace string
	museHome  string
	withUsage bool
	withTrace bool
	fullScan  bool
	config    string
}

func addSessionFlags(fs *flag.FlagSet, sf *sessionFlags) {
	fs.StringVar(&sf.session, "session", "", "session id")
	fs.StringVar(&sf.file, "file", "", "session.jsonl path")
	fs.BoolVar(&sf.last, "last", false, "newest session overall")
	fs.StringVar(&sf.workspace, "workspace", "", "workspace dir")
	fs.StringVar(&sf.museHome, "muse-home", "", "muse home override")
	fs.BoolVar(&sf.withUsage, "with-usage", false, "probe muse usage --json")
	fs.BoolVar(&sf.withTrace, "with-trace", false, "run muse trace inspect")
	fs.BoolVar(&sf.fullScan, "full-scan", false, "parse the whole log")
	fs.StringVar(&sf.config, "config", "", "config file path")
}

func (sf sessionFlags) collectOptions() collect.Options {
	workspace := sf.workspace
	if workspace == "" && sf.file == "" && sf.session == "" && !sf.last {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		}
	}
	return collect.Options{
		MuseHome:  sf.museHome,
		File:      sf.file,
		SessionID: sf.session,
		Last:      sf.last,
		Workspace: workspace,
		UseTrace:  sf.withTrace,
		UseUsage:  sf.withUsage,
		FullScan:  sf.fullScan,
	}
}

type printFlags struct {
	sessionFlags
	jsonOut bool
	stub    bool
	plain   bool
	color   bool
	width   int
}

// cmdPrint renders one HUD snapshot. It always exits 0: a HUD must
// never break the prompt or status bar that invoked it. Collection
// problems degrade to fewer (or no) segments; doctor explains them.
func cmdPrint(args []string, stdout, stderr io.Writer) int {
	if config.Disabled() {
		return 0
	}
	var pf printFlags
	fs := flag.NewFlagSet("print", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&pf.jsonOut, "json", false, "emit snapshot JSON")
	fs.BoolVar(&pf.stub, "stub", false, "render the stub snapshot")
	fs.BoolVar(&pf.plain, "plain", false, "no ANSI colour")
	fs.BoolVar(&pf.color, "color", false, "force ANSI colour")
	fs.IntVar(&pf.width, "width", 0, "fit lines to n columns")
	addSessionFlags(fs, &pf.sessionFlags)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "muse-hud print: %v\n", err)
		return 0
	}
	if pf.width < 0 {
		fmt.Fprintf(stderr, "muse-hud print: --width must be >= 0\n")
		return 0
	}

	cfg, _, _ := loadConfig(pf.config) // bad config: defaults; doctor shows why
	snap := collectSnapshot(pf)
	if pf.jsonOut {
		if out, err := render.JSON(snap); err == nil {
			fmt.Fprintln(stdout, string(out))
		}
		return 0
	}
	opts := render.Options{
		Color: colorEnabled(pf, cfg, stdout),
		Width: resolveWidth(pf.width, cfg, stdout),
	}
	if out := render.StringWithOptions(snap, cfg, opts); out != "" {
		fmt.Fprintln(stdout, out)
	}
	return 0
}

// colorEnabled resolves colour: --plain, config plain, NO_COLOR and a
// dumb terminal always win; --color forces colour for piped status
// bars; otherwise colour only on a TTY.
func colorEnabled(pf printFlags, cfg config.Config, stdout io.Writer) bool {
	if pf.plain || cfg.Plain {
		return false
	}
	if _, present := os.LookupEnv("NO_COLOR"); present {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	if pf.color {
		return true
	}
	return isTTY(stdout)
}

// resolveWidth resolves the column budget: --width, then config, then
// the terminal size (TTY only), then $COLUMNS; 0 = unbounded.
func resolveWidth(flagWidth int, cfg config.Config, stdout io.Writer) int {
	if flagWidth > 0 {
		return flagWidth
	}
	if cfg.Width > 0 {
		return cfg.Width
	}
	if w := terminalWidth(stdout); w > 0 {
		return w
	}
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// loadedConfig bundles the config with how loading went, for doctor.
type loadedConfig struct {
	cfg    config.Config
	path   string
	exists bool
	err    error
}

// loadConfig loads the config at an explicit path, or the default
// <os.UserConfigDir()>/muse-hud/config.json. Bad config falls back to
// defaults; the error is returned for doctor, never for the HUD lines.
func loadConfig(explicit string) (config.Config, string, error) {
	lc := loadConfigFull(explicit)
	return lc.cfg, lc.path, lc.err
}

func loadConfigFull(explicit string) loadedConfig {
	lc := loadedConfig{cfg: config.Default()}
	path := explicit
	if path == "" {
		var err error
		path, err = config.Path()
		if err != nil {
			lc.err = err
			return lc
		}
	}
	lc.path = path
	if _, err := os.Stat(path); err == nil {
		lc.exists = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		lc.err = err
		return lc
	}
	cfg, err := config.Load(path)
	lc.cfg = cfg
	lc.err = err
	return lc
}

func collectSnapshot(pf printFlags) snapshot.Snapshot {
	if pf.stub {
		return collect.StubSnapshot()
	}
	res, err := collect.Collect(context.Background(), pf.collectOptions())
	if err != nil {
		// No session (or unreadable log): an empty, schema-tagged
		// snapshot renders as nothing — and as honest empty JSON.
		return snapshot.Snapshot{SchemaVersion: snapshot.SchemaVersion, Sources: map[string]snapshot.Source{}}
	}
	return res.Snapshot
}
