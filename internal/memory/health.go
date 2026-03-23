package memory

import (
	"fmt"
	"math"
)

// MemoryHealth represents the health metrics for a feature's memory.
type MemoryHealth struct {
	Score           float64  // 0-100
	TotalMemories   int
	ActiveFacts     int
	StaleFactCount  int // facts that have been invalidated 30+ days ago
	ConflictCount   int // same subject+predicate with multiple active objects
	OrphanNoteCount int // notes with zero links
	StaleNoteCount  int // notes older than 30 days with no links
	SummaryCount    int
	Suggestions     []string // actionable suggestions
}

// GetMemoryHealth computes memory health metrics for a feature.
// If featureID is empty, it computes health across all features.
func (s *Store) GetMemoryHealth(featureID string) (*MemoryHealth, error) {
	h := &MemoryHealth{}
	r := s.db.Reader()

	featureFilter := ""
	var args []any
	if featureID != "" {
		featureFilter = " AND feature_id = ?"
		args = append(args, featureID)
	}

	// Total notes
	var totalNotes int
	err := r.QueryRow(`SELECT COUNT(*) FROM notes WHERE 1=1`+featureFilter, args...).Scan(&totalNotes)
	if err != nil {
		return nil, fmt.Errorf("count notes: %w", err)
	}

	// Total active facts
	err = r.QueryRow(`SELECT COUNT(*) FROM facts WHERE invalid_at IS NULL`+featureFilter, args...).Scan(&h.ActiveFacts)
	if err != nil {
		return nil, fmt.Errorf("count active facts: %w", err)
	}

	h.TotalMemories = totalNotes + h.ActiveFacts

	// Stale facts: invalidated more than 30 days ago
	err = r.QueryRow(
		`SELECT COUNT(*) FROM facts WHERE invalid_at IS NOT NULL AND invalid_at < datetime('now', '-30 days')`+featureFilter,
		args...,
	).Scan(&h.StaleFactCount)
	if err != nil {
		return nil, fmt.Errorf("count stale facts: %w", err)
	}

	// Conflict count: same subject+predicate with multiple active objects
	conflictQuery := `SELECT COUNT(*) FROM (
		SELECT subject, predicate FROM facts WHERE invalid_at IS NULL` + featureFilter + `
		GROUP BY subject, predicate HAVING COUNT(*) > 1
	)`
	err = r.QueryRow(conflictQuery, args...).Scan(&h.ConflictCount)
	if err != nil {
		return nil, fmt.Errorf("count conflicts: %w", err)
	}

	// Orphan notes: notes with zero links (neither source nor target)
	orphanQuery := `SELECT COUNT(*) FROM notes n WHERE 1=1` + featureFilter + `
		AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.source_id = n.id AND ml.source_type = 'note')
		AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.target_id = n.id AND ml.target_type = 'note')`
	// For orphan notes, args need to be repeated for the feature filter in the outer query
	err = r.QueryRow(orphanQuery, args...).Scan(&h.OrphanNoteCount)
	if err != nil {
		return nil, fmt.Errorf("count orphan notes: %w", err)
	}

	// Stale notes: older than 30 days with no links
	staleNoteQuery := `SELECT COUNT(*) FROM notes n WHERE n.created_at < datetime('now', '-30 days')` + featureFilter + `
		AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.source_id = n.id AND ml.source_type = 'note')
		AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.target_id = n.id AND ml.target_type = 'note')`
	err = r.QueryRow(staleNoteQuery, args...).Scan(&h.StaleNoteCount)
	if err != nil {
		return nil, fmt.Errorf("count stale notes: %w", err)
	}

	// Summary count
	summaryQuery := `SELECT COUNT(*) FROM summaries`
	var summaryArgs []any
	if featureID != "" {
		summaryQuery += ` WHERE scope = ?`
		summaryArgs = append(summaryArgs, "feature:"+featureID)
	}
	err = r.QueryRow(summaryQuery, summaryArgs...).Scan(&h.SummaryCount)
	if err != nil {
		return nil, fmt.Errorf("count summaries: %w", err)
	}

	// Calculate score
	score := 100.0
	score -= float64(h.ConflictCount) * 10
	score -= math.Min(float64(h.StaleFactCount), 5) * 5
	score -= math.Min(float64(h.OrphanNoteCount), 10) * 2
	if h.SummaryCount == 0 && h.TotalMemories > 20 {
		score -= 10
	}
	h.Score = math.Max(0, math.Min(100, score))

	// Generate suggestions
	if h.ConflictCount > 0 {
		h.Suggestions = append(h.Suggestions,
			fmt.Sprintf("You have %d contradicting facts. Run consolidation.", h.ConflictCount))
	}
	if h.StaleFactCount > 5 {
		h.Suggestions = append(h.Suggestions,
			fmt.Sprintf("%d facts haven't been referenced in 30+ days. Review with devmem_search.", h.StaleFactCount))
	}
	if h.OrphanNoteCount > 10 {
		h.Suggestions = append(h.Suggestions,
			fmt.Sprintf("%d notes have no connections. Consider consolidation.", h.OrphanNoteCount))
	}
	if h.SummaryCount == 0 && h.TotalMemories > 20 {
		h.Suggestions = append(h.Suggestions,
			"No summaries generated. Memory may be fragmented.")
	}

	return h, nil
}

