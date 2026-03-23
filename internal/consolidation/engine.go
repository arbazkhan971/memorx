package consolidation

import (
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/arbaz/devmem/internal/storage"
)

type ConsolidationState struct {
	LastRunAt         string
	EntropyScore      float64
	UnsummarizedCount int
	ConflictCount     int
	NextTriggerAt     string
}

type Config struct {
	EntropyThreshold  float64
	IdleTimeout       time.Duration
	MaxUnsummarized   int
	MaxConflicts      int
	DecayHalfLifeDays float64
}

func DefaultConfig() Config {
	return Config{
		EntropyThreshold:  0.7,
		IdleTimeout:       5 * time.Minute,
		MaxUnsummarized:   20,
		MaxConflicts:      3,
		DecayHalfLifeDays: 14.0,
	}
}

type Engine struct {
	db      *storage.DB
	cfg     Config
	mu      sync.Mutex
	stopCh  chan struct{}
	done    chan struct{}
	running bool
}

func NewEngine(db *storage.DB, cfg Config) *Engine {
	return &Engine{db: db, cfg: cfg}
}

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
				if entropy, err := e.calculateEntropy(); err == nil && entropy >= e.cfg.EntropyThreshold {
					_ = e.RunOnce()
				}
			case <-e.stopCh:
				return
			}
		}
	}()
}

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

func (e *Engine) RunOnce() error {
	invalidated, err := e.DetectContradictions()
	if err != nil {
		return fmt.Errorf("detect contradictions: %w", err)
	}
	if _, err = e.DiscoverLinks(); err != nil {
		return fmt.Errorf("discover links: %w", err)
	}

	featureIDs, err := e.getFeatureIDs()
	if err != nil {
		return fmt.Errorf("get feature IDs: %w", err)
	}
	for _, fid := range featureIDs {
		e.GenerateSummaries(fid) //nolint:errcheck // best-effort
	}

	entropy, err := e.calculateEntropy()
	if err != nil {
		return fmt.Errorf("calculate entropy: %w", err)
	}
	unsummarized, err := e.countUnsummarized()
	if err != nil {
		return fmt.Errorf("count unsummarized: %w", err)
	}

	now := time.Now().UTC().Format(time.DateTime)
	nextTrigger := time.Now().UTC().Add(e.cfg.IdleTimeout).Format(time.DateTime)
	_, err = e.db.Writer().Exec(
		`UPDATE consolidation_state SET last_run_at=?, entropy_score=?, unsummarized_count=?, conflict_count=?, next_trigger_at=? WHERE id=1`,
		now, entropy, unsummarized, invalidated, nextTrigger,
	)
	if err != nil {
		return fmt.Errorf("update consolidation state: %w", err)
	}
	return nil
}

func (e *Engine) GetState() (*ConsolidationState, error) {
	state := &ConsolidationState{}
	var lastRunAt, nextTriggerAt sql.NullString
	err := e.db.Reader().QueryRow(
		`SELECT COALESCE(last_run_at,''), entropy_score, unsummarized_count, conflict_count, COALESCE(next_trigger_at,'') FROM consolidation_state WHERE id=1`,
	).Scan(&lastRunAt, &state.EntropyScore, &state.UnsummarizedCount, &state.ConflictCount, &nextTriggerAt)
	if err != nil {
		return nil, fmt.Errorf("get consolidation state: %w", err)
	}
	state.LastRunAt = lastRunAt.String
	state.NextTriggerAt = nextTriggerAt.String
	return state, nil
}

// calculateEntropy: weighted sum of unsummarized ratio (0.4), conflict ratio (0.3), time ratio (0.3), each capped at 1.0.
func (e *Engine) calculateEntropy() (float64, error) {
	unsummarized, err := e.countUnsummarized()
	if err != nil {
		return 0, err
	}
	conflicts, err := e.countConflicts()
	if err != nil {
		return 0, err
	}
	hours, err := e.hoursSinceLastRun()
	if err != nil {
		return 0, err
	}
	cap := func(v, max float64) float64 { return math.Min(v/max, 1.0) }
	return cap(float64(unsummarized), float64(e.cfg.MaxUnsummarized))*0.4 +
		cap(float64(conflicts), float64(e.cfg.MaxConflicts))*0.3 +
		cap(hours, 24.0)*0.3, nil
}

func (e *Engine) countUnsummarized() (int, error) {
	var count int
	err := e.db.Reader().QueryRow(
		`SELECT COUNT(*) FROM notes n WHERE NOT EXISTS (
			SELECT 1 FROM summaries s WHERE s.scope='feature:'||n.feature_id AND s.covers_from<=n.created_at AND s.covers_to>=n.created_at)`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unsummarized: %w", err)
	}
	return count, nil
}

func (e *Engine) countConflicts() (int, error) {
	var count int
	err := e.db.Reader().QueryRow(
		conflictGroupsSQL,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count conflicts: %w", err)
	}
	return count, nil
}

func (e *Engine) hoursSinceLastRun() (float64, error) {
	var lastRunAt sql.NullString
	err := e.db.Reader().QueryRow(`SELECT last_run_at FROM consolidation_state WHERE id=1`).Scan(&lastRunAt)
	if err != nil || !lastRunAt.Valid || lastRunAt.String == "" {
		return 24, nil
	}
	lastRun, err := time.Parse(time.DateTime, lastRunAt.String)
	if err != nil {
		return 24, nil
	}
	return time.Since(lastRun).Hours(), nil
}

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

// ApplyDecay counts notes older than 30 days with no outgoing links.
func (e *Engine) ApplyDecay() (int, error) {
	var count int
	err := e.db.Reader().QueryRow(
		`SELECT COUNT(*) FROM notes n WHERE n.created_at < datetime('now','-30 days')
		 AND NOT EXISTS (SELECT 1 FROM memory_links ml WHERE ml.source_id=n.id AND ml.source_type='note')`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count stale notes: %w", err)
	}
	return count, nil
}
