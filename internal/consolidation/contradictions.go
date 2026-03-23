package consolidation

import (
	"fmt"
	"time"
)

// Shared SQL for counting conflict groups (used by both countConflicts and DetectContradictions).
const conflictGroupsSQL = `SELECT COUNT(*) FROM (SELECT subject, predicate FROM facts WHERE invalid_at IS NULL GROUP BY subject, predicate HAVING COUNT(*)>1)`

// DetectContradictions resolves facts with duplicate subject+predicate by keeping only the most recent.
func (e *Engine) DetectContradictions() (int, error) {
	rows, err := e.db.Reader().Query(
		`SELECT subject, predicate FROM facts WHERE invalid_at IS NULL GROUP BY subject, predicate HAVING COUNT(*)>1`,
	)
	if err != nil {
		return 0, fmt.Errorf("query conflicts: %w", err)
	}
	defer rows.Close()

	type conflictGroup struct{ subject, predicate string }
	var groups []conflictGroup
	for rows.Next() {
		var g conflictGroup
		if err := rows.Scan(&g.subject, &g.predicate); err != nil {
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
		factRows, err := e.db.Reader().Query(
			`SELECT id FROM facts WHERE subject=? AND predicate=? AND invalid_at IS NULL ORDER BY valid_at DESC`,
			g.subject, g.predicate,
		)
		if err != nil {
			return totalInvalidated, fmt.Errorf("query facts for conflict: %w", err)
		}

		var factIDs []string
		first := true
		for factRows.Next() {
			var id string
			if err := factRows.Scan(&id); err != nil {
				factRows.Close()
				return totalInvalidated, fmt.Errorf("scan fact: %w", err)
			}
			if first {
				first = false
				continue
			}
			factIDs = append(factIDs, id)
		}
		factRows.Close()

		for _, id := range factIDs {
			if _, err := e.db.Writer().Exec(`UPDATE facts SET invalid_at=? WHERE id=?`, now, id); err != nil {
				return totalInvalidated, fmt.Errorf("invalidate fact %s: %w", id, err)
			}
			totalInvalidated++
		}
	}
	return totalInvalidated, nil
}
