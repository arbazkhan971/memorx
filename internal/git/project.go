package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindGitRoot returns the root directory of the git repository
// containing the given directory.
func FindGitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// EnsureMemoryDir creates the .memory/ directory at the git root
// and adds it to .gitignore if not already present.
func EnsureMemoryDir(gitRoot string) (string, error) {
	memDir := filepath.Join(gitRoot, ".memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return "", fmt.Errorf("create .memory dir: %w", err)
	}

	if err := ensureGitignore(gitRoot); err != nil {
		return "", fmt.Errorf("update .gitignore: %w", err)
	}

	return memDir, nil
}

// CurrentBranch returns the current git branch name.
func CurrentBranch(gitRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = gitRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ProjectName returns the name of the git repository directory.
func ProjectName(gitRoot string) string {
	return filepath.Base(gitRoot)
}

func ensureGitignore(gitRoot string) error {
	gitignorePath := filepath.Join(gitRoot, ".gitignore")

	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if strings.Contains(string(content), ".memory/") {
		return nil // already in .gitignore
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := ".memory/\n"
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		entry = "\n" + entry
	}
	_, err = f.WriteString(entry)
	return err
}