// ForgetStaleFacts deletes facts that were invalidated more than 30 days ago.
// If featureID is non-empty, scopes to that feature.
func (s *Store) ForgetStaleFacts(featureID string) (int, error) {
	query := `DELETE FROM facts WHERE invalid_at IS NOT NULL AND invalid_at < datetime('now', '-30 days')`
	var args []any
	if featureID != "" {
		query += ` AND feature_id = ?`
		args = append(args, featureID)
	}
	result, err := s.db.Writer().Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("forget stale facts: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ForgetStaleNotes deletes notes older than 60 days with zero links.
// If featureID is non-empty, scopes to that feature.
func (s *Store) ForgetStaleNotes(featureID string) (int, error) {
	featureFilter := ""
	var args []any
	if featureID != "" {
		featureFilter = ` AND n.feature_id = ?`
		args = append(args, featureID)
	}
	query := `DELETE FROM notes WHERE id IN (
		SELECT n.id FROM notes n
		WHERE n.created_at < datetime('now', '-60 days')` + featureFilter + `
		AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.source_id = n.id AND ml.source_type = 'note')
		AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.target_id = n.id AND ml.target_type = 'note')
	)`
	result, err := s.db.Writer().Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("forget stale notes: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ForgetCompletedFeatures deletes all data for features with status='done'
// that have been inactive for more than 90 days.
func (s *Store) ForgetCompletedFeatures() (int, error) {
	// Find completed features older than 90 days
	rows, err := s.db.Reader().Query(
		`SELECT id FROM features WHERE status = 'done' AND last_active < datetime('now', '-90 days')`,
	)
	if err != nil {
		return 0, fmt.Errorf("query completed features: %w", err)
	}
	defer rows.Close()

	var featureIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan feature id: %w", err)
		}
		featureIDs = append(featureIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate features: %w", err)
	}

	w := s.db.Writer()
	deleted := 0
	for _, fid := range featureIDs {
		// Delete in order respecting foreign keys
		tables := []string{
			`DELETE FROM memory_links WHERE source_id IN (SELECT id FROM notes WHERE feature_id = ?) OR target_id IN (SELECT id FROM notes WHERE feature_id = ?)`,
			`DELETE FROM memory_links WHERE source_id IN (SELECT id FROM facts WHERE feature_id = ?) OR target_id IN (SELECT id FROM facts WHERE feature_id = ?)`,
			`DELETE FROM summaries WHERE scope = ?`,
			`DELETE FROM plan_steps WHERE plan_id IN (SELECT id FROM plans WHERE feature_id = ?)`,
			`DELETE FROM plans WHERE feature_id = ?`,
			`DELETE FROM semantic_changes WHERE session_id IN (SELECT id FROM sessions WHERE feature_id = ?)`,
			`DELETE FROM commits WHERE feature_id = ?`,
			`DELETE FROM notes WHERE feature_id = ?`,
			`DELETE FROM facts WHERE feature_id = ?`,
			`DELETE FROM sessions WHERE feature_id = ?`,
			`DELETE FROM features WHERE id = ?`,
		}
		for _, stmt := range tables {
			// Some statements use fid twice or use scope prefix
			if stmt == `DELETE FROM summaries WHERE scope = ?` {
				if _, err := w.Exec(stmt, "feature:"+fid); err != nil {
					return deleted, fmt.Errorf("delete summaries for feature %s: %w", fid, err)
				}
			} else if stmt == `DELETE FROM memory_links WHERE source_id IN (SELECT id FROM notes WHERE feature_id = ?) OR target_id IN (SELECT id FROM notes WHERE feature_id = ?)` ||
				stmt == `DELETE FROM memory_links WHERE source_id IN (SELECT id FROM facts WHERE feature_id = ?) OR target_id IN (SELECT id FROM facts WHERE feature_id = ?)` {
				if _, err := w.Exec(stmt, fid, fid); err != nil {
					return deleted, fmt.Errorf("delete links for feature %s: %w", fid, err)
				}
			} else {
				if _, err := w.Exec(stmt, fid); err != nil {
					return deleted, fmt.Errorf("delete data for feature %s: %w", fid, err)
				}
			}
		}
		deleted++
	}

	return deleted, nil
}

// ForgetByID deletes a specific note or fact by its ID.
func (s *Store) ForgetByID(id string) (string, error) {
	w := s.db.Writer()

	// Try deleting as a note first
	result, err := w.Exec(`DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return "", fmt.Errorf("delete note: %w", err)
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		// Clean up associated links
		w.Exec(`DELETE FROM memory_links WHERE (source_id = ? AND source_type = 'note') OR (target_id = ? AND target_type = 'note')`, id, id) //nolint:errcheck
		return "note", nil
	}

	// Try deleting as a fact
	result, err = w.Exec(`DELETE FROM facts WHERE id = ?`, id)
	if err != nil {
		return "", fmt.Errorf("delete fact: %w", err)
	}
	n, _ = result.RowsAffected()
	if n > 0 {
		// Clean up associated links
		w.Exec(`DELETE FROM memory_links WHERE (source_id = ? AND source_type = 'fact') OR (target_id = ? AND target_type = 'fact')`, id, id) //nolint:errcheck
		return "fact", nil
	}

	return "", fmt.Errorf("no note or fact found with ID %q", id)
}
