package memory

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Session represents a tool session within a feature.
type Session struct {
	ID        string
	FeatureID string
	Tool      string
	StartedAt string
	EndedAt   string
}

// CreateSession creates a new session under the active feature.
func (s *Store) CreateSession(featureID, tool string) (*Session, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)

	_, err := s.db.Writer().Exec(
		`INSERT INTO sessions (id, feature_id, tool, started_at) VALUES (?, ?, ?, ?)`,
		id, featureID, tool, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &Session{
		ID:        id,
		FeatureID: featureID,
		Tool:      tool,
		StartedAt: now,
		EndedAt:   "",
	}, nil
}

// EndSession sets the ended_at timestamp for a session.
func (s *Store) EndSession(sessionID string) error {
	now := time.Now().UTC().Format(time.DateTime)
	result, err := s.db.Writer().Exec(
		`UPDATE sessions SET ended_at = ? WHERE id = ?`, now, sessionID,
	)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %q not found", sessionID)
	}
	return nil
}

// GetCurrentSession returns the most recent un-ended session.
func (s *Store) GetCurrentSession() (*Session, error) {
	sess := &Session{}
	var endedAt sql.NullString
	err := s.db.Reader().QueryRow(
		`SELECT id, feature_id, tool, started_at, COALESCE(ended_at, '')
		 FROM sessions WHERE ended_at IS NULL
		 ORDER BY started_at DESC LIMIT 1`,
	).Scan(&sess.ID, &sess.FeatureID, &sess.Tool, &sess.StartedAt, &endedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active session")
	}
	if err != nil {
		return nil, fmt.Errorf("get current session: %w", err)
	}
	if endedAt.Valid {
		sess.EndedAt = endedAt.String
	}
	return sess, nil
}

// ListSessions returns sessions for a feature, ordered by most recent first.
func (s *Store) ListSessions(featureID string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Reader().Query(
		`SELECT id, feature_id, tool, started_at, COALESCE(ended_at, '')
		 FROM sessions WHERE feature_id = ?
		 ORDER BY started_at DESC LIMIT ?`,
		featureID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.FeatureID, &sess.Tool, &sess.StartedAt, &sess.EndedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}
