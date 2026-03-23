package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arbaz/devmem/internal/memory"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// formatBriefing generates a concise "welcome back" briefing for stderr output
// and the devmem_briefing tool. It summarizes the active feature, plan progress,
// last session info, and the most recent note.
func formatBriefing(ctx *memory.Context, feature *memory.Feature) string {
	if feature == nil {
		return "devmem: No active feature. Use devmem_start_feature to begin."
	}

	var lines []string

	// Line 1: Active feature + branch
	featureLine := fmt.Sprintf("devmem: Welcome back! Active feature: %s", feature.Name)
	if feature.Branch != "" {
		featureLine += fmt.Sprintf(" (branch: %s)", feature.Branch)
	}
	lines = append(lines, featureLine)

	// Line 2: Plan progress (only if there's a plan)
	if ctx.Plan != nil {
		lines = append(lines, fmt.Sprintf("devmem: Plan: %s (%d/%d steps done)",
			ctx.Plan.Title, ctx.Plan.CompletedStep, ctx.Plan.TotalSteps))
	}

	// Line 3: Last session info
	if len(ctx.SessionHistory) > 0 {
		lastSession := ctx.SessionHistory[0]
		agoStr := formatTimeAgo(lastSession.StartedAt)
		lines = append(lines, fmt.Sprintf("devmem: Last session: %s via %s", agoStr, lastSession.Tool))
	}

	// Line 4: Most recent note (truncated)
	if len(ctx.RecentNotes) > 0 {
		content := strings.ReplaceAll(ctx.RecentNotes[0].Content, "\n", " ")
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		lines = append(lines, fmt.Sprintf("devmem: Recent: \"%s\"", content))
	}

	// Tip line
	lines = append(lines, "devmem: Tip: Say \"where did I leave off?\" for full context")

	return strings.Join(lines, "\n")
}

// formatTimeAgo converts a datetime string to a human-readable "X ago" format.
func formatTimeAgo(datetime string) string {
	t, err := time.Parse(time.DateTime, datetime)
	if err != nil {
		// Try RFC3339 as fallback
		t, err = time.Parse(time.RFC3339, datetime)
		if err != nil {
			return datetime
		}
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

// handleBriefing implements the devmem_briefing tool.
func (s *DevMemServer) handleBriefing(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	feature, err := s.store.GetActiveFeature()
	if err != nil {
		return mcplib.NewToolResultText("devmem: No active feature. Use devmem_start_feature to begin."), nil
	}

	// Use standard tier so we get notes + sessions for the briefing
	ctxData, err := s.store.GetContext(feature.ID, "standard", nil)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("Failed to load context: %v", err)), nil
	}

	// Also load session history for the briefing (standard tier doesn't include sessions)
	sessions, _ := s.store.ListSessions(feature.ID, 5)
	ctxData.SessionHistory = sessions

	return mcplib.NewToolResultText(formatBriefing(ctxData, feature)), nil
}
