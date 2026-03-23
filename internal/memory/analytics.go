package memory

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// FeatureAnalytics contains analytics for a single feature.
type FeatureAnalytics struct {
	Name                 string
	SessionCount         int
	CommitCount          int
	NoteCount            int
	DecisionCount        int
	BlockerCount         int
	FactCount            int
	ActiveFactCount      int
	InvalidatedFactCount int
	PlanProgress         string         // "4/7 (57%)" or "no plan"
	IntentBreakdown      map[string]int // {"feature": 3, "bugfix": 2, ...}
	DaysSinceCreated     int
	DaysSinceLastActive  int
	AvgSessionDuration   string // "45m" or "n/a"
}

// ProjectAnalytics contains project-wide analytics.
type ProjectAnalytics struct {
	TotalFeatures      int
	ActiveFeatures     int
	PausedFeatures     int
	DoneFeatures       int
	TotalSessions      int
	TotalCommits       int
	TotalNotes         int
	TotalFacts         int
	MostActiveFeature  string
	MostBlockedFeature string   // feature with most blockers
	RecentActivity     []string // last 5 events across all features
}

// GetFeatureAnalytics returns analytics for a specific feature by ID.
func (s *Store) GetFeatureAnalytics(featureID string) (*FeatureAnalytics, error) {
	r := s.db.Reader()

	// Load the feature
	f, err := scanFeature(r.QueryRow("SELECT "+featureCols+" FROM features WHERE id = ?", featureID))
	if err != nil {
		return nil, fmt.Errorf("feature not found: %w", err)
	}

	a := &FeatureAnalytics{
		Name:            f.Name,
		IntentBreakdown: make(map[string]int),
	}

	// Counts
	r.QueryRow(`SELECT COUNT(*) FROM sessions WHERE feature_id = ?`, featureID).Scan(&a.SessionCount)
	r.QueryRow(`SELECT COUNT(*) FROM commits WHERE feature_id = ?`, featureID).Scan(&a.CommitCount)
	r.QueryRow(`SELECT COUNT(*) FROM notes WHERE feature_id = ?`, featureID).Scan(&a.NoteCount)
	r.QueryRow(`SELECT COUNT(*) FROM notes WHERE feature_id = ? AND type = 'decision'`, featureID).Scan(&a.DecisionCount)
	r.QueryRow(`SELECT COUNT(*) FROM notes WHERE feature_id = ? AND type = 'blocker'`, featureID).Scan(&a.BlockerCount)
	r.QueryRow(`SELECT COUNT(*) FROM facts WHERE feature_id = ?`, featureID).Scan(&a.FactCount)
	r.QueryRow(`SELECT COUNT(*) FROM facts WHERE feature_id = ? AND invalid_at IS NULL`, featureID).Scan(&a.ActiveFactCount)
	a.InvalidatedFactCount = a.FactCount - a.ActiveFactCount

	// Plan progress
	a.PlanProgress = s.loadPlanProgress(r, featureID)

	// Intent breakdown from commits
	rows, err := r.Query(
		`SELECT COALESCE(intent_type, 'unknown'), COUNT(*) FROM commits WHERE feature_id = ? GROUP BY intent_type`,
		featureID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var intentType string
			var count int
			if rows.Scan(&intentType, &count) == nil {
				a.IntentBreakdown[intentType] = count
			}
		}
	}

	// Days since created / last active
	now := time.Now().UTC()
	if created, err := time.Parse(time.DateTime, f.CreatedAt); err == nil {
		a.DaysSinceCreated = int(math.Floor(now.Sub(created).Hours() / 24))
	}
	if lastActive, err := time.Parse(time.DateTime, f.LastActive); err == nil {
		a.DaysSinceLastActive = int(math.Floor(now.Sub(lastActive).Hours() / 24))
	}

	// Avg session duration
	a.AvgSessionDuration = s.loadAvgSessionDuration(r, featureID)

	return a, nil
}

