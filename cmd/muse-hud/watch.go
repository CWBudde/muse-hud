package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/CWBudde/muse-hud/internal/config"
	"github.com/CWBudde/muse-hud/internal/render"
	"github.com/CWBudde/muse-hud/internal/watch"
)

// watchFlags are the `watch` subcommand flags: the session/config
// selection shared with print, plus the loop controls.
type watchFlags struct {
	sessionFlags
	plain    bool
	color    bool
	width    int
	interval time.Duration
	frames   int
	long     time.Duration
}

// cmdWatch runs the live side-pane view: it polls the newest session
// log (internal/watch model + internal/collect tailer) and redraws the
// Phase 2 renderer output on each tick. Terminal handling is standard
// library only: termios raw mode for single keys when stdin is a TTY
// (line-mode fallback otherwise), home+clear redraws on a TTY, and
// change-only plain frames when piped. --frames N renders exactly N
// frames and exits, which is the bounded smoke/scripting form.
func cmdWatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var wf watchFlags
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&wf.plain, "plain", false, "no ANSI colour")
	fs.BoolVar(&wf.color, "color", false, "force ANSI colour")
	fs.IntVar(&wf.width, "width", 0, "fit lines to n columns")
	fs.DurationVar(&wf.interval, "interval", 500*time.Millisecond, "poll/redraw interval")
	fs.IntVar(&wf.frames, "frames", 0, "render exactly n frames then exit (0 = run until q)")
	fs.DurationVar(&wf.long, "long-running", watch.DefaultLongRunning, "running-tool attention threshold")
	addSessionFlags(fs, &wf.sessionFlags)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "muse-hud watch: %v\n", err)
		return 2
	}
	if wf.width < 0 || wf.frames < 0 {
		fmt.Fprintln(stderr, "muse-hud watch: --width and --frames must be >= 0")
		return 2
	}
	if wf.interval < 100*time.Millisecond || wf.interval > 10*time.Second {
		fmt.Fprintln(stderr, "muse-hud watch: --interval must be 100ms..10s")
		return 2
	}
	if config.Disabled() {
		fmt.Fprintln(stderr, "muse-hud watch: disabled by MUSE_HUD_DISABLE=1")
		return 0
	}

	cfg, _, _ := loadConfig(wf.config) // bad config: defaults; doctor shows why
	collectOpts := wf.collectOptions()
	collectOpts.SkipVersion = true // watch never spawns `muse --version`; the log build tags the snapshot
	collectOpts.UseTrace = false
	collectOpts.UseUsage = false
	model := watch.New(watch.Options{
		Collect:     collectOpts,
		Config:      cfg,
		LongRunning: wf.long,
		SkipGit:     collectOpts.SkipGit,
	})

	return runWatch(model, wf, stdin, stdout)
}

// runWatch is the terminal loop around the model, split out so tests
// can drive it with pipes and a frame bound.
func runWatch(model *watch.Model, wf watchFlags, stdin io.Reader, stdout io.Writer) int {
	ttyOut := isTTY(stdout)

	keys := make(chan byte, 16)
	if stdin != nil {
		go readKeys(stdin, keys)
	}
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	if ttyOut {
		fmt.Fprint(stdout, "\x1b[?25l") // hide cursor
		defer fmt.Fprint(stdout, "\x1b[?25h\n")
	}
	restore := func() {}
	if f, ok := stdin.(*os.File); ok && isTTY(f) {
		if rest, err := makeRaw(f); err == nil {
			restore = rest
		}
	}
	defer restore()

	ctx := context.Background()
	ticker := time.NewTicker(wf.interval)
	defer ticker.Stop()

	lastFrame := ""
	framesDone := 0
	draw := func(now time.Time) {
		color := colorEnabled(printFlags{plain: wf.plain, color: wf.color}, model.Config(), stdout)
		width := resolveWidth(wf.width, model.Config(), stdout)
		lines := model.View(render.Options{Color: color, Width: width})
		frame := strings.Join(lines, "\n")
		framesDone++
		if ttyOut {
			fmt.Fprintf(stdout, "\x1b[H\x1b[2J%s\n", frame)
		} else if frame != lastFrame || wf.frames > 0 {
			// Piped: no clear escapes. With --frames, every frame is
			// emitted (separated) so a smoke run shows the sequence;
			// unbounded piped watch emits only on change.
			if lastFrame != "" {
				fmt.Fprintln(stdout, "---")
			}
			fmt.Fprintln(stdout, frame)
		}
		lastFrame = frame
	}

	now := time.Now()
	model.Update(ctx, now)
	draw(now)
	if wf.frames == 1 {
		return 0
	}
	for {
		select {
		case <-sigs:
			return 0
		case key, ok := <-keys:
			if !ok {
				keys = nil // stdin closed: keep watching until signal/frames
				continue
			}
			if model.HandleKey(key) {
				return 0
			}
			draw(time.Now()) // toggles/pause take effect immediately
			if wf.frames > 0 && framesDone >= wf.frames {
				return 0
			}
		case tick := <-ticker.C:
			model.Update(ctx, tick)
			draw(tick)
			if wf.frames > 0 && framesDone >= wf.frames {
				return 0
			}
		}
	}
}

// readKeys forwards stdin bytes to the key channel. Escape-sequence
// bytes (arrows etc.) are forwarded too and ignored by the model.
func readKeys(r io.Reader, keys chan<- byte) {
	defer close(keys)
	buf := make([]byte, 64)
	for {
		n, err := r.Read(buf)
		for _, b := range buf[:n] {
			keys <- b
		}
		if err != nil {
			return
		}
	}
}
