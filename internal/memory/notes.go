package memory

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Note struct{ ID, FeatureID, SessionID, Content, Type, CreatedAt, UpdatedAt string }
type MemoryLink struct {
	ID, SourceID, SourceType, TargetID, TargetType, Relationship string
	Strength                                                     float64
	CreatedAt                                                    string
}

const noteCols = `id, feature_id, COALESCE(session_id, ''), content, type, created_at, updated_at`

func scanNote(sc interface{ Scan(...any) error }) (Note, error) {
	var n Note
	return n, sc.Scan(&n.ID, &n.FeatureID, &n.SessionID, &n.Content, &n.Type, &n.CreatedAt, &n.UpdatedAt)
}
func (s *Store) CreateNote(featureID, sessionID, content, noteType string) (*Note, error) {
	if noteType == "" {
		noteType = "note"
	}
	// Strip <private>...</private> blocks at the store boundary so every
	// capture path (hooks, MCP, import) gets the same privacy guarantee.
	content = StripPrivate(content)
	if content == "" {
		return nil, fmt.Errorf("note empty after privacy stripping")
	}
	id, now := uuid.New().String(), time.Now().UTC().Format(time.DateTime)
	w := s.db.Writer()
	if _, err := w.Exec(`INSERT INTO notes (id, feature_id, session_id, content, type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, featureID, nullIfEmpty(sessionID), content, noteType, now, now); err != nil {
		return nil, fmt.Errorf("create note: %w", err)
	}
	var rowID int64
	if err := w.QueryRow(`SELECT rowid FROM notes WHERE id = ?`, id).Scan(&rowID); err != nil {
		return nil, fmt.Errorf("get note rowid: %w", err)
	}
	if _, err := w.Exec(`INSERT INTO notes_fts(rowid, content, type) VALUES (?, ?, ?)`, rowID, content, noteType); err != nil {
		return nil, fmt.Errorf("sync note to fts: %w", err)
	}
	if _, err := w.Exec(`INSERT INTO notes_trigram(rowid, content) VALUES (?, ?)`, rowID, content); err != nil {
		return nil, fmt.Errorf("sync note to trigram: %w", err)
	}
	return &Note{ID: id, FeatureID: featureID, SessionID: sessionID, Content: content, Type: noteType, CreatedAt: now, UpdatedAt: now}, nil
}
func (s *Store) ListNotes(featureID, noteType string, limit int) ([]Note, error) {
	if limit <= 0 {
		limit = 50
	}
	q, args := `SELECT `+noteCols+` FROM notes WHERE feature_id = ?`, []any{featureID}
	if noteType != "" {
		q += ` AND type = ?`
		args = append(args, noteType)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	return collectRows(s.db.Reader(), q, args, func(rows *sql.Rows) (Note, error) { return scanNote(rows) })
}
func (s *Store) GetNote(noteID string) (*Note, error) {
	n, err := scanNote(s.db.Reader().QueryRow(`SELECT `+noteCols+` FROM notes WHERE id = ?`, noteID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("note %q not found", noteID)
	}
	if err != nil {
		return nil, fmt.Errorf("get note: %w", err)
	}
	return &n, nil
}
func collectRows[T any](r *sql.DB, query string, args []any, fn func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		if v, err := fn(rows); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		} else {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}
func (s *Store) CreateLink(sourceID, sourceType, targetID, targetType, relationship string, strength float64) error {
	now := time.Now().UTC().Format(time.DateTime)
	w := s.db.Writer()
	for _, d := range [2][4]string{{sourceID, sourceType, targetID, targetType}, {targetID, targetType, sourceID, sourceType}} {
		if _, err := w.Exec(`INSERT OR IGNORE INTO memory_links (id, source_id, source_type, target_id, target_type, relationship, strength, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uuid.New().String(), d[0], d[1], d[2], d[3], relationship, strength, now); err != nil {
			return fmt.Errorf("create link %s->%s: %w", d[0], d[2], err)
		}
	}
	return nil
}
func (s *Store) GetLinks(memoryID, memoryType string) ([]MemoryLink, error) {
	rows, err := s.db.Reader().Query(`SELECT id, source_id, source_type, target_id, target_type, relationship, strength, created_at FROM memory_links WHERE source_id = ? AND source_type = ? ORDER BY strength DESC, created_at DESC`, memoryID, memoryType)
	if err != nil {
		return nil, fmt.Errorf("get links: %w", err)
	}
	defer rows.Close()
	var out []MemoryLink
	for rows.Next() {
		var l MemoryLink
		if err := rows.Scan(&l.ID, &l.SourceID, &l.SourceType, &l.TargetID, &l.TargetType, &l.Relationship, &l.Strength, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *Store) AutoLink(sourceID, sourceType, content string) (int, error) {
	count := 0
	for _, t := range [3][2]string{{"note", "notes"}, {"fact", "facts"}, {"commit", "commits"}} {
		rows, err := s.db.Reader().Query(fmt.Sprintf(`SELECT t.id, '%s' as type, rank FROM %s_fts fts JOIN %s t ON t.rowid = fts.rowid WHERE %s_fts MATCH ? AND rank < -0.5 ORDER BY rank LIMIT 10`, t[0], t[1], t[1], t[1]), content)
		if err != nil {
			continue
		}
		for rows.Next() {
			var targetID, targetType string
			var rank float64
			if rows.Scan(&targetID, &targetType, &rank) != nil {
				continue
			}
			if targetID == sourceID && targetType == sourceType {
				continue
			}
			strength := 0.5
			if rank < -2.0 {
				strength = 0.9
			} else if rank < -1.0 {
				strength = 0.7
			}
			if s.CreateLink(sourceID, sourceType, targetID, targetType, "related", strength) == nil {
				count++
			}
		}
		rows.Close()
	}
	return count, nil
}
