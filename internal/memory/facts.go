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

// factColumns is the SELECT list shared by all fact queries.
const factColumns = `id, feature_id, COALESCE(session_id, ''), subject, predicate, object,
        valid_at, COALESCE(invalid_at, ''), recorded_at, confidence`

// scanFact scans a single fact row into a Fact struct.
func scanFact(row interface{ Scan(dest ...any) error }) (Fact, error) {
	var f Fact
	err := row.Scan(&f.ID, &f.FeatureID, &f.SessionID,
		&f.Subject, &f.Predicate, &f.Object,
		&f.ValidAt, &f.InvalidAt, &f.RecordedAt, &f.Confidence)
	return f, err
}

// collectFacts iterates rows and returns a slice of Facts.
func collectFacts(rows *sql.Rows) ([]Fact, error) {
	defer rows.Close()
	var facts []Fact
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
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

	if err == nil {
		if existingObject == object {
			// Same fact already exists, return it
			f, err := scanFact(s.db.Reader().QueryRow(
				`SELECT `+factColumns+` FROM facts WHERE id = ?`, existingID))
			if err != nil {
				return nil, fmt.Errorf("read existing fact: %w", err)
			}
			return &f, nil
		}
		// Contradiction: invalidate the old fact
		if _, err := w.Exec(`UPDATE facts SET invalid_at = ? WHERE id = ?`, now, existingID); err != nil {
			return nil, fmt.Errorf("invalidate old fact: %w", err)
		}
	}

	// Insert new fact
	id := uuid.New().String()
	if _, err = w.Exec(
		`INSERT INTO facts (id, feature_id, session_id, subject, predicate, object, valid_at, recorded_at, confidence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1.0)`,
		id, featureID, nullIfEmpty(sessionID), subject, predicate, object, now, now,
	); err != nil {
		return nil, fmt.Errorf("create fact: %w", err)
	}

	// Sync to FTS
	var rowID int64
	if err = w.QueryRow(`SELECT rowid FROM facts WHERE id = ?`, id).Scan(&rowID); err != nil {
		return nil, fmt.Errorf("get fact rowid: %w", err)
	}
	if _, err = w.Exec(
		`INSERT INTO facts_fts(rowid, subject, predicate, object) VALUES (?, ?, ?, ?)`,
		rowID, subject, predicate, object,
	); err != nil {
		return nil, fmt.Errorf("sync fact to fts: %w", err)
	}

	return &Fact{
		ID: id, FeatureID: featureID, SessionID: sessionID,
		Subject: subject, Predicate: predicate, Object: object,
		ValidAt: now, RecordedAt: now, Confidence: 1.0,
	}, nil
}

// GetActiveFacts returns all active facts (invalid_at IS NULL) for a feature.
func (s *Store) GetActiveFacts(featureID string) ([]Fact, error) {
	return s.queryFacts(featureID, nil)
}

// QueryFactsAsOf performs a bi-temporal query returning facts that were valid at a given time.
func (s *Store) QueryFactsAsOf(featureID string, asOf time.Time) ([]Fact, error) {
	return s.queryFacts(featureID, &asOf)
}

// queryFacts is the shared implementation for GetActiveFacts and QueryFactsAsOf.
func (s *Store) queryFacts(featureID string, asOf *time.Time) ([]Fact, error) {
	var query string
	var args []any

	if asOf != nil {
		ts := asOf.UTC().Format(time.DateTime)
		query = `SELECT ` + factColumns + ` FROM facts
		         WHERE feature_id = ? AND valid_at <= ? AND (invalid_at IS NULL OR invalid_at > ?)
		         ORDER BY valid_at DESC`
		args = []any{featureID, ts, ts}
	} else {
		query = `SELECT ` + factColumns + ` FROM facts
		         WHERE feature_id = ? AND invalid_at IS NULL
		         ORDER BY valid_at DESC`
		args = []any{featureID}
	}

	rows, err := s.db.Reader().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	return collectFacts(rows)
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

// nullIfEmpty returns a sql.NullString that is NULL if the value is empty.
func nullIfEmpty(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
