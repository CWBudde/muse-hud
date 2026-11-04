package collect

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitState is the result of the fixed git probes in a workspace.
type GitState struct {
	Branch string // branch name, or short commit when detached
	Dirty  bool   // any porcelain status output (tracked or untracked)
}

// DefaultGitTimeout bounds each git probe. A HUD must never hang on a
// slow or locked repository; on timeout the branch segment falls back
// to whatever the session log itself observed.
const DefaultGitTimeout = 500 * time.Millisecond

// ProbeGit runs fixed-argument, shell-free git probes in dir:
// `git -C <dir> rev-parse --abbrev-ref HEAD` and
// `git -C <dir> status --porcelain`. GIT_OPTIONAL_LOCKS=0 keeps git
// from taking optional locks (read-only by construction).
func ProbeGit(ctx context.Context, dir string, timeout time.Duration) (GitState, error) {
	if dir == "" {
		return GitState{}, fmt.Errorf("git probe: empty dir")
	}
	if timeout <= 0 {
		timeout = DefaultGitTimeout
	}
	branch, err := gitOutput(ctx, dir, timeout, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return GitState{}, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "HEAD" || branch == "" { // detached HEAD
		if sha, serr := gitOutput(ctx, dir, timeout, "rev-parse", "--short", "HEAD"); serr == nil {
			branch = strings.TrimSpace(sha)
		}
	}
	status, err := gitOutput(ctx, dir, timeout, "status", "--porcelain")
	if err != nil {
		// Branch without a dirty verdict: report the error so the
		// caller keeps the log-observed state instead of guessing.
		return GitState{}, err
	}
	return GitState{Branch: branch, Dirty: strings.TrimSpace(status) != ""}, nil
}

func gitOutput(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	// Porcelain output can be long; only emptiness matters, but the
	// whole stream must be drained so git is not SIGPIPEd. Cap what we
	// retain at 64 KiB.
	var buf cappedBuffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return buf.String(), nil
}

type cappedBuffer struct {
	bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	const max = 64 * 1024
	if b.Len() < max {
		keep := max - b.Len()
		if keep > len(p) {
			keep = len(p)
		}
		_, _ = b.Buffer.Write(p[:keep])
	}
	return len(p), nil
}
