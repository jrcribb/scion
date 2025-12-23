package util

import (
	"os/exec"
	"strings"
)

// IsGitRepo returns true if the current working directory is inside a git repository.
func IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	err := cmd.Run()
	return err == nil
}

// RepoRoot returns the absolute path to the root of the git repository.
func RepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// IsIgnored returns true if the given path is ignored by git.
func IsIgnored(path string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", path)
	err := cmd.Run()
	return err == nil
}

// CreateWorktree creates a new git worktree at the specified path with a new branch.
func CreateWorktree(path, branch string) error {
	// git worktree add -b <branch> <path>
	cmd := exec.Command("git", "worktree", "add", "-b", branch, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		// If branch already exists, try to just add it
		if strings.Contains(string(output), "already exists") {
			cmd = exec.Command("git", "worktree", "add", path, branch)
			return cmd.Run()
		}
		return err
	}
	return nil
}

// RemoveWorktree removes a git worktree at the specified path.
func RemoveWorktree(path string) error {
	// git worktree remove <path>
	cmd := exec.Command("git", "worktree", "remove", path, "--force")
	return cmd.Run()
}
