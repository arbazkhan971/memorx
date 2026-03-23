package consolidation

import "fmt"

// ApplyDecay is a V1 placeholder for memory decay.
// It counts notes older than 30 days that have no outgoing links (stale memories).
// It does not delete them yet — just returns the count.
func (e *Engine) ApplyDecay() (int, error) {
	var count int
	err := e.db.Reader().QueryRow(
		`SELECT COUNT(*) FROM notes n
		 WHERE n.created_at < datetime('now', '-30 days')
		 AND NOT EXISTS (
			SELECT 1 FROM memory_links ml
			WHERE ml.source_id = n.id AND ml.source_type = 'note'
		 )`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count stale notes: %w", err)
	}
	return count, nil
}
