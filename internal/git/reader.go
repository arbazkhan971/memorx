package git

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Commit struct {
	Hash, Message, Author, CommittedAt string
	FilesChanged                       []FileChange
}

type FileChange struct {
	Path, Action string
}

func ReadCommits(gitRoot string, since time.Time) ([]Commit, error) {
	cmd := exec.Command("git", "log",
		"--since="+since.Format(time.RFC3339),
		"--format=%H||%s||%an||%aI",
		"--no-merges",
	)
	cmd.Dir = gitRoot
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, nil
		}
		return nil, fmt.Errorf("git log: %w", err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	var commits []Commit
	for _, line := range strings.Split(trimmed, "\n") {
		c, ok := parseCommitLine(line)
		if !ok {
			continue
		}
		if c.FilesChanged, err = readFilesChanged(gitRoot, c.Hash); err != nil {
			return nil, fmt.Errorf("read files for %s: %w", c.Hash, err)
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func parseCommitLine(line string) (Commit, bool) {
	parts := strings.SplitN(strings.TrimSpace(line), "||", 4)
	if len(parts) != 4 {
		return Commit{}, false
	}
	return Commit{Hash: parts[0], Message: parts[1], Author: parts[2], CommittedAt: parts[3]}, true
}

func readFilesChanged(gitRoot, hash string) ([]FileChange, error) {
	raw, err := gitOutput(gitRoot, "diff-tree", "--root", "--no-commit-id", "--name-status", "-r", hash)
	if err != nil {
		return nil, fmt.Errorf("git diff-tree: %w", err)
	}
	var files []FileChange
	for _, line := range strings.Split(raw, "\n") {
		if parts := strings.Fields(line); len(parts) >= 2 {
			path := parts[1]
			if strings.HasPrefix(parts[0], "R") && len(parts) >= 3 {
				path = parts[2]
			}
			files = append(files, FileChange{Path: path, Action: parseAction(parts[0])})
		}
	}
	return files, nil
}

var actionMap = map[string]string{"A": "added", "D": "deleted"}

func parseAction(status string) string {
	if a, ok := actionMap[status]; ok {
		return a
	}
	if strings.HasPrefix(status, "C") {
		return "added"
	}
	return "modified"
}
