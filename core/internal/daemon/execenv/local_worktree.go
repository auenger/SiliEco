package execenv

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func localGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", cmdArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func prepareLocalWorktree(ctx context.Context, source, target, branch string) (string, string, string, error) {
	repoRoot, err := localGit(ctx, source, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", "", fmt.Errorf("local worktree requires a Git repository: %w", err)
	}
	repoRoot, err = filepath.Abs(repoRoot)
	if err != nil {
		return "", "", "", err
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return "", "", "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(repoRoot); resolveErr == nil {
		repoRoot = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(sourceAbs); resolveErr == nil {
		sourceAbs = resolved
	}
	rel, err := filepath.Rel(repoRoot, sourceAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("local worktree source is outside its Git root")
	}
	status, err := localGit(ctx, repoRoot, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", "", "", err
	}
	if status != "" {
		return "", "", "", fmt.Errorf("local worktree mode requires a clean working tree; commit or stash local changes first")
	}
	base, err := localGit(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("local worktree requires at least one commit: %w", err)
	}
	if _, err := localGit(ctx, repoRoot, "worktree", "prune"); err != nil {
		return "", "", "", err
	}
	if _, err := localGit(ctx, repoRoot, "worktree", "add", "-b", branch, target, base); err != nil {
		return "", "", "", err
	}
	return target, filepath.Join(target, rel), base, nil
}

// FinalizeLocalWorktree commits task changes to the server-generated branch.
// The linked worktree is intentionally retained until normal env GC so a failed
// commit never destroys user work and the branch remains inspectable.
func FinalizeLocalWorktree(ctx context.Context, root, branch string) (string, error) {
	if root == "" {
		return "", nil
	}
	if _, err := localGit(ctx, root, "add", "-A"); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--cached", "--quiet")
	if err := cmd.Run(); err == nil {
		return localGit(ctx, root, "rev-parse", "HEAD")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		return "", err
	}
	if _, err := localGit(ctx, root, "-c", "user.name=Silieco Agent", "-c", "user.email=agent@silieco.local", "commit", "--no-gpg-sign", "--no-verify", "-m", "Silieco task changes ("+branch+")"); err != nil {
		return "", err
	}
	return localGit(ctx, root, "rev-parse", "HEAD")
}
