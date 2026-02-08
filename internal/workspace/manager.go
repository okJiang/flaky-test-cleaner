package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Options struct {
	RemoteURL    string
	MirrorDir    string
	WorktreesDir string
	MaxWorktrees int
	GitBinary    string
}

type Manager struct {
	opts     Options
	active   map[string]*Lease
	mu       sync.Mutex
	mirrorMu sync.Mutex
}

type Lease struct {
	Name   string
	Path   string
	Commit string

	mgr       *Manager
	releaseMu sync.Mutex
	released  bool
}

var ErrWorktreeLimit = errors.New("worktree limit reached")

func NewManager(opts Options) (*Manager, error) {
	if strings.TrimSpace(opts.RemoteURL) == "" {
		return nil, errors.New("workspace remote url must be set")
	}
	if strings.TrimSpace(opts.MirrorDir) == "" {
		return nil, errors.New("workspace mirror dir must be set")
	}
	if strings.TrimSpace(opts.WorktreesDir) == "" {
		return nil, errors.New("workspace worktrees dir must be set")
	}
	if opts.GitBinary == "" {
		opts.GitBinary = "git"
	}
	mirrorDir, err := filepath.Abs(opts.MirrorDir)
	if err != nil {
		return nil, err
	}
	worktreesDir, err := filepath.Abs(opts.WorktreesDir)
	if err != nil {
		return nil, err
	}
	opts.MirrorDir = mirrorDir
	opts.WorktreesDir = worktreesDir
	return &Manager{opts: opts, active: make(map[string]*Lease)}, nil
}

func (m *Manager) Ensure(ctx context.Context) error { return m.ensureMirror(ctx, true) }

func (m *Manager) ensureMirror(ctx context.Context, fetch bool) error {
	m.mirrorMu.Lock()
	defer m.mirrorMu.Unlock()

	if _, err := os.Stat(m.opts.MirrorDir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(m.opts.MirrorDir), 0o755); err != nil {
			return err
		}
		if _, err := m.runGit(ctx, "", "clone", "--mirror", m.opts.RemoteURL, m.opts.MirrorDir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if fetch {
		if _, err := m.runGit(ctx, m.opts.MirrorDir, "fetch", "--prune"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(m.opts.WorktreesDir, 0o755); err != nil {
		return err
	}
	return nil
}

func (m *Manager) CatFile(ctx context.Context, sha, path string) ([]byte, error) {
	if err := m.ensureMirror(ctx, false); err != nil {
		return nil, err
	}
	return m.runGit(ctx, m.opts.MirrorDir, "show", fmt.Sprintf("%s:%s", sha, path))
}

func (m *Manager) ListTree(ctx context.Context, sha, prefix string) ([]string, error) {
	if err := m.ensureMirror(ctx, false); err != nil {
		return nil, err
	}
	args := []string{"ls-tree", "-r", "--name-only", sha}
	if strings.TrimSpace(prefix) != "" {
		args = append(args, "--", prefix)
	}
	out, err := m.runGit(ctx, m.opts.MirrorDir, args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func (m *Manager) Grep(ctx context.Context, sha, pattern string, scopes ...string) ([]string, error) {
	if err := m.ensureMirror(ctx, false); err != nil {
		return nil, err
	}
	args := []string{"grep", "-n", "--no-color", "-e", pattern, sha}
	if len(scopes) > 0 {
		args = append(args, "--")
		args = append(args, scopes...)
	}
	out, err := m.runGit(ctx, m.opts.MirrorDir, args...)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

func (m *Manager) HasPath(ctx context.Context, sha, path string) (bool, error) {
	if err := m.ensureMirror(ctx, false); err != nil {
		return false, err
	}
	_, err := m.runGit(ctx, m.opts.MirrorDir, "cat-file", "-e", fmt.Sprintf("%s:%s", sha, path))
	if err != nil {
		if isGitNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *Manager) Acquire(ctx context.Context, name, sha string) (*Lease, error) {
	if err := m.ensureMirror(ctx, true); err != nil {
		return nil, err
	}

	cleanName := sanitizeName(name)
	worktreePath := filepath.Join(m.opts.WorktreesDir, cleanName)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.opts.MaxWorktrees > 0 && len(m.active) >= m.opts.MaxWorktrees {
		return nil, ErrWorktreeLimit
	}
	if existing, ok := m.active[cleanName]; ok {
		return nil, fmt.Errorf("worktree %s already active at %s", cleanName, existing.Path)
	}

	if err := os.RemoveAll(worktreePath); err != nil {
		return nil, err
	}

	if _, err := m.runGit(ctx, m.opts.MirrorDir, "worktree", "add", "--force", worktreePath, sha); err != nil {
		return nil, err
	}

	lease := &Lease{Name: cleanName, Path: worktreePath, Commit: sha, mgr: m}
	m.active[cleanName] = lease
	return lease, nil
}

func (l *Lease) Pathname() string { return l.Path }

func (l *Lease) Release(ctx context.Context) error {
	l.releaseMu.Lock()
	defer l.releaseMu.Unlock()
	if l.released {
		return nil
	}
	err := l.mgr.releaseLease(ctx, l)
	if err == nil {
		l.released = true
	}
	return err
}

func (m *Manager) releaseLease(ctx context.Context, lease *Lease) error {
	if _, err := m.runGit(ctx, m.opts.MirrorDir, "worktree", "remove", "--force", lease.Path); err != nil {
		return err
	}
	if err := os.RemoveAll(lease.Path); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.active, lease.Name)
	m.mu.Unlock()
	return nil
}

func (m *Manager) runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, m.opts.GitBinary, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out, fmt.Errorf("git %s failed: %s: %w", strings.Join(args, " "), strings.TrimSpace(stderr.String()), ee)
		}
		return out, err
	}
	return out, nil
}

func splitLines(b []byte) []string {
	raw := strings.Split(strings.TrimSpace(string(b)), "\n")
	var out []string
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func isGitNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Not a valid object name") || strings.Contains(msg, "does not exist in") || strings.Contains(msg, "pathspec")
}

func sanitizeName(name string) string {
	clean := filepath.Base(strings.TrimSpace(name))
	if clean == "." || clean == "" {
		return fmt.Sprintf("lease-%d", os.Getpid())
	}
	clean = strings.ReplaceAll(clean, string(os.PathSeparator), "-")
	clean = strings.ReplaceAll(clean, "..", "-")
	return clean
}
