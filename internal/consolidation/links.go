package consolidation

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// DiscoverLinks finds notes with no outgoing links and creates "related" links via FTS.
func (e *Engine) DiscoverLinks() (int, error) {
	rows, err := e.db.Reader().Query(
		`SELECT n.id, n.content FROM notes n LEFT JOIN memory_links ml ON ml.source_id=n.id AND ml.source_type='note' WHERE ml.id IS NULL`,
	)
	if err != nil {
		return 0, fmt.Errorf("query unlinked notes: %w", err)
	}
	defer rows.Close()

	type unlinkedNote struct{ id, content string }
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
		query := buildFTSQuery(note.content)
		if query == "" {
			continue
		}
		matchRows, err := e.db.Reader().Query(
			`SELECT n.id, rank FROM notes_fts fts JOIN notes n ON n.rowid=fts.rowid WHERE notes_fts MATCH ? ORDER BY rank LIMIT 10`, query,
		)
		if err != nil {
			continue
		}
		for matchRows.Next() {
			var targetID string
			var rank float64
			if err := matchRows.Scan(&targetID, &rank); err != nil {
				continue
			}
			if targetID == note.id {
				continue
			}
			strength := 0.5
			if rank < -2.0 {
				strength = 0.9
			} else if rank < -1.0 {
				strength = 0.7
			}
			if err := e.createLink(note.id, "note", targetID, "note", "related", strength); err == nil {
				totalLinks++
			}
		}
		matchRows.Close()
	}
	return totalLinks, nil
}

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
	if len(terms) > 10 {
		terms = terms[:10]
	}
	return strings.Join(terms, " OR ")
}

func (e *Engine) createLink(sourceID, sourceType, targetID, targetType, relationship string, strength float64) error {
	_, err := e.db.Writer().Exec(
		`INSERT OR IGNORE INTO memory_links (id, source_id, source_type, target_id, target_type, relationship, strength, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New().String(), sourceID, sourceType, targetID, targetType, relationship, strength, time.Now().UTC().Format(time.DateTime),
	)
	if err != nil {
		return fmt.Errorf("create link: %w", err)
	}
	return nil
}
