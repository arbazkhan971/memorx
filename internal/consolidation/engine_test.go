package consolidation_test

import (
	"path/filepath"
	"testing"

	"github.com/arbaz/devmem/internal/consolidation"
	"github.com/arbaz/devmem/internal/storage"
)

// newTestDB creates a temp DB with migrations applied.
func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.NewDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func TestNewEngine(t *testing.T) {
	db := newTestDB(t)
	cfg := consolidation.DefaultConfig()

	engine := consolidation.NewEngine(db, cfg)
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := consolidation.DefaultConfig()

	if cfg.EntropyThreshold != 0.7 {
		t.Errorf("expected EntropyThreshold 0.7, got %f", cfg.EntropyThreshold)
	}
	if cfg.MaxUnsummarized != 20 {
		t.Errorf("expected MaxUnsummarized 20, got %d", cfg.MaxUnsummarized)
	}
	if cfg.MaxConflicts != 3 {
		t.Errorf("expected MaxConflicts 3, got %d", cfg.MaxConflicts)
	}
	if cfg.DecayHalfLifeDays != 14.0 {
		t.Errorf("expected DecayHalfLifeDays 14.0, got %f", cfg.DecayHalfLifeDays)
	}
}

func TestRunOnce_EmptyDB(t *testing.T) {
	db := newTestDB(t)
	engine := consolidation.NewEngine(db, consolidation.DefaultConfig())

	err := engine.RunOnce()
	if err != nil {
		t.Fatalf("RunOnce on empty DB: %v", err)
	}
}

func TestGetState_InitialState(t *testing.T) {
	db := newTestDB(t)
	engine := consolidation.NewEngine(db, consolidation.DefaultConfig())

	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	if state.EntropyScore != 0.0 {
		t.Errorf("expected initial entropy 0.0, got %f", state.EntropyScore)
	}
	if state.UnsummarizedCount != 0 {
		t.Errorf("expected initial unsummarized count 0, got %d", state.UnsummarizedCount)
	}
	if state.ConflictCount != 0 {
		t.Errorf("expected initial conflict count 0, got %d", state.ConflictCount)
	}
}

func TestGetState_AfterRunOnce(t *testing.T) {
	db := newTestDB(t)
	engine := consolidation.NewEngine(db, consolidation.DefaultConfig())

	err := engine.RunOnce()
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	if state.LastRunAt == "" {
		t.Error("expected non-empty LastRunAt after RunOnce")
	}
	if state.NextTriggerAt == "" {
		t.Error("expected non-empty NextTriggerAt after RunOnce")
	}
}

func TestStartStop(t *testing.T) {
	db := newTestDB(t)
	cfg := consolidation.DefaultConfig()
	cfg.IdleTimeout = 100 * 1000 * 1000 // very long to avoid firing during test

	engine := consolidation.NewEngine(db, cfg)

	engine.Start()
	// Starting again should be a no-op
	engine.Start()

	engine.Stop()
	// Stopping again should be a no-op
	engine.Stop()
}