// GetProjectAnalytics returns project-wide analytics across all features.
func (s *Store) GetProjectAnalytics() (*ProjectAnalytics, error) {
	r := s.db.Reader()
	a := &ProjectAnalytics{}

	r.QueryRow(`SELECT COUNT(*) FROM features`).Scan(&a.TotalFeatures)
	r.QueryRow(`SELECT COUNT(*) FROM features WHERE status = 'active'`).Scan(&a.ActiveFeatures)
	r.QueryRow(`SELECT COUNT(*) FROM features WHERE status = 'paused'`).Scan(&a.PausedFeatures)
	r.QueryRow(`SELECT COUNT(*) FROM features WHERE status = 'done'`).Scan(&a.DoneFeatures)
	r.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&a.TotalSessions)
	r.QueryRow(`SELECT COUNT(*) FROM commits`).Scan(&a.TotalCommits)
	r.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&a.TotalNotes)
	r.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&a.TotalFacts)

	// Most active feature = feature with most sessions
	var mostActiveID sql.NullString
	r.QueryRow(
		`SELECT feature_id FROM sessions GROUP BY feature_id ORDER BY COUNT(*) DESC LIMIT 1`,
	).Scan(&mostActiveID)
	if mostActiveID.Valid {
		r.QueryRow(`SELECT name FROM features WHERE id = ?`, mostActiveID.String).Scan(&a.MostActiveFeature)
	}

	// Most blocked feature = feature with most blocker notes
	var mostBlockedID sql.NullString
	r.QueryRow(
		`SELECT feature_id FROM notes WHERE type = 'blocker' GROUP BY feature_id ORDER BY COUNT(*) DESC LIMIT 1`,
	).Scan(&mostBlockedID)
	if mostBlockedID.Valid {
		r.QueryRow(`SELECT name FROM features WHERE id = ?`, mostBlockedID.String).Scan(&a.MostBlockedFeature)
	}

	// Recent activity: last 5 events (notes + commits) across all features
	a.RecentActivity = s.loadRecentActivity(r)

	return a, nil
}

// loadPlanProgress returns plan progress as a string like "4/7 (57%)" or "no plan".
func (s *Store) loadPlanProgress(r *sql.DB, featureID string) string {
	var planID string
	err := r.QueryRow(
		`SELECT id FROM plans WHERE feature_id = ? AND status = 'active' AND invalid_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		featureID,
	).Scan(&planID)
	if err != nil {
		return "no plan"
	}

	var total, completed int
	r.QueryRow(`SELECT COUNT(*) FROM plan_steps WHERE plan_id = ?`, planID).Scan(&total)
	r.QueryRow(`SELECT COUNT(*) FROM plan_steps WHERE plan_id = ? AND status = 'completed'`, planID).Scan(&completed)

	if total == 0 {
		return "0/0 (0%)"
	}
	pct := completed * 100 / total
	return fmt.Sprintf("%d/%d (%d%%)", completed, total, pct)
}

// loadAvgSessionDuration computes the average duration of ended sessions.
func (s *Store) loadAvgSessionDuration(r *sql.DB, featureID string) string {
	rows, err := r.Query(
		`SELECT started_at, ended_at FROM sessions WHERE feature_id = ? AND ended_at IS NOT NULL`,
		featureID,
	)
	if err != nil {
		return "n/a"
	}
	defer rows.Close()

	var totalDuration time.Duration
	var count int
	for rows.Next() {
		var startStr, endStr string
		if rows.Scan(&startStr, &endStr) != nil {
			continue
		}
		start, err1 := time.Parse(time.DateTime, startStr)
		end, err2 := time.Parse(time.DateTime, endStr)
		if err1 != nil || err2 != nil {
			continue
		}
		totalDuration += end.Sub(start)
		count++
	}

	if count == 0 {
		return "n/a"
	}

	avg := totalDuration / time.Duration(count)
	return formatDuration(avg)
}

// formatDuration returns a human-readable duration string like "2h30m" or "45m".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// loadRecentActivity returns the last 5 events (notes and commits) across all features.
func (s *Store) loadRecentActivity(r *sql.DB) []string {
	rows, err := r.Query(`
		SELECT event_type, content, feature_name, created_at FROM (
			SELECT 'note' AS event_type, n.content, f.name AS feature_name, n.created_at
			FROM notes n JOIN features f ON n.feature_id = f.id
			UNION ALL
			SELECT 'commit' AS event_type, c.message AS content, f.name AS feature_name, c.committed_at AS created_at
			FROM commits c JOIN features f ON c.feature_id = f.id
		) events
		ORDER BY created_at DESC
		LIMIT 5
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var activity []string
	for rows.Next() {
		var eventType, content, featureName, createdAt string
		if rows.Scan(&eventType, &content, &featureName, &createdAt) != nil {
			continue
		}
		// Truncate content for readability
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		activity = append(activity, fmt.Sprintf("[%s] %s — %s (%s)", eventType, content, featureName, createdAt))
	}
	return activity
}
