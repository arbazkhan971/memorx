package consolidation

import (
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/arbaz/devmem/internal/storage"
)

// ConsolidationState represents the current state of the consolidation engine.
type ConsolidationState struct {
	LastRunAt         string
	EntropyScore      float64
	UnsummarizedCount int
	ConflictCount     int
	NextTriggerAt     string
}

// Config controls the consolidation engine's behavior.
type Config struct {
	EntropyThreshold  float64       // default 0.7
	IdleTimeout       time.Duration // default 5 minutes
	MaxUnsummarized   int           // default 20
	MaxConflicts      int           // default 3
	DecayHalfLifeDays float64       // default 14.0
}

// DefaultConfig returns the default consolidation configuration.
func DefaultConfig() Config {
	return Config{
		EntropyThreshold:  0.7,
		IdleTimeout:       5 * time.Minute,
		MaxUnsummarized:   20,
		MaxConflicts:      3,
		DecayHalfLifeDays: 14.0,
	}
}

// Engine manages memory quality through periodic consolidation.
type Engine struct {
	db     *storage.DB
	cfg    Config
	mu     sync.Mutex
	stopCh chan struct{}
	done   chan struct{}
	running bool
}

// NewEngine creates a new consolidation engine.
func NewEngine(db *storage.DB, cfg Config) *Engine {
	return &Engine{
		db:  db,
		cfg: cfg,
	}
}

// Start begins the background consolidation goroutine.
func (e *Engine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return
	}

	e.stopCh = make(chan struct{})
	e.done = make(chan struct{})
	e.running = true

	go func() {
		defer close(e.done)
		ticker := time.NewTicker(e.cfg.IdleTimeout)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				entropy, err := e.calculateEntropy()
				if err != nil {
					continue
				}
				if entropy >= e.cfg.EntropyThreshold {
					_ = e.RunOnce()
				}
			case <-e.stopCh:
				return
			}
		}
	}()
}

// Stop stops the background consolidation goroutine.
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	close(e.stopCh)
	e.running = false
	e.mu.Unlock()

	<-e.done
}

// RunOnce executes a single consolidation pass.
func (e *Engine) RunOnce() error {
	// Detect and resolve contradictions
	invalidated, err := e.DetectContradictions()
	if err != nil {
		return fmt.Errorf("detect contradictions: %w", err)
	}

	// Discover links for unlinked notes
	_, err = e.DiscoverLinks()
	if err != nil {
		return fmt.Errorf("discover links: %w", err)
	}

	// Generate summaries for all features with enough unsummarized notes
	featureIDs, err := e.getFeatureIDs()
	if err != nil {
		return fmt.Errorf("get feature IDs: %w", err)
	}
	for _, fid := range featureIDs {
		_, err := e.GenerateSummaries(fid)
		if err != nil {
			continue // best-effort per feature
		}
	}

	// Calculate and store the new entropy
	entropy, err := e.calculateEntropy()
	if err != nil {
		return fmt.Errorf("calculate entropy: %w", err)
	}

	// Count unsummarized and conflicts for state
	unsummarized, err := e.countUnsummarized()
	if err != nil {
		return fmt.Errorf("count unsummarized: %w", err)
	}

	now := time.Now().UTC().Format(time.DateTime)
	nextTrigger := time.Now().UTC().Add(e.cfg.IdleTimeout).Format(time.DateTime)

	_, err = e.db.Writer().Exec(
		`UPDATE consolidation_state SET
			last_run_at = ?,
			entropy_score = ?,
			unsummarized_count = ?,
			conflict_count = ?,
			next_trigger_at = ?
		WHERE id = 1`,
		now, entropy, unsummarized, invalidated, nextTrigger,
	)
	if err != nil {
		return fmt.Errorf("update consolidation state: %w", err)
	}

	return nil
}

// GetState reads the current consolidation state from the database.
func (e *Engine) GetState() (*ConsolidationState, error) {
	state := &ConsolidationState{}
	var lastRunAt, nextTriggerAt sql.NullString

	err := e.db.Reader().QueryRow(
		`SELECT COALESCE(last_run_at, ''), entropy_score, unsummarized_count, conflict_count, COALESCE(next_trigger_at, '')
		 FROM consolidation_state WHERE id = 1`,
	).Scan(&lastRunAt, &state.EntropyScore, &state.UnsummarizedCount, &state.ConflictCount, &nextTriggerAt)
	if err != nil {
		return nil, fmt.Errorf("get consolidation state: %w", err)
	}

	state.LastRunAt = lastRunAt.String
	state.NextTriggerAt = nextTriggerAt.String
	return state, nil
}

// calculateEntropy computes the entropy score based on the formula:
// entropy = (unsummarized_count / MaxUnsummarized) * 0.4 +
//
//	(conflict_count / MaxConflicts) * 0.3 +
//	(hours_since_last_run / 24) * 0.3
//
// Each component is capped at 1.0 before weighting.
func (e *Engine) calculateEntropy() (float64, error) {
	unsummarized, err := e.countUnsummarized()
	if err != nil {
		return 0, err
	}

	conflicts, err := e.countConflicts()
	if err != nil {
		return 0, err
	}

	hoursSinceLastRun, err := e.hoursSinceLastRun()
	if err != nil {
		return 0, err
	}

	unsummarizedRatio := math.Min(float64(unsummarized)/float64(e.cfg.MaxUnsummarized), 1.0)
	conflictRatio := math.Min(float64(conflicts)/float64(e.cfg.MaxConflicts), 1.0)
	timeRatio := math.Min(hoursSinceLastRun/24.0, 1.0)

	entropy := unsummarizedRatio*0.4 + conflictRatio*0.3 + timeRatio*0.3
	return entropy, nil
}

// countUnsummarized returns the total number of notes not covered by any summary.
func (e *Engine) countUnsummarized() (int, error) {
	var count int
	err := e.db.Reader().QueryRow(
		`SELECT COUNT(*) FROM notes n
		 WHERE NOT EXISTS (
			SELECT 1 FROM summaries s
			WHERE s.scope = 'feature:' || n.feature_id
			AND s.covers_from <= n.created_at
			AND s.covers_to >= n.created_at
		 )`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unsummarized: %w", err)
	}
	return count, nil
}

// countConflicts returns the number of active fact conflict groups.
func (e *Engine) countConflicts() (int, error) {
	var count int
	err := e.db.Reader().QueryRow(
		`SELECT COUNT(*) FROM (
			SELECT subject, predicate
			FROM facts WHERE invalid_at IS NULL
			GROUP BY subject, predicate
			HAVING COUNT(*) > 1
		)`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count conflicts: %w", err)
	}
	return count, nil
}

// hoursSinceLastRun returns the hours elapsed since the last consolidation run.
func (e *Engine) hoursSinceLastRun() (float64, error) {
	var lastRunAt sql.NullString
	err := e.db.Reader().QueryRow(
		`SELECT last_run_at FROM consolidation_state WHERE id = 1`,
	).Scan(&lastRunAt)
	if err != nil {
		return 24, nil // default to 24 hours if can't read
	}
	if !lastRunAt.Valid || lastRunAt.String == "" {
		return 24, nil // never run before, return 24 hours
	}

	lastRun, err := time.Parse(time.DateTime, lastRunAt.String)
	if err != nil {
		return 24, nil
	}

	return time.Since(lastRun).Hours(), nil
}

// getFeatureIDs returns all feature IDs from the database.
func (e *Engine) getFeatureIDs() ([]string, error) {
	rows, err := e.db.Reader().Query(`SELECT id FROM features`)
	if err != nil {
		return nil, fmt.Errorf("query features: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan feature id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
