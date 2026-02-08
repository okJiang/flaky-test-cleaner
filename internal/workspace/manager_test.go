package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}

	runGitTest(t, origin, "init")
	runGitTest(t, origin, "config", "user.email", "ci@example.com")
	runGitTest(t, origin, "config", "user.name", "CI")

	writeFile(t, filepath.Join(origin, "README.md"), "hello workspace\n")
	runGitTest(t, origin, "add", ".")
	runGitTest(t, origin, "commit", "-m", "init")
	sha := strings.TrimSpace(string(runGitTest(t, origin, "rev-parse", "HEAD")))

	opts := Options{RemoteURL: origin, MirrorDir: filepath.Join(root, "mirror.git"), WorktreesDir: filepath.Join(root, "worktrees"), MaxWorktrees: 1}
	mgr, err := NewManager(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Ensure(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	content, err := mgr.CatFile(ctx, sha, "README.md")
	if err != nil {
		t.Fatalf("cat file: %v", err)
	}
	if got := strings.TrimSpace(string(content)); got != "hello workspace" {
		t.Fatalf("unexpected content: %s", got)
	}

	files, err := mgr.ListTree(ctx, sha, "")
	if err != nil {
		t.Fatalf("list tree: %v", err)
	}
	if len(files) != 1 || files[0] != "README.md" {
		t.Fatalf("unexpected files: %v", files)
	}

	lease, err := mgr.Acquire(ctx, "fp123", sha)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lease.Path, "README.md")); err != nil {
		t.Fatalf("stat worktree file: %v", err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := mgr.Acquire(ctx, "fp456", sha); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestManagerWorktreeLimit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}

	runGitTest(t, origin, "init")
	runGitTest(t, origin, "config", "user.email", "ci@example.com")
	runGitTest(t, origin, "config", "user.name", "CI")
	writeFile(t, filepath.Join(origin, "file.txt"), "data\n")
	runGitTest(t, origin, "add", ".")
	runGitTest(t, origin, "commit", "-m", "init")
	sha := strings.TrimSpace(string(runGitTest(t, origin, "rev-parse", "HEAD")))

	opts := Options{RemoteURL: origin, MirrorDir: filepath.Join(root, "mirror.git"), WorktreesDir: filepath.Join(root, "worktrees"), MaxWorktrees: 1}
	mgr, err := NewManager(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Ensure(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	lease, err := mgr.Acquire(ctx, "first", sha)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := mgr.Acquire(ctx, "second", sha); !errorsIs(err, ErrWorktreeLimit) {
		t.Fatalf("expected worktree limit error, got %v", err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func errorsIs(err error, target error) bool {
	return err != nil && target != nil && (errors.Is(err, target) || strings.Contains(err.Error(), target.Error()))
}
