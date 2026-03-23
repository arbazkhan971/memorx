package memory

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Fact represents a bi-temporal fact about a feature.
type Fact struct {
	ID         string
	FeatureID  string
	SessionID  string
	Subject    string
	Predicate  string
	Object     string
	ValidAt    string
	InvalidAt  string
	RecordedAt string
	Confidence float64
}

// CreateFact creates a new fact with contradiction resolution.
// If an active fact with the same subject+predicate exists and the object differs,
// the old fact is invalidated before inserting the new one.
func (s *Store) CreateFact(featureID, sessionID, subject, predicate, object string) (*Fact, error) {
	now := time.Now().UTC().Format(time.DateTime)
	w := s.db.Writer()

	// Check for existing active fact with same subject+predicate
	var existingID, existingObject string
	err := w.QueryRow(
		`SELECT id, object FROM facts
		 WHERE feature_id = ? AND subject = ? AND predicate = ? AND invalid_at IS NULL`,
		featureID, subject, predicate,
	).Scan(&existingID, &existingObject)

	if err == nil && existingObject != object {
		// Contradiction: invalidate the old fact
		if _, err := w.Exec(
			`UPDATE facts SET invalid_at = ? WHERE id = ?`, now, existingID,
		); err != nil {
			return nil, fmt.Errorf("invalidate old fact: %w", err)
		}
	} else if err == nil && existingObject == object {
		// Same fact already exists, return it
		existing := &Fact{}
		err := s.db.Reader().QueryRow(
			`SELECT id, feature_id, COALESCE(session_id, ''), subject, predicate, object,
			        valid_at, COALESCE(invalid_at, ''), recorded_at, confidence
			 FROM facts WHERE id = ?`, existingID,
		).Scan(&existing.ID, &existing.FeatureID, &existing.SessionID,
			&existing.Subject, &existing.Predicate, &existing.Object,
			&existing.ValidAt, &existing.InvalidAt, &existing.RecordedAt, &existing.Confidence)
		if err != nil {
			return nil, fmt.Errorf("read existing fact: %w", err)
		}
		return existing, nil
	}

	// Insert new fact
	id := uuid.New().String()
	_, err = w.Exec(
		`INSERT INTO facts (id, feature_id, session_id, subject, predicate, object, valid_at, recorded_at, confidence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1.0)`,
		id, featureID, nullIfEmpty(sessionID), subject, predicate, object, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create fact: %w", err)
	}

	// Sync to FTS: get the rowid of the newly inserted fact
	var rowID int64
	err = w.QueryRow(`SELECT rowid FROM facts WHERE id = ?`, id).Scan(&rowID)
	if err != nil {
		return nil, fmt.Errorf("get fact rowid: %w", err)
	}
	_, err = w.Exec(
		`INSERT INTO facts_fts(rowid, subject, predicate, object) VALUES (?, ?, ?, ?)`,
		rowID, subject, predicate, object,
	)
	if err != nil {
		return nil, fmt.Errorf("sync fact to fts: %w", err)
	}

	return &Fact{
		ID:         id,
		FeatureID:  featureID,
		SessionID:  sessionID,
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		ValidAt:    now,
		InvalidAt:  "",
		RecordedAt: now,
		Confidence: 1.0,
	}, nil
}

// GetActiveFacts returns all active facts (invalid_at IS NULL) for a feature.
func (s *Store) GetActiveFacts(featureID string) ([]Fact, error) {
	rows, err := s.db.Reader().Query(
		`SELECT id, feature_id, COALESCE(session_id, ''), subject, predicate, object,
		        valid_at, COALESCE(invalid_at, ''), recorded_at, confidence
		 FROM facts WHERE feature_id = ? AND invalid_at IS NULL
		 ORDER BY valid_at DESC`,
		featureID,
	)
	if err != nil {
		return nil, fmt.Errorf("get active facts: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		if err := rows.Scan(&f.ID, &f.FeatureID, &f.SessionID,
			&f.Subject, &f.Predicate, &f.Object,
			&f.ValidAt, &f.InvalidAt, &f.RecordedAt, &f.Confidence); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// InvalidateFact sets invalid_at=now on a fact.
func (s *Store) InvalidateFact(factID string) error {
	now := time.Now().UTC().Format(time.DateTime)
	result, err := s.db.Writer().Exec(
		`UPDATE facts SET invalid_at = ? WHERE id = ?`, now, factID,
	)
	if err != nil {
		return fmt.Errorf("invalidate fact: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("fact %q not found", factID)
	}
	return nil
}

// QueryFactsAsOf performs a bi-temporal query returning facts that were valid at a given time.
// A fact is valid at time T if valid_at <= T AND (invalid_at IS NULL OR invalid_at > T).
func (s *Store) QueryFactsAsOf(featureID string, asOf time.Time) ([]Fact, error) {
	ts := asOf.UTC().Format(time.DateTime)
	rows, err := s.db.Reader().Query(
		`SELECT id, feature_id, COALESCE(session_id, ''), subject, predicate, object,
		        valid_at, COALESCE(invalid_at, ''), recorded_at, confidence
		 FROM facts
		 WHERE feature_id = ? AND valid_at <= ? AND (invalid_at IS NULL OR invalid_at > ?)
		 ORDER BY valid_at DESC`,
		featureID, ts, ts,
	)
	if err != nil {
		return nil, fmt.Errorf("query facts as of: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		if err := rows.Scan(&f.ID, &f.FeatureID, &f.SessionID,
			&f.Subject, &f.Predicate, &f.Object,
			&f.ValidAt, &f.InvalidAt, &f.RecordedAt, &f.Confidence); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// nullIfEmpty returns a sql.NullString that is NULL if the value is empty.
func nullIfEmpty(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
