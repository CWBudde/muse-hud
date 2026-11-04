package collect

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoSession reports that discovery found no session log matching the
// requested selector.
var ErrNoSession = errors.New("no Muse session found")

// Session is one discovered session log.
type Session struct {
	ID            string // session directory name
	Path          string // path to session.jsonl
	ModTimeUnixMs int64
	Size          int64
	// WorkspaceRoot is filled lazily by a bounded head scan (see
	// HeadWorkspaceRoot); empty until then or if the head carries none.
	WorkspaceRoot string
}

// FindOptions selects a session. At most one of File, SessionID and
// Last should be set; precedence is File > SessionID > Last > Workspace
// (newest for the workspace).
type FindOptions struct {
	MuseHome  string // muse home override (dir containing sessions/)
	File      string // explicit session.jsonl path
	SessionID string // explicit session id (directory name)
	Last      bool   // newest top-level session overall
	Workspace string // newest top-level session for this workspace root
}

// DefaultMuseHome returns ~/.local/share/muse on Linux (verified
// layout; PLAN.md assumption). The sessions live under
// <home>/sessions/YYYY/MM/DD/<session-id>/session.jsonl.
func DefaultMuseHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "muse"), nil
}

// SessionsDir resolves <muse-home>/sessions.
func SessionsDir(museHome string) (string, error) {
	if museHome == "" {
		var err error
		museHome, err = DefaultMuseHome()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(museHome, "sessions"), nil
}

// ListSessions enumerates top-level sessions only:
// sessions/YYYY/MM/DD/<id>/session.jsonl. Nested subagent logs
// (<id>/subagent/<child>/session.jsonl) are excluded — a HUD for a
// workspace shows the parent session; a child can still be selected
// explicitly via File or SessionID.
func ListSessions(museHome string) ([]Session, error) {
	dir, err := SessionsDir(museHome)
	if err != nil {
		return nil, err
	}
	var out []Session
	years, err := readDirs(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: sessions dir %s", ErrNoSession, dir)
	}
	if err != nil {
		return nil, err
	}
	for _, y := range years {
		months, err := readDirs(filepath.Join(dir, y))
		if err != nil {
			continue
		}
		for _, m := range months {
			days, err := readDirs(filepath.Join(dir, y, m))
			if err != nil {
				continue
			}
			for _, d := range days {
				ids, err := readDirs(filepath.Join(dir, y, m, d))
				if err != nil {
					continue
				}
				for _, id := range ids {
					p := filepath.Join(dir, y, m, d, id, "session.jsonl")
					if s, ok := statSession(id, p); ok {
						out = append(out, s)
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModTimeUnixMs != out[j].ModTimeUnixMs {
			return out[i].ModTimeUnixMs > out[j].ModTimeUnixMs
		}
		return out[i].Path > out[j].Path
	})
	return out, nil
}

func readDirs(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

func statSession(id, path string) (Session, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return Session{}, false
	}
	return Session{ID: id, Path: path, ModTimeUnixMs: fi.ModTime().UnixMilli(), Size: fi.Size()}, true
}

// Find resolves the requested session.
func Find(opts FindOptions) (Session, error) {
	switch {
	case opts.File != "":
		if _, err := os.Stat(opts.File); err != nil {
			return Session{}, fmt.Errorf("%w: file %s: %v", ErrNoSession, opts.File, err)
		}
		// A real session.jsonl lives in a dir named by the session id;
		// any other file name identifies itself.
		id := strings.TrimSuffix(filepath.Base(opts.File), filepath.Ext(opts.File))
		if filepath.Base(opts.File) == "session.jsonl" {
			id = filepath.Base(filepath.Dir(opts.File))
		}
		s, _ := statSession(id, opts.File)
		s.Path = opts.File
		if s.ID == "" || s.ID == "." {
			s.ID = id
		}
		return s, nil
	case opts.SessionID != "":
		return findByID(opts.MuseHome, opts.SessionID)
	case opts.Last:
		sessions, err := ListSessions(opts.MuseHome)
		if err != nil {
			return Session{}, err
		}
		if len(sessions) == 0 {
			return Session{}, ErrNoSession
		}
		return sessions[0], nil
	case opts.Workspace != "":
		return findForWorkspace(opts.MuseHome, opts.Workspace)
	default:
		return Session{}, fmt.Errorf("%w: no selector given", ErrNoSession)
	}
}

// findByID searches top-level and nested (subagent) session dirs.
func findByID(museHome, id string) (Session, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return Session{}, fmt.Errorf("%w: invalid session id %q", ErrNoSession, id)
	}
	dir, err := SessionsDir(museHome)
	if err != nil {
		return Session{}, err
	}
	var found Session
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || d.Name() != id {
			return nil
		}
		if s, ok := statSession(id, filepath.Join(path, "session.jsonl")); ok {
			found = s
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return Session{}, fmt.Errorf("%w: search %s: %v", ErrNoSession, dir, err)
	}
	if found.Path == "" {
		return Session{}, fmt.Errorf("%w: session id %s", ErrNoSession, id)
	}
	return found, nil
}

// maxWorkspaceScans bounds how many newest sessions are head-scanned
// while looking for a workspace match.
const maxWorkspaceScans = 50

func findForWorkspace(museHome, workspace string) (Session, error) {
	sessions, err := ListSessions(museHome)
	if err != nil {
		return Session{}, err
	}
	want := cleanPath(workspace)
	for i, s := range sessions {
		if i >= maxWorkspaceScans {
			break
		}
		root, err := HeadWorkspaceRoot(s.Path)
		if err != nil || root == "" {
			continue
		}
		s.WorkspaceRoot = root
		if cleanPath(root) == want {
			return s, nil
		}
	}
	return Session{}, fmt.Errorf("%w: for workspace %s", ErrNoSession, workspace)
}

func cleanPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}

// HeadWorkspaceRoot reads a bounded head of a session log (first
// headScanBytes) and returns the first workspace_root carried by a
// session.workspace_branch.observed, runtime.session.metadata, or
// session-start intake record. It never reads the whole log: large
// sessions can exceed 100 MiB.
const headScanBytes = 256 * 1024

func HeadWorkspaceRoot(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	var read int
	for read < headScanBytes {
		line, err := r.ReadBytes('\n')
		read += len(line)
		if root := workspaceRootFromLine(line); root != "" {
			return root, nil
		}
		if err != nil {
			return "", nil // EOF or partial final line: no root in head
		}
	}
	return "", nil
}

// workspaceRootFromLine extracts workspace_root from one raw envelope
// line via substring search + targeted decode, avoiding a full parse
// of every head line.
func workspaceRootFromLine(line []byte) string {
	idx := strings.Index(string(line), `"workspace_root"`)
	if idx < 0 {
		return ""
	}
	env, ok := parseEnvelopeLine(line)
	if !ok {
		return ""
	}
	root, _ := extractWorkspaceRoot(env)
	return root
}
