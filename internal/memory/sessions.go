package memory

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID, FeatureID, Tool, StartedAt, EndedAt, Summary string
}

const sessionCols = `id, feature_id, tool, started_at, COALESCE(ended_at, ''), COALESCE(summary, '')`

func scanSession(sc interface{ Scan(...any) error }) (Session, error) {
	var s Session
	err := sc.Scan(&s.ID, &s.FeatureID, &s.Tool, &s.StartedAt, &s.EndedAt, &s.Summary)
	return s, err
}

func (s *Store) CreateSession(featureID, tool string) (*Session, error) {
	id, now := uuid.New().String(), time.Now().UTC().Format(time.DateTime)
	if _, err := s.db.Writer().Exec(`INSERT INTO sessions (id, feature_id, tool, started_at) VALUES (?, ?, ?, ?)`, id, featureID, tool, now); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Session{ID: id, FeatureID: featureID, Tool: tool, StartedAt: now}, nil
}

func (s *Store) endSession(sessionID, summary string, withSummary bool) error {
	now := time.Now().UTC().Format(time.DateTime)
	var res sql.Result
	var err error
	if withSummary {
		res, err = s.db.Writer().Exec(`UPDATE sessions SET ended_at = ?, summary = ? WHERE id = ?`, now, summary, sessionID)
	} else {
		res, err = s.db.Writer().Exec(`UPDATE sessions SET ended_at = ? WHERE id = ?`, now, sessionID)
	}
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("session %q not found", sessionID)
	}
	return nil
}

func (s *Store) EndSessionWithSummary(sessionID, summary string) error {
	return s.endSession(sessionID, summary, true)
}

func (s *Store) EndSession(sessionID string) error {
	return s.endSession(sessionID, "", false)
}

func (s *Store) GetCurrentSession() (*Session, error) {
	sess, err := scanSession(s.db.Reader().QueryRow(`SELECT ` + sessionCols + ` FROM sessions WHERE ended_at IS NULL ORDER BY started_at DESC LIMIT 1`))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active session")
	}
	if err != nil {
		return nil, fmt.Errorf("get current session: %w", err)
	}
	return &sess, nil
}

func (s *Store) ListSessions(featureID string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	return collectRows(s.db.Reader(),
		`SELECT `+sessionCols+` FROM sessions WHERE feature_id = ? ORDER BY started_at DESC LIMIT ?`,
		[]any{featureID, limit},
		func(rows *sql.Rows) (Session, error) { return scanSession(rows) })
}
