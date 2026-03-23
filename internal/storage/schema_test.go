package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/arbaz/devmem/internal/storage"
)

func TestMigrate_CreatesAllTables(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.NewDB(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	expectedTables := []string{
		"features", "sessions", "facts", "notes",
		"plans", "plan_steps", "commits", "semantic_changes",
		"memory_links", "summaries", "consolidation_state",
		"schema_version",
	}
	for _, table := range expectedTables {
		var name string
		err := db.Reader().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrate_CreatesFTSTables(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.NewDB(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	ftsTables := []string{"notes_fts", "commits_fts", "facts_fts", "plans_fts", "notes_trigram", "commits_trigram"}
	for _, table := range ftsTables {
		var name string
		err := db.Reader().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("FTS table %s not found: %v", table, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.NewDB(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("second Migrate should be idempotent: %v", err)
	}
}

func TestMigrate_ConsolidationStateInitialized(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.NewDB(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var entropy float64
	err = db.Reader().QueryRow("SELECT entropy_score FROM consolidation_state WHERE id = 1").Scan(&entropy)
	if err != nil {
		t.Fatalf("consolidation_state not initialized: %v", err)
	}
	if entropy != 0.0 {
		t.Fatalf("expected entropy 0.0, got %f", entropy)
	}
}
