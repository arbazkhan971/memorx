package memory_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arbaz/devmem/internal/memory"
	"github.com/arbaz/devmem/internal/storage"
)

func TestWriteSnapshot_EmptyDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := storage.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := memory.NewStore(db)
	memDir := filepath.Join(dir, ".memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = store.WriteSnapshot(memDir, "test-project", "/tmp/test-project")
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	// Read and parse the output
	data, err := os.ReadFile(filepath.Join(memDir, "current.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var snap memory.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if snap.Project != "test-project" {
		t.Errorf("expected project 'test-project', got %q", snap.Project)
	}
	if snap.ProjectPath != "/tmp/test-project" {
		t.Errorf("expected project_path '/tmp/test-project', got %q", snap.ProjectPath)
	}
	if snap.ActiveFeature != nil {
		t.Error("expected nil active_feature for empty DB")
	}
	if snap.ActivePlan != nil {
		t.Error("expected nil active_plan for empty DB")
	}
	if len(snap.Features) != 0 {
		t.Errorf("expected 0 features, got %d", len(snap.Features))
	}
}

func TestWriteSnapshot_WithData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := storage.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := memory.NewStore(db)

	// Create features
	_, err = store.StartFeature("auth-v2", "Authentication system v2")
	if err != nil {
		t.Fatalf("StartFeature auth-v2: %v", err)
	}

	_, err = store.StartFeature("billing-fix", "Fix billing bugs")
	if err != nil {
		t.Fatalf("StartFeature billing-fix: %v", err)
	}

	// Create a session for billing-fix
	feature, err := store.GetActiveFeature()
	if err != nil {
		t.Fatalf("GetActiveFeature: %v", err)
	}
	_, err = store.CreateSession(feature.ID, "claude-code")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	memDir := filepath.Join(dir, ".memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = store.WriteSnapshot(memDir, "myproject", "/home/user/myproject")
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(memDir, "current.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var snap memory.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify project info
	if snap.Project != "myproject" {
		t.Errorf("expected project 'myproject', got %q", snap.Project)
	}

	// Verify features
	if len(snap.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(snap.Features))
	}

	// Verify active feature is billing-fix (last started)
	if snap.ActiveFeature == nil {
		t.Fatal("expected non-nil active_feature")
	}
	if snap.ActiveFeature.Name != "billing-fix" {
		t.Errorf("expected active feature 'billing-fix', got %q", snap.ActiveFeature.Name)
	}
	if snap.ActiveFeature.Status != "active" {
		t.Errorf("expected active status, got %q", snap.ActiveFeature.Status)
	}

	// Verify billing-fix has 1 session
	if snap.ActiveFeature.Sessions != 1 {
		t.Errorf("expected 1 session for billing-fix, got %d", snap.ActiveFeature.Sessions)
	}

	// Verify auth-v2 is paused
	found := false
	for _, f := range snap.Features {
		if f.Name == "auth-v2" {
			found = true
			if f.Status != "paused" {
				t.Errorf("expected auth-v2 status 'paused', got %q", f.Status)
			}
		}
	}
	if !found {
		t.Error("expected to find auth-v2 in features list")
	}

	// Verify no active plan
	if snap.ActivePlan != nil {
		t.Error("expected nil active_plan when no plan exists")
	}

	// Verify JSON is valid and pretty-printed (has indentation)
	if data[0] != '{' {
		t.Error("expected JSON to start with '{'")
	}
	// Pretty-printed JSON will have newlines
	hasNewline := false
	for _, b := range data {
		if b == '\n' {
			hasNewline = true
			break
		}
	}
	if !hasNewline {
		t.Error("expected pretty-printed JSON with newlines")
	}
}

func TestWriteSnapshot_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := storage.NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := memory.NewStore(db)
	memDir := filepath.Join(dir, ".memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// First write with no features
	err = store.WriteSnapshot(memDir, "proj", "/tmp/proj")
	if err != nil {
		t.Fatalf("first WriteSnapshot: %v", err)
	}

	// Add a feature
	_, err = store.StartFeature("new-feat", "A feature")
	if err != nil {
		t.Fatalf("StartFeature: %v", err)
	}

	// Second write should overwrite
	err = store.WriteSnapshot(memDir, "proj", "/tmp/proj")
	if err != nil {
		t.Fatalf("second WriteSnapshot: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(memDir, "current.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var snap memory.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(snap.Features) != 1 {
		t.Errorf("expected 1 feature after overwrite, got %d", len(snap.Features))
	}
	if snap.Features[0].Name != "new-feat" {
		t.Errorf("expected feature 'new-feat', got %q", snap.Features[0].Name)
	}
}
