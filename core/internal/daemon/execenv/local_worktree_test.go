package execenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

func TestLocalWorktreeCreatesIsolatedBranchAndFinalizesChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.name", "Test")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "subdir", "file.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "base")
	target := filepath.Join(t.TempDir(), "task-worktree")
	root, cwd, baseSHA, err := prepareLocalWorktree(context.Background(), filepath.Join(repo, "subdir"), target, "silieco/task-test")
	if err != nil {
		t.Fatal(err)
	}
	if baseSHA == "" {
		t.Fatal("expected base SHA")
	}
	if cwd != filepath.Join(target, "subdir") {
		t.Fatalf("cwd = %q", cwd)
	}
	if err := os.WriteFile(filepath.Join(cwd, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finalSHA, err := FinalizeLocalWorktree(context.Background(), root, "silieco/task-test")
	if err != nil {
		t.Fatal(err)
	}
	if finalSHA == "" || finalSHA == baseSHA {
		t.Fatalf("final SHA = %q, base SHA = %q", finalSHA, baseSHA)
	}
	got := runTestGit(t, repo, "show", "silieco/task-test:subdir/file.txt")
	if got != "changed\n" {
		t.Fatalf("branch content = %q", got)
	}
	base := runTestGit(t, repo, "show", "HEAD:subdir/file.txt")
	if base != "base\n" {
		t.Fatalf("original checkout changed: %q", base)
	}
}

func TestLocalWorktreeRejectsDirtySource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.name", "Test")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := prepareLocalWorktree(context.Background(), repo, filepath.Join(t.TempDir(), "wt"), "silieco/task-dirty"); err == nil {
		t.Fatal("expected dirty source to be rejected")
	}
}
