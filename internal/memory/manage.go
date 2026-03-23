package memory

import (
	"database/sql"
	"fmt"
)

// MemoryItem is a union type representing either a note or a fact.
type MemoryItem struct {
	ID, Type, Content, CreatedAt string
	Pinned                       bool
}

// MemoryFilter controls which memories are returned by ListMemories.
type MemoryFilter struct {
	Type   string // "notes", "facts", or "" for all
	Pinned *bool  // nil = all, true = pinned only, false = unpinned only
	Limit  int
}

// PinMemory marks a note or fact as pinned so it always appears in context.
func (s *Store) PinMemory(id, memType string) error {
	return s.setPinned(id, memType, true)
}

// UnpinMemory clears the pinned flag on a note or fact.
func (s *Store) UnpinMemory(id, memType string) error {
	return s.setPinned(id, memType, false)
}

func (s *Store) setPinned(id, memType string, pinned bool) error {
	table, err := memTypeTable(memType)
	if err != nil {
		return err
	}
	val := 0
	if pinned {
		val = 1
	}
	result, err := s.db.Writer().Exec(fmt.Sprintf(`UPDATE %s SET pinned = ? WHERE id = ?`, table), val, id)
	if err != nil {
		return fmt.Errorf("set pinned on %s: %w", memType, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("%s %q not found", memType, id)
	}
	return nil
}

// ListMemories queries both notes and facts tables, merges results, and sorts by created_at DESC.
func (s *Store) ListMemories(featureID string, filter MemoryFilter) ([]MemoryItem, error) {
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	r := s.db.Reader()
	var items []MemoryItem

	if filter.Type == "" || filter.Type == "notes" {
		notes, err := s.queryMemoryItems(r, "note", "notes", "content", "created_at", featureID, filter)
		if err != nil {
			return nil, fmt.Errorf("list notes: %w", err)
		}
		items = append(items, notes...)
	}

	if filter.Type == "" || filter.Type == "facts" {
		facts, err := s.queryMemoryItems(r, "fact", "facts", "subject || ' ' || predicate || ' ' || object", "recorded_at", featureID, filter)
		if err != nil {
			return nil, fmt.Errorf("list facts: %w", err)
		}
		items = append(items, facts...)
	}

	// Sort by created_at DESC (simple insertion sort since slices are already sorted)
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].CreatedAt > items[j-1].CreatedAt; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *Store) queryMemoryItems(r *sql.DB, typeName, table, contentExpr, timeCol, featureID string, filter MemoryFilter) ([]MemoryItem, error) {
	q := fmt.Sprintf(`SELECT id, %s, %s, COALESCE(pinned, 0) FROM %s WHERE feature_id = ?`, contentExpr, timeCol, table)
	args := []any{featureID}

	// For facts, only include active facts (not invalidated).
	if table == "facts" {
		q += ` AND invalid_at IS NULL`
	}

	if filter.Pinned != nil {
		if *filter.Pinned {
			q += ` AND pinned = 1`
		} else {
			q += ` AND (pinned = 0 OR pinned IS NULL)`
		}
	}

	q += fmt.Sprintf(` ORDER BY %s DESC LIMIT ?`, timeCol)
	args = append(args, filter.Limit)

	rows, err := r.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []MemoryItem
	for rows.Next() {
		var item MemoryItem
		var pinnedInt int
		if err := rows.Scan(&item.ID, &item.Content, &item.CreatedAt, &pinnedInt); err != nil {
			return nil, err
		}
		item.Type = typeName
		item.Pinned = pinnedInt == 1
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetPinnedMemories returns all pinned notes and facts for a feature.
func (s *Store) GetPinnedMemories(featureID string) ([]MemoryItem, error) {
	pinned := true
	return s.ListMemories(featureID, MemoryFilter{Pinned: &pinned, Limit: 100})
}

func memTypeTable(memType string) (string, error) {
	switch memType {
	case "note":
		return "notes", nil
	case "fact":
		return "facts", nil
	default:
		return "", fmt.Errorf("invalid memory type %q (must be 'note' or 'fact')", memType)
	}
}
