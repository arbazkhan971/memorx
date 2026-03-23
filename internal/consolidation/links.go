package consolidation

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// DiscoverLinks finds notes with zero outgoing links and creates "related" links
// by searching the FTS index for similar content.
// Returns the total number of links created.
func (e *Engine) DiscoverLinks() (int, error) {
	// Find notes that have no outgoing links
	rows, err := e.db.Reader().Query(
		`SELECT n.id, n.content FROM notes n
		 LEFT JOIN memory_links ml ON ml.source_id = n.id AND ml.source_type = 'note'
		 WHERE ml.id IS NULL`,
	)
	if err != nil {
		return 0, fmt.Errorf("query unlinked notes: %w", err)
	}
	defer rows.Close()

	type unlinkedNote struct {
		id      string
		content string
	}

	var notes []unlinkedNote
	for rows.Next() {
		var n unlinkedNote
		if err := rows.Scan(&n.id, &n.content); err != nil {
			return 0, fmt.Errorf("scan unlinked note: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate unlinked notes: %w", err)
	}

	totalLinks := 0

	for _, note := range notes {
		// Extract meaningful words from the first 100 chars and join with OR for FTS
		query := buildFTSQuery(note.content)
		if query == "" {
			continue
		}

		// Search notes_fts for related content
		matchRows, err := e.db.Reader().Query(
			`SELECT n.id, rank
			 FROM notes_fts fts
			 JOIN notes n ON n.rowid = fts.rowid
			 WHERE notes_fts MATCH ?
			 ORDER BY rank
			 LIMIT 10`,
			query,
		)
		if err != nil {
			// FTS match errors are common with special characters; skip
			continue
		}

		for matchRows.Next() {
			var targetID string
			var rank float64
			if err := matchRows.Scan(&targetID, &rank); err != nil {
				continue
			}

			// Don't link to self
			if targetID == note.id {
				continue
			}

			// Convert BM25 rank to strength (more negative = better match)
			strength := 0.5
			if rank < -2.0 {
				strength = 0.9
			} else if rank < -1.0 {
				strength = 0.7
			}

			// Create forward link
			err := e.createLink(note.id, "note", targetID, "note", "related", strength)
			if err != nil {
				continue // best-effort
			}
			totalLinks++
		}
		matchRows.Close()
	}

	return totalLinks, nil
}

// buildFTSQuery extracts words from content and joins them with OR for FTS5 MATCH.
// Uses the first ~100 chars and filters to alphanumeric words of length >= 3.
func buildFTSQuery(content string) string {
	if len(content) > 100 {
		content = content[:100]
	}

	words := strings.FieldsFunc(content, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var terms []string
	seen := make(map[string]bool)
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) < 3 || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
	}

	if len(terms) == 0 {
		return ""
	}
	// Limit to 10 terms to avoid overly broad queries
	if len(terms) > 10 {
		terms = terms[:10]
	}

	return strings.Join(terms, " OR ")
}

// createLink inserts a single link (not bidirectional, to avoid double-counting).
func (e *Engine) createLink(sourceID, sourceType, targetID, targetType, relationship string, strength float64) error {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)

	_, err := e.db.Writer().Exec(
		`INSERT OR IGNORE INTO memory_links (id, source_id, source_type, target_id, target_type, relationship, strength, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sourceID, sourceType, targetID, targetType, relationship, strength, now,
	)
	if err != nil {
		return fmt.Errorf("create link: %w", err)
	}
	return nil
}
