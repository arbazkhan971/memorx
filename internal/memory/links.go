package memory

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MemoryLink represents a bidirectional link between memory items.
type MemoryLink struct {
	ID           string
	SourceID     string
	SourceType   string
	TargetID     string
	TargetType   string
	Relationship string
	Strength     float64
	CreatedAt    string
}

// CreateLink creates a bidirectional link between two memory items.
// Both directions are inserted (source->target and target->source).
func (s *Store) CreateLink(sourceID, sourceType, targetID, targetType, relationship string, strength float64) error {
	now := time.Now().UTC().Format(time.DateTime)
	w := s.db.Writer()

	// Forward direction
	id1 := uuid.New().String()
	_, err := w.Exec(
		`INSERT OR IGNORE INTO memory_links (id, source_id, source_type, target_id, target_type, relationship, strength, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id1, sourceID, sourceType, targetID, targetType, relationship, strength, now,
	)
	if err != nil {
		return fmt.Errorf("create forward link: %w", err)
	}

	// Reverse direction
	id2 := uuid.New().String()
	_, err = w.Exec(
		`INSERT OR IGNORE INTO memory_links (id, source_id, source_type, target_id, target_type, relationship, strength, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id2, targetID, targetType, sourceID, sourceType, relationship, strength, now,
	)
	if err != nil {
		return fmt.Errorf("create reverse link: %w", err)
	}

	return nil
}

// GetLinks returns all links where the given memory item is the source.
func (s *Store) GetLinks(memoryID, memoryType string) ([]MemoryLink, error) {
	rows, err := s.db.Reader().Query(
		`SELECT id, source_id, source_type, target_id, target_type, relationship, strength, created_at
		 FROM memory_links WHERE source_id = ? AND source_type = ?
		 ORDER BY strength DESC, created_at DESC`,
		memoryID, memoryType,
	)
	if err != nil {
		return nil, fmt.Errorf("get links: %w", err)
	}
	defer rows.Close()

	var links []MemoryLink
	for rows.Next() {
		var l MemoryLink
		if err := rows.Scan(&l.ID, &l.SourceID, &l.SourceType, &l.TargetID, &l.TargetType,
			&l.Relationship, &l.Strength, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// AutoLink searches FTS indexes for content and creates "related" links
// for results above a BM25 rank threshold. Returns the count of links created.
func (s *Store) AutoLink(sourceID, sourceType, content string) (int, error) {
	count := 0

	// Search notes_fts
	noteLinks, err := s.searchAndLink(sourceID, sourceType, content,
		`SELECT n.id, 'note' as type, rank
		 FROM notes_fts fts
		 JOIN notes n ON n.rowid = fts.rowid
		 WHERE notes_fts MATCH ? AND rank < -0.5
		 ORDER BY rank LIMIT 10`,
	)
	if err != nil {
		return 0, fmt.Errorf("autolink notes: %w", err)
	}
	count += noteLinks

	// Search facts_fts
	factLinks, err := s.searchAndLink(sourceID, sourceType, content,
		`SELECT f.id, 'fact' as type, rank
		 FROM facts_fts fts
		 JOIN facts f ON f.rowid = fts.rowid
		 WHERE facts_fts MATCH ? AND rank < -0.5
		 ORDER BY rank LIMIT 10`,
	)
	if err != nil {
		return count, fmt.Errorf("autolink facts: %w", err)
	}
	count += factLinks

	// Search commits_fts
	commitLinks, err := s.searchAndLink(sourceID, sourceType, content,
		`SELECT c.id, 'commit' as type, rank
		 FROM commits_fts fts
		 JOIN commits c ON c.rowid = fts.rowid
		 WHERE commits_fts MATCH ? AND rank < -0.5
		 ORDER BY rank LIMIT 10`,
	)
	if err != nil {
		return count, fmt.Errorf("autolink commits: %w", err)
	}
	count += commitLinks

	return count, nil
}

// searchAndLink performs an FTS search and creates links for matching results.
func (s *Store) searchAndLink(sourceID, sourceType, content, query string) (int, error) {
	rows, err := s.db.Reader().Query(query, content)
	if err != nil {
		// FTS match errors are common with special characters; skip silently
		return 0, nil
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var targetID, targetType string
		var rank float64
		if err := rows.Scan(&targetID, &targetType, &rank); err != nil {
			continue
		}
		// Don't link to self
		if targetID == sourceID && targetType == sourceType {
			continue
		}
		// Convert rank to strength (BM25 returns negative values, more negative = better match)
		strength := 0.5
		if rank < -2.0 {
			strength = 0.9
		} else if rank < -1.0 {
			strength = 0.7
		}

		if err := s.CreateLink(sourceID, sourceType, targetID, targetType, "related", strength); err != nil {
			continue // best-effort linking
		}
		count++
	}
	return count, rows.Err()
}
