package collect

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Runner runs a fixed-argument external command and returns its stdout.
// It is the single seam through which adapters shell out (to `muse`),
// so tests can substitute synthetic fixtures. Arguments are always
// fixed by the caller — never a shell, never user-supplied strings
// beyond a session-log path muse-hud discovered itself.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the production Runner: exec.CommandContext with no
// shell. Stderr is captured only to enrich errors. Note `muse trace
// inspect` exits non-zero on a partial (still-running) log while still
// emitting a usable JSON report on stdout, so adapters must try to
// parse stdout even when err != nil.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", name, err, firstLine(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
