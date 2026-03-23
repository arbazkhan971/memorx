package memory

import (
	"fmt"
	"strings"
	"time"
)

// TimelineEvent represents a single event in a project timeline.
type TimelineEvent struct {
	Timestamp string // formatted datetime
	Type      string // session, decision, commit, progress, blocker, fact, note
	Content   string
	Feature   string
}

// GetTimeline returns chronological events for the given time range, optionally filtered by feature name.
func (s *Store) GetTimeline(days int, featureName string) ([]TimelineEvent, error) {
	if days <= 0 {
		days = 30
	}
	r := s.db.Reader()
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.DateTime)

	var featureFilter string
	var featureArgs []any
	if featureName != "" {
		// Resolve feature name to ID
		var featureID string
		err := r.QueryRow(`SELECT id FROM features WHERE name = ?`, featureName).Scan(&featureID)
		if err != nil {
			return nil, fmt.Errorf("feature %q not found", featureName)
		}
		featureFilter = ` AND feature_id = ?`
		featureArgs = []any{featureID}
	}

	var events []TimelineEvent

	// Sessions (started)
	args := append([]any{since}, featureArgs...)
	rows, err := r.Query(`
		SELECT s.started_at, 'session', s.tool || ' started', f.name
		FROM sessions s
		JOIN features f ON s.feature_id = f.id
		WHERE s.started_at >= ?`+featureFilter+`
		ORDER BY s.started_at ASC
	`, args...)
	if err == nil {
		events = append(events, scanTimelineRows(rows)...)
	}

	// Sessions (ended with summary)
	rows, err = r.Query(`
		SELECT s.ended_at, 'session', 'ended — ' || COALESCE(s.summary, ''), f.name
		FROM sessions s
		JOIN features f ON s.feature_id = f.id
		WHERE s.ended_at IS NOT NULL AND s.ended_at >= ?`+featureFilter+`
		ORDER BY s.ended_at ASC
	`, args...)
	if err == nil {
		events = append(events, scanTimelineRows(rows)...)
	}

	// Notes (decisions, blockers, progress, etc.)
	rows, err = r.Query(`
		SELECT n.created_at, n.type, n.content, f.name
		FROM notes n
		JOIN features f ON n.feature_id = f.id
		WHERE n.created_at >= ?`+featureFilter+`
		ORDER BY n.created_at ASC
	`, args...)
	if err == nil {
		events = append(events, scanTimelineRows(rows)...)
	}

	// Commits
	rows, err = r.Query(`
		SELECT c.committed_at, 'commit', c.message, f.name
		FROM commits c
		JOIN features f ON c.feature_id = f.id
		WHERE c.committed_at >= ?`+featureFilter+`
		ORDER BY c.committed_at ASC
	`, args...)
	if err == nil {
		events = append(events, scanTimelineRows(rows)...)
	}

	// Facts
	rows, err = r.Query(`
		SELECT fa.recorded_at, 'fact', fa.subject || ' ' || fa.predicate || ' ' || fa.object, f.name
		FROM facts fa
		JOIN features f ON fa.feature_id = f.id
		WHERE fa.recorded_at >= ?`+featureFilter+`
		ORDER BY fa.recorded_at ASC
	`, args...)
	if err == nil {
		events = append(events, scanTimelineRows(rows)...)
	}

	// Sort all events chronologically
	sortTimelineEvents(events)

	return events, nil
}

func scanTimelineRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
}) []TimelineEvent {
	defer rows.Close()
	var out []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		if rows.Scan(&e.Timestamp, &e.Type, &e.Content, &e.Feature) != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

func sortTimelineEvents(events []TimelineEvent) {
	// Simple insertion sort (usually a few hundred events max)
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].Timestamp < events[j-1].Timestamp; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}

// FormatTimeline renders events as a chronological timeline string.
func FormatTimeline(events []TimelineEvent) string {
	if len(events) == 0 {
		return "No events found in the specified time range."
	}
	var b strings.Builder
	for _, e := range events {
		ts := formatTimelineTimestamp(e.Timestamp)
		content := e.Content
		if len(content) > 120 {
			content = content[:120] + "..."
		}
		// Replace newlines in content for single-line display
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Fprintf(&b, "%s [%s] %s", ts, e.Type, content)
		if e.Feature != "" {
			fmt.Fprintf(&b, " (%s)", e.Feature)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func formatTimelineTimestamp(ts string) string {
	t, err := time.Parse(time.DateTime, ts)
	if err != nil {
		return ts
	}
	return t.Format("Jan 02 15:04")
}
