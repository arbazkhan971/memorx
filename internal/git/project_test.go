package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arbaz/devmem/internal/git"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Resolve symlinks (macOS /var -> /private/var)
	dir, _ = filepath.EvalSymlinks(dir)
	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	return dir
}

func TestFindGitRoot_FromRoot(t *testing.T) {
	dir := initTestRepo(t)
	root, err := git.FindGitRoot(dir)
	if err != nil {
		t.Fatalf("FindGitRoot: %v", err)
	}
	if root != dir {
		t.Fatalf("expected %s, got %s", dir, root)
	}
}

func TestFindGitRoot_FromSubdir(t *testing.T) {
	dir := initTestRepo(t)
	subdir := filepath.Join(dir, "src", "pkg")
	os.MkdirAll(subdir, 0755)

	root, err := git.FindGitRoot(subdir)
	if err != nil {
		t.Fatalf("FindGitRoot: %v", err)
	}
	if root != dir {
		t.Fatalf("expected %s, got %s", dir, root)
	}
}

func TestFindGitRoot_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := git.FindGitRoot(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestEnsureMemoryDir_Creates(t *testing.T) {
	dir := initTestRepo(t)
	memDir, err := git.EnsureMemoryDir(dir)
	if err != nil {
		t.Fatalf("EnsureMemoryDir: %v", err)
	}

	expected := filepath.Join(dir, ".memory")
	if memDir != expected {
		t.Fatalf("expected %s, got %s", expected, memDir)
	}

	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		t.Fatal(".memory/ dir not created")
	}
}

func TestEnsureMemoryDir_AddsGitignore(t *testing.T) {
	dir := initTestRepo(t)
	_, err := git.EnsureMemoryDir(dir)
	if err != nil {
		t.Fatalf("EnsureMemoryDir: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal("expected .gitignore to exist")
	}
	if !strings.Contains(string(content), ".memory/") {
		t.Fatal(".gitignore should contain .memory/")
	}
}

func TestEnsureMemoryDir_Idempotent(t *testing.T) {
	dir := initTestRepo(t)
	git.EnsureMemoryDir(dir)
	git.EnsureMemoryDir(dir) // second call

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal("expected .gitignore to exist")
	}
	// Should only appear once
	count := strings.Count(string(content), ".memory/")
	if count != 1 {
		t.Fatalf("expected .memory/ to appear once, got %d times", count)
	}
}

func TestProjectName(t *testing.T) {
	name := git.ProjectName("/Users/arbaz/Lineupx/LineupX-NextJS")
	if name != "LineupX-NextJS" {
		t.Fatalf("expected LineupX-NextJS, got %s", name)
	}
}
