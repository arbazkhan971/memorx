// Package hooks contains shared helpers used by both the MCP server and
// the CLI hook subcommands. Keeping these outside internal/mcp lets the
// cmd/devmem binary invoke them without depending on the full MCP layer.
package hooks

import (
	"fmt"
	"strings"
	"time"

	"github.com/arbazkhan971/memorx/internal/memory"
)

// FormatBriefing renders a short "welcome back" summary of the active
// feature for injection into Claude Code sessions via SessionStart hook.
func FormatBriefing(ctx *memory.Context, feature *memory.Feature) string {
	if feature == nil {
		return "memorx: No active feature. Use memorx_start_feature to begin."
	}
	var lines []string
	fl := fmt.Sprintf("memorx: Welcome back! Active feature: %s", feature.Name)
	if feature.Branch != "" {
		fl += fmt.Sprintf(" [%s]", feature.Branch)
	}
	lines = append(lines, fl)
	if ctx.Plan != nil {
		lines = append(lines, fmt.Sprintf("memorx: plan: %s (%d/%d steps done)", ctx.Plan.Title, ctx.Plan.CompletedStep, ctx.Plan.TotalSteps))
	}
	if len(ctx.SessionHistory) > 0 {
		lines = append(lines, fmt.Sprintf("memorx: last: %s via %s", formatTimeAgo(ctx.SessionHistory[0].StartedAt), ctx.SessionHistory[0].Tool))
	}
	if ctx.LastSessionSummary != "" {
		lines = append(lines, "memorx: last summary: "+truncate(ctx.LastSessionSummary, 160))
	}
	if len(ctx.RecentNotes) > 0 {
		lines = append(lines, fmt.Sprintf("memorx: recent: %q", truncate(ctx.RecentNotes[0].Content, 80)))
	}
	lines = append(lines, "memorx: tip: say \"where did I leave off?\" for full context")
	return strings.Join(lines, "\n")
}

func formatTimeAgo(datetime string) string {
	t, err := time.Parse(time.DateTime, datetime)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, datetime); err != nil {
			return datetime
		}
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
