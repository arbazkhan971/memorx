package consolidation

import (
	"fmt"
	"time"
)

// DetectContradictions finds active facts with the same subject+predicate
// and resolves them by keeping only the most recent one.
// Returns the count of facts invalidated.
func (e *Engine) DetectContradictions() (int, error) {
	// Find conflict groups: same subject+predicate with multiple active facts
	rows, err := e.db.Reader().Query(
		`SELECT subject, predicate, COUNT(*) as cnt
		 FROM facts WHERE invalid_at IS NULL
		 GROUP BY subject, predicate
		 HAVING COUNT(*) > 1`,
	)
	if err != nil {
		return 0, fmt.Errorf("query conflicts: %w", err)
	}
	defer rows.Close()

	type conflictGroup struct {
		subject   string
		predicate string
	}

	var groups []conflictGroup
	for rows.Next() {
		var g conflictGroup
		var cnt int
		if err := rows.Scan(&g.subject, &g.predicate, &cnt); err != nil {
			return 0, fmt.Errorf("scan conflict group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate conflict groups: %w", err)
	}

	totalInvalidated := 0
	now := time.Now().UTC().Format(time.DateTime)

	for _, g := range groups {
		// For each conflict group, find all active facts and keep the most recent
		factRows, err := e.db.Reader().Query(
			`SELECT id, valid_at FROM facts
			 WHERE subject = ? AND predicate = ? AND invalid_at IS NULL
			 ORDER BY valid_at DESC`,
			g.subject, g.predicate,
		)
		if err != nil {
			return totalInvalidated, fmt.Errorf("query facts for conflict: %w", err)
		}

		var factIDs []string
		first := true
		for factRows.Next() {
			var id, validAt string
			if err := factRows.Scan(&id, &validAt); err != nil {
				factRows.Close()
				return totalInvalidated, fmt.Errorf("scan fact: %w", err)
			}
			if first {
				// Keep the most recent (first by ORDER BY valid_at DESC)
				first = false
				continue
			}
			factIDs = append(factIDs, id)
		}
		factRows.Close()

		// Invalidate all but the most recent
		for _, id := range factIDs {
			_, err := e.db.Writer().Exec(
				`UPDATE facts SET invalid_at = ? WHERE id = ?`, now, id,
			)
			if err != nil {
				return totalInvalidated, fmt.Errorf("invalidate fact %s: %w", id, err)
			}
			totalInvalidated++
		}
	}

	return totalInvalidated, nil
}
