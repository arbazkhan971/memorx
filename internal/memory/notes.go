package memory

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Note represents a note attached to a feature.
type Note struct {
	ID        string
	FeatureID string
	SessionID string
	Content   string
	Type      string
	CreatedAt string
	UpdatedAt string
}

// CreateNote creates a new note and syncs to FTS and trigram indexes.
func (s *Store) CreateNote(featureID, sessionID, content, noteType string) (*Note, error) {
	if noteType == "" {
		noteType = "note"
	}
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)
	w := s.db.Writer()

	_, err := w.Exec(
		`INSERT INTO notes (id, feature_id, session_id, content, type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, featureID, nullIfEmpty(sessionID), content, noteType, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create note: %w", err)
	}

	// Sync to FTS: get the rowid of the newly inserted note
	var rowID int64
	err = w.QueryRow(`SELECT rowid FROM notes WHERE id = ?`, id).Scan(&rowID)
	if err != nil {
		return nil, fmt.Errorf("get note rowid: %w", err)
	}

	// Sync to notes_fts
	_, err = w.Exec(
		`INSERT INTO notes_fts(rowid, content, type) VALUES (?, ?, ?)`,
		rowID, content, noteType,
	)
	if err != nil {
		return nil, fmt.Errorf("sync note to fts: %w", err)
	}

	// Sync to notes_trigram
	_, err = w.Exec(
		`INSERT INTO notes_trigram(rowid, content) VALUES (?, ?)`,
		rowID, content,
	)
	if err != nil {
		return nil, fmt.Errorf("sync note to trigram: %w", err)
	}

	return &Note{
		ID:        id,
		FeatureID: featureID,
		SessionID: sessionID,
		Content:   content,
		Type:      noteType,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ListNotes returns notes for a feature, optionally filtered by type.
// If noteType is empty, all notes are returned.
func (s *Store) ListNotes(featureID string, noteType string, limit int) ([]Note, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows *sql.Rows
	var err error

	if noteType == "" {
		rows, err = s.db.Reader().Query(
			`SELECT id, feature_id, COALESCE(session_id, ''), content, type, created_at, updated_at
			 FROM notes WHERE feature_id = ?
			 ORDER BY created_at DESC LIMIT ?`,
			featureID, limit,
		)
	} else {
		rows, err = s.db.Reader().Query(
			`SELECT id, feature_id, COALESCE(session_id, ''), content, type, created_at, updated_at
			 FROM notes WHERE feature_id = ? AND type = ?
			 ORDER BY created_at DESC LIMIT ?`,
			featureID, noteType, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.FeatureID, &n.SessionID, &n.Content, &n.Type, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// GetNote retrieves a note by ID.
func (s *Store) GetNote(noteID string) (*Note, error) {
	n := &Note{}
	err := s.db.Reader().QueryRow(
		`SELECT id, feature_id, COALESCE(session_id, ''), content, type, created_at, updated_at
		 FROM notes WHERE id = ?`, noteID,
	).Scan(&n.ID, &n.FeatureID, &n.SessionID, &n.Content, &n.Type, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("note %q not found", noteID)
	}
	if err != nil {
		return nil, fmt.Errorf("get note: %w", err)
	}
	return n, nil
}
