package memory

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/arbaz/devmem/internal/git"
	"github.com/arbaz/devmem/internal/storage"
	"github.com/google/uuid"
)

// Feature represents a development feature being tracked.
type Feature struct {
	ID          string
	Name        string
	Description string
	Status      string
	Branch      string
	CreatedAt   string
	LastActive  string
}

// Store wraps a storage.DB and provides memory operations.
type Store struct {
	db *storage.DB
}

// NewStore creates a new Store backed by the given DB.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// detectBranch tries to detect the current git branch.
func detectBranch() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, err := git.FindGitRoot(cwd)
	if err != nil {
		return ""
	}
	branch, err := git.CurrentBranch(root)
	if err != nil {
		return ""
	}
	return branch
}

// CreateFeature creates a new feature with a UUID and auto-detected git branch.
func (s *Store) CreateFeature(name, description string) (*Feature, error) {
	id := uuid.New().String()
	branch := detectBranch()
	now := time.Now().UTC().Format(time.DateTime)

	_, err := s.db.Writer().Exec(
		`INSERT INTO features (id, name, description, status, branch, created_at, last_active)
		 VALUES (?, ?, ?, 'active', ?, ?, ?)`,
		id, name, description, branch, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create feature: %w", err)
	}

	return &Feature{
		ID:          id,
		Name:        name,
		Description: description,
		Status:      "active",
		Branch:      branch,
		CreatedAt:   now,
		LastActive:  now,
	}, nil
}

// GetFeature retrieves a feature by name.
func (s *Store) GetFeature(name string) (*Feature, error) {
	f := &Feature{}
	err := s.db.Reader().QueryRow(
		`SELECT id, name, description, status, COALESCE(branch, ''), created_at, last_active
		 FROM features WHERE name = ?`, name,
	).Scan(&f.ID, &f.Name, &f.Description, &f.Status, &f.Branch, &f.CreatedAt, &f.LastActive)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("feature %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get feature: %w", err)
	}
	return f, nil
}

// ListFeatures returns features filtered by status.
// statusFilter can be "all", "active", "paused", or "done".
func (s *Store) ListFeatures(statusFilter string) ([]Feature, error) {
	var rows *sql.Rows
	var err error

	if statusFilter == "" || statusFilter == "all" {
		rows, err = s.db.Reader().Query(
			`SELECT id, name, description, status, COALESCE(branch, ''), created_at, last_active
			 FROM features ORDER BY last_active DESC`)
	} else {
		rows, err = s.db.Reader().Query(
			`SELECT id, name, description, status, COALESCE(branch, ''), created_at, last_active
			 FROM features WHERE status = ? ORDER BY last_active DESC`, statusFilter)
	}
	if err != nil {
		return nil, fmt.Errorf("list features: %w", err)
	}
	defer rows.Close()

	var features []Feature
	for rows.Next() {
		var f Feature
		if err := rows.Scan(&f.ID, &f.Name, &f.Description, &f.Status, &f.Branch, &f.CreatedAt, &f.LastActive); err != nil {
			return nil, fmt.Errorf("scan feature: %w", err)
		}
		features = append(features, f)
	}
	return features, rows.Err()
}

// UpdateFeatureStatus updates a feature's status by name.
func (s *Store) UpdateFeatureStatus(name, status string) error {
	now := time.Now().UTC().Format(time.DateTime)
	result, err := s.db.Writer().Exec(
		`UPDATE features SET status = ?, last_active = ? WHERE name = ?`,
		status, now, name,
	)
	if err != nil {
		return fmt.Errorf("update feature status: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("feature %q not found", name)
	}
	return nil
}

// GetActiveFeature returns the currently active feature (status='active').
func (s *Store) GetActiveFeature() (*Feature, error) {
	f := &Feature{}
	err := s.db.Reader().QueryRow(
		`SELECT id, name, description, status, COALESCE(branch, ''), created_at, last_active
		 FROM features WHERE status = 'active' ORDER BY last_active DESC LIMIT 1`,
	).Scan(&f.ID, &f.Name, &f.Description, &f.Status, &f.Branch, &f.CreatedAt, &f.LastActive)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active feature")
	}
	if err != nil {
		return nil, fmt.Errorf("get active feature: %w", err)
	}
	return f, nil
}

// StartFeature creates or resumes a feature.
// If it already exists, set it to active. Any currently active feature is auto-paused.
func (s *Store) StartFeature(name, description string) (*Feature, error) {
	now := time.Now().UTC().Format(time.DateTime)

	// Auto-pause any currently active feature
	_, err := s.db.Writer().Exec(
		`UPDATE features SET status = 'paused', last_active = ? WHERE status = 'active'`, now,
	)
	if err != nil {
		return nil, fmt.Errorf("pause active features: %w", err)
	}

	// Check if feature already exists
	existing := &Feature{}
	err = s.db.Reader().QueryRow(
		`SELECT id, name, description, status, COALESCE(branch, ''), created_at, last_active
		 FROM features WHERE name = ?`, name,
	).Scan(&existing.ID, &existing.Name, &existing.Description, &existing.Status, &existing.Branch, &existing.CreatedAt, &existing.LastActive)

	if err == sql.ErrNoRows {
		// Create new feature
		return s.createFeatureActive(name, description, now)
	}
	if err != nil {
		return nil, fmt.Errorf("check existing feature: %w", err)
	}

	// Resume existing feature
	_, err = s.db.Writer().Exec(
		`UPDATE features SET status = 'active', last_active = ? WHERE id = ?`,
		now, existing.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("resume feature: %w", err)
	}
	existing.Status = "active"
	existing.LastActive = now
	return existing, nil
}

// createFeatureActive creates a new feature with active status.
func (s *Store) createFeatureActive(name, description, now string) (*Feature, error) {
	id := uuid.New().String()
	branch := detectBranch()

	_, err := s.db.Writer().Exec(
		`INSERT INTO features (id, name, description, status, branch, created_at, last_active)
		 VALUES (?, ?, ?, 'active', ?, ?, ?)`,
		id, name, description, branch, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create feature: %w", err)
	}

	return &Feature{
		ID:          id,
		Name:        name,
		Description: description,
		Status:      "active",
		Branch:      branch,
		CreatedAt:   now,
		LastActive:  now,
	}, nil
}
