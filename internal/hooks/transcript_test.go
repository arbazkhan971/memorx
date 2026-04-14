package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeTranscript_Empty(t *testing.T) {
	if got := SummarizeTranscript("/nonexistent/path.jsonl"); got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestSummarizeTranscript_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"user","message":{"content":"hello"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"We'll use Postgres for this."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/a/b.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git commit -m 'fix'"}}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := SummarizeTranscript(path)
	if !strings.Contains(got, "1 user turns") {
		t.Errorf("expected user turn count in %q", got)
	}
	if !strings.Contains(got, "1 files edited") {
		t.Errorf("expected file edit count in %q", got)
	}
	if !strings.Contains(got, "1 commits") {
		t.Errorf("expected commit count in %q", got)
	}
	if !strings.Contains(got, "Postgres") {
		t.Errorf("expected decision line in %q", got)
	}
}

func TestExtractDecisions(t *testing.T) {
	text := `Some context.
We'll use Redis for caching.
Random sentence.
Decided to ship on Monday.
Another.`
	got := extractDecisions(text)
	if len(got) != 2 {
		t.Fatalf("expected 2 decisions, got %d: %v", len(got), got)
	}
}
