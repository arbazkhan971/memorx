package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// MemoryDiff captures what changed in memory since a given point in time.
type MemoryDiff struct {
	NewFacts         []Fact
	InvalidatedFacts []Fact
	NewNotes         []Note
	NewCommits       int
	PlanDelta        string // e.g. "3/7 -> 5/7 (+2 steps)"
	NewLinks         int
	NewFiles         []string
	SessionsSince    int
}

// GetDiff returns a MemoryDiff for a feature since the given time.
func (s *Store) GetDiff(featureID string, since time.Time) (*MemoryDiff, error) {
	r := s.db.Reader()
	ts := since.UTC().Format(time.DateTime)
	diff := &MemoryDiff{}

	// New facts: recorded_at >= since and still active
	newFacts, err := collectRows(r,
		`SELECT `+factColumns+` FROM facts WHERE feature_id = ? AND recorded_at >= ? AND invalid_at IS NULL ORDER BY recorded_at DESC`,
		[]any{featureID, ts},
		func(rows *sql.Rows) (Fact, error) { return scanFact(rows) },
	)
	if err != nil {
		return nil, fmt.Errorf("query new facts: %w", err)
	}
	diff.NewFacts = newFacts

	// Invalidated facts: invalid_at >= since (facts that were invalidated since the given time)
	invalidated, err := collectRows(r,
		`SELECT `+factColumns+` FROM facts WHERE feature_id = ? AND invalid_at IS NOT NULL AND invalid_at >= ? ORDER BY invalid_at DESC`,
		[]any{featureID, ts},
		func(rows *sql.Rows) (Fact, error) { return scanFact(rows) },
	)
	if err != nil {
		return nil, fmt.Errorf("query invalidated facts: %w", err)
	}
	diff.InvalidatedFacts = invalidated

	// New notes: created_at >= since
	newNotes, err := collectRows(r,
		`SELECT `+noteCols+` FROM notes WHERE feature_id = ? AND created_at >= ? ORDER BY created_at DESC`,
		[]any{featureID, ts},
		func(rows *sql.Rows) (Note, error) { return scanNote(rows) },
	)
	if err != nil {
		return nil, fmt.Errorf("query new notes: %w", err)
	}
	diff.NewNotes = newNotes

	// New commits: committed_at >= since
	diff.NewCommits = countRows(r, `SELECT COUNT(*) FROM commits WHERE feature_id = ? AND committed_at >= ?`, featureID, ts)

	// Plan delta: compare current plan progress with the state at `since`
	diff.PlanDelta = s.computePlanDelta(r, featureID, ts)

	// New links: created_at >= since, scoped to this feature's memories
	diff.NewLinks = countRows(r,
		`SELECT COUNT(*) FROM memory_links WHERE created_at >= ? AND (
			source_id IN (SELECT id FROM notes WHERE feature_id = ? UNION SELECT id FROM facts WHERE feature_id = ? UNION SELECT id FROM commits WHERE feature_id = ?)
			OR target_id IN (SELECT id FROM notes WHERE feature_id = ? UNION SELECT id FROM facts WHERE feature_id = ? UNION SELECT id FROM commits WHERE feature_id = ?)
		)`,
		ts, featureID, featureID, featureID, featureID, featureID, featureID,
	)

	// New files: first_seen >= since
	newFiles, err := collectRows(r,
		`SELECT DISTINCT path FROM files_touched WHERE feature_id = ? AND first_seen >= ? ORDER BY first_seen DESC`,
		[]any{featureID, ts},
		func(rows *sql.Rows) (string, error) {
			var p string
			return p, rows.Scan(&p)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query new files: %w", err)
	}
	diff.NewFiles = newFiles

	// Sessions since: started_at >= since
	diff.SessionsSince = countRows(r, `SELECT COUNT(*) FROM sessions WHERE feature_id = ? AND started_at >= ?`, featureID, ts)

	return diff, nil
}

// GetLastSessionEndTime returns the end time of the most recent completed session for a feature.
// If no completed session is found, returns zero time.
func (s *Store) GetLastSessionEndTime(featureID string) (time.Time, error) {
	var endedAt string
	err := s.db.Reader().QueryRow(
		`SELECT ended_at FROM sessions WHERE feature_id = ? AND ended_at IS NOT NULL ORDER BY ended_at DESC LIMIT 1`,
		featureID,
	).Scan(&endedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("get last session end: %w", err)
	}
	t, err := time.Parse(time.DateTime, endedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse session end time: %w", err)
	}
	return t, nil
}

// computePlanDelta calculates the plan progress change since a given timestamp.
func (s *Store) computePlanDelta(r *sql.DB, featureID, sinceTS string) string {
	var planID string
	if r.QueryRow(`SELECT id FROM plans WHERE feature_id = ? AND status = 'active' AND invalid_at IS NULL ORDER BY created_at DESC LIMIT 1`, featureID).Scan(&planID) != nil {
		return "no plan"
	}

	totalSteps := countRows(r, `SELECT COUNT(*) FROM plan_steps WHERE plan_id = ?`, planID)
	completedNow := countRows(r, `SELECT COUNT(*) FROM plan_steps WHERE plan_id = ? AND status = 'completed'`, planID)
	completedSince := countRows(r, `SELECT COUNT(*) FROM plan_steps WHERE plan_id = ? AND status = 'completed' AND completed_at >= ?`, planID, sinceTS)

	completedBefore := completedNow - completedSince
	if completedSince == 0 {
		return fmt.Sprintf("%d/%d (no change)", completedNow, totalSteps)
	}
	return fmt.Sprintf("%d/%d -> %d/%d (+%d steps)", completedBefore, totalSteps, completedNow, totalSteps, completedSince)
}
