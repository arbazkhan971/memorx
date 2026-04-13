package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/arbazkhan971/memorx/internal/dashboard"
	"github.com/arbazkhan971/memorx/internal/hooks"
	"github.com/arbazkhan971/memorx/internal/memory"
)

// hookEvent mirrors the JSON payload Claude Code sends to hooks on stdin.
// We only decode the fields we care about — unknown fields are tolerated.
type hookEvent struct {
	SessionID      string          `json:"session_id"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	Prompt         string          `json:"prompt"`
	TranscriptPath string          `json:"transcript_path"`
	CWD            string          `json:"cwd"`
}

func runHook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: memorx hook <session-start|user-prompt-submit|post-tool-use|stop|session-end>")
	}
	event := args[0]

	// Read stdin if available — Claude Code sends a JSON payload. Don't
	// block if nothing is piped in (useful for manual invocations).
	var payload hookEvent
	if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &payload) // tolerate invalid JSON
		}
	}

	db, gitRoot, closeDB, err := openProjectDB()
	if err != nil {
		// If we're not in a git repo, hooks should silently do nothing.
		return nil
	}
	defer closeDB()

	store := memory.NewStore(db)

	switch event {
	case "session-start":
		return hookSessionStart(store, gitRoot, payload)
	case "user-prompt-submit":
		return hookUserPromptSubmit(store, payload)
	case "post-tool-use":
		return hookPostToolUse(store, gitRoot, payload)
	case "stop":
		return nil
	case "session-end":
		return hookSessionEnd(store, payload)
	default:
		return fmt.Errorf("unknown hook event: %q", event)
	}
}

// hookSessionStart emits a briefing to stdout. Claude Code injects the
// stdout of a SessionStart hook as additional context into the session.
func hookSessionStart(store *memory.Store, gitRoot string, _ hookEvent) error {
	feature, err := store.GetActiveFeature()
	if err != nil {
		// No active feature yet — emit a friendly intro so users aren't confused.
		fmt.Println("memorx: no active feature yet. Use `memorx_start_feature` to begin.")
		return nil
	}

	sess, _ := store.CreateSession(feature.ID, "claude-code")
	if sess != nil {
		dashboard.PublishEvent("session_start", map[string]any{
			"feature":    feature.Name,
			"session_id": sess.ID,
		})
	}

	ctxData, err := store.GetContext(feature.ID, "standard", nil)
	if err != nil {
		fmt.Printf("memorx: active feature %s (context unavailable)\n", feature.Name)
		return nil
	}
	sessions, _ := store.ListSessions(feature.ID, 5)
	ctxData.SessionHistory = sessions

	fmt.Println(hooks.FormatBriefing(ctxData, feature))
	return nil
}

// hookUserPromptSubmit captures the user prompt as a low-signal observation.
// Privacy tags are stripped at the Store boundary (see memory.CreateNote).
func hookUserPromptSubmit(store *memory.Store, p hookEvent) error {
	if strings.TrimSpace(p.Prompt) == "" {
		return nil
	}
	feature, err := store.GetActiveFeature()
	if err != nil {
		return nil
	}
	sess, _ := store.GetCurrentSession()
	sessionID := ""
	if sess != nil {
		sessionID = sess.ID
	}
	content := "user: " + truncatePrompt(p.Prompt, 500)
	note, err := store.CreateNote(feature.ID, sessionID, content, "observation")
	if err == nil && note != nil {
		dashboard.PublishEvent("observation", map[string]any{
			"feature": feature.Name,
			"content": note.Content,
			"type":    note.Type,
		})
	}
	return nil
}

// hookPostToolUse watches for git commits and triggers a sync. Any commit
// via Bash(git commit ...) kicks off memorx_sync behind the scenes.
func hookPostToolUse(store *memory.Store, gitRoot string, p hookEvent) error {
	if p.ToolName != "Bash" {
		return nil
	}
	// Tool input for Bash looks like: {"command": "git commit -m ...", ...}
	var input struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(p.ToolInput, &input)
	cmd := strings.TrimSpace(input.Command)
	if !strings.Contains(cmd, "git commit") && !strings.Contains(cmd, "git merge") {
		return nil
	}
	feature, err := store.GetActiveFeature()
	if err != nil {
		return nil
	}
	sess, _ := store.GetCurrentSession()
	sessionID := ""
	if sess != nil {
		sessionID = sess.ID
	}
	// Delegate to the hooks package so install + MCP server share one impl.
	if n, err := hooks.SyncCommits(store, gitRoot, feature.ID, sessionID); err == nil && n > 0 {
		dashboard.PublishEvent("commits_synced", map[string]any{
			"feature": feature.Name,
			"count":   n,
		})
	}
	return nil
}

// hookSessionEnd generates a rule-based summary from the transcript and
// ends the current session with it.
func hookSessionEnd(store *memory.Store, p hookEvent) error {
	sess, err := store.GetCurrentSession()
	if err != nil || sess == nil {
		return nil
	}
	summary := ""
	if p.TranscriptPath != "" {
		summary = hooks.SummarizeTranscript(p.TranscriptPath)
	}
	if summary == "" {
		summary = fmt.Sprintf("Session ended at %s", time.Now().UTC().Format(time.RFC3339))
	}
	_ = store.EndSessionWithSummary(sess.ID, summary)
	dashboard.PublishEvent("session_end", map[string]any{
		"session_id": sess.ID,
		"summary":    summary,
	})
	return nil
}

func truncatePrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
