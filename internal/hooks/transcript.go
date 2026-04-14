package hooks

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SummarizeTranscript reads a Claude Code JSONL session transcript and
// produces a rule-based summary. No LLM dependency — we extract tool
// calls, edits, commits, and explicit TODO/decision patterns from the
// assistant and user messages.
//
// This is intentionally simple so it stays fast and deterministic. A
// future version can swap in an LLM summarizer behind the same interface.
func SummarizeTranscript(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var (
		toolCounts = map[string]int{}
		filesEdited = map[string]bool{}
		commits     []string
		decisions   []string
		userTurns   int
	)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24) // allow big lines

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "user":
			userTurns++
		case "assistant":
			scanAssistant(entry.Message, toolCounts, filesEdited, &commits, &decisions)
		}
	}

	return renderSummary(userTurns, toolCounts, filesEdited, commits, decisions)
}

func scanAssistant(msg json.RawMessage, toolCounts map[string]int, filesEdited map[string]bool, commits, decisions *[]string) {
	var m struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		return
	}
	for _, c := range m.Content {
		switch c.Type {
		case "tool_use":
			toolCounts[c.Name]++
			switch c.Name {
			case "Edit", "Write", "NotebookEdit":
				var in struct {
					FilePath string `json:"file_path"`
				}
				if json.Unmarshal(c.Input, &in) == nil && in.FilePath != "" {
					filesEdited[in.FilePath] = true
				}
			case "Bash":
				var in struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(c.Input, &in) == nil {
					if strings.Contains(in.Command, "git commit") {
						*commits = append(*commits, trimCommand(in.Command))
					}
				}
			}
		case "text":
			for _, d := range extractDecisions(c.Text) {
				*decisions = append(*decisions, d)
				if len(*decisions) >= 5 {
					return
				}
			}
		}
	}
}

// extractDecisions finds sentences in assistant text that look like
// decisions or conclusions, e.g. "decided to...", "going with...",
// "chose X because Y".
func extractDecisions(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(line) > 240 {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "decided ") ||
			strings.Contains(lower, "going with ") ||
			strings.Contains(lower, "we'll use ") ||
			strings.Contains(lower, "we will use ") ||
			strings.HasPrefix(lower, "chose ") {
			out = append(out, line)
		}
	}
	return out
}

func trimCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) > 120 {
		return cmd[:120] + "..."
	}
	return cmd
}

func renderSummary(userTurns int, toolCounts map[string]int, filesEdited map[string]bool, commits, decisions []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Session: %d user turns", userTurns)

	if n := len(filesEdited); n > 0 {
		fmt.Fprintf(&b, ", %d files edited", n)
	}
	if n := len(commits); n > 0 {
		fmt.Fprintf(&b, ", %d commits", n)
	}
	topTool, topCount := "", 0
	for name, n := range toolCounts {
		if n > topCount {
			topTool, topCount = name, n
		}
	}
	if topTool != "" {
		fmt.Fprintf(&b, ". Most-used tool: %s (%d)", topTool, topCount)
	}
	if len(decisions) > 0 {
		b.WriteString(". Decisions: ")
		b.WriteString(strings.Join(decisions, "; "))
	}
	if len(filesEdited) > 0 && len(filesEdited) <= 5 {
		files := make([]string, 0, len(filesEdited))
		for f := range filesEdited {
			files = append(files, f)
		}
		fmt.Fprintf(&b, ". Files: %s", strings.Join(files, ", "))
	}
	b.WriteString(".")
	return b.String()
}
