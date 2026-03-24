package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Wave 12: Doc Automation tests ---

func TestGenerateADR_AllDecisions(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("adr-feat", "ADR test")
	store.CreateNote(f.ID, "", "Use PostgreSQL for the database layer", "decision")
	store.CreateNote(f.ID, "", "Use Docker for deployment", "decision")

	doc, err := store.GenerateADR("")
	if err != nil {
		t.Fatalf("GenerateADR: %v", err)
	}

	if !strings.Contains(doc, "Architecture Decision Records") {
		t.Error("expected ADR header")
	}
	if !strings.Contains(doc, "PostgreSQL") {
		t.Error("expected PostgreSQL decision")
	}
	if !strings.Contains(doc, "Docker") {
		t.Error("expected Docker decision")
	}
	if !strings.Contains(doc, "ADR-001") {
		t.Error("expected ADR-001 numbering")
	}
	if !strings.Contains(doc, "ADR-002") {
		t.Error("expected ADR-002 numbering")
	}
}

func TestGenerateADR_SingleDecision(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("adr-single", "Single ADR")
	note, _ := store.CreateNote(f.ID, "", "Use JWT for authentication", "decision")

	doc, err := store.GenerateADR(note.ID)
	if err != nil {
		t.Fatalf("GenerateADR: %v", err)
	}

	if !strings.Contains(doc, "JWT") {
		t.Error("expected JWT in ADR")
	}
	if !strings.Contains(doc, "Accepted") {
		t.Error("expected Accepted status")
	}
	if !strings.Contains(doc, "### Decision") {
		t.Error("expected Decision section")
	}
	if !strings.Contains(doc, "### Context") {
		t.Error("expected Context section")
	}
}

func TestGenerateADR_NoDecisions(t *testing.T) {
	store := newTestStore(t)

	doc, err := store.GenerateADR("")
	if err != nil {
		t.Fatalf("GenerateADR: %v", err)
	}
	if !strings.Contains(doc, "No decisions recorded") {
		t.Error("expected no-decisions message")
	}
}

func TestGenerateADR_InvalidID(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GenerateADR("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestGenerateADR_WithContext(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("adr-ctx", "ADR with context")
	store.CreateFact(f.ID, "", "db", "uses", "postgres")
	store.CreateNote(f.ID, "", "Related note about DB setup", "note")
	note, _ := store.CreateNote(f.ID, "", "Use connection pooling for database", "decision")
	store.CreateNote(f.ID, "", "DB connections are slow", "blocker")

	doc, err := store.GenerateADR(note.ID)
	if err != nil {
		t.Fatalf("GenerateADR: %v", err)
	}

	if !strings.Contains(doc, "Known facts") {
		t.Error("expected facts in context")
	}
	if !strings.Contains(doc, "Blocker") {
		t.Error("expected blocker in consequences")
	}
}

// --- README Generation tests ---

func TestGenerateReadme_WritesFile(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	store.CreateFeature("readme-feat", "A cool feature")

	outPath := filepath.Join(dir, "README.md")
	content, err := store.GenerateReadme(dir, outPath)
	if err != nil {
		t.Fatalf("GenerateReadme: %v", err)
	}

	if content == "" {
		t.Fatal("expected non-empty content")
	}

	// Verify file was written.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if string(data) != content {
		t.Error("file content does not match returned content")
	}
}

func TestGenerateReadme_IncludesFeatures(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	store.CreateFeature("auth-system", "Authentication system")
	store.CreateFeature("api-v2", "API version 2")

	content, err := store.GenerateReadme(dir, "")
	if err != nil {
		t.Fatalf("GenerateReadme: %v", err)
	}

	if !strings.Contains(content, "auth-system") {
		t.Error("expected auth-system feature")
	}
	if !strings.Contains(content, "api-v2") {
		t.Error("expected api-v2 feature")
	}
	if !strings.Contains(content, "## Features") {
		t.Error("expected Features section")
	}
}

func TestGenerateReadme_IncludesTechFacts(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	f, _ := store.CreateFeature("tech-feat", "Tech feature")
	store.CreateFact(f.ID, "", "backend", "uses", "Go")

	content, err := store.GenerateReadme(dir, "")
	if err != nil {
		t.Fatalf("GenerateReadme: %v", err)
	}

	if !strings.Contains(content, "## Tech Stack") {
		t.Error("expected Tech Stack section")
	}
	if !strings.Contains(content, "Go") {
		t.Error("expected Go in tech stack")
	}
}

func TestGenerateReadme_DefaultOutputPath(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	_, err := store.GenerateReadme(dir, "")
	if err != nil {
		t.Fatalf("GenerateReadme: %v", err)
	}

	// Verify file was written at default location.
	defaultPath := filepath.Join(dir, "README.md")
	if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
		t.Fatal("expected README.md at git root")
	}
}

// --- API Docs tests ---

func TestGenerateAPIDocs_NoContent(t *testing.T) {
	store := newTestStore(t)

	doc, err := store.GenerateAPIDocs()
	if err != nil {
		t.Fatalf("GenerateAPIDocs: %v", err)
	}

	if !strings.Contains(doc, "API Documentation") {
		t.Error("expected API Documentation header")
	}
	if !strings.Contains(doc, "No API-related content") {
		t.Error("expected no-content message")
	}
}

func TestGenerateAPIDocs_FindsAPIContent(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("api-feat", "API feature")
	store.CreateNote(f.ID, "", "POST /api/users creates a new user", "decision")
	store.CreateNote(f.ID, "", "GET /api/users returns user list", "note")
	store.CreateFact(f.ID, "", "/api/health", "endpoint", "GET health check")

	doc, err := store.GenerateAPIDocs()
	if err != nil {
		t.Fatalf("GenerateAPIDocs: %v", err)
	}

	if !strings.Contains(doc, "API Documentation") {
		t.Error("expected API Documentation header")
	}
	if !strings.Contains(doc, "/api/users") {
		t.Error("expected /api/users in docs")
	}
	if !strings.Contains(doc, "/api/health") {
		t.Error("expected /api/health endpoint")
	}
}

func TestGenerateAPIDocs_GroupsByType(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("api-group", "API grouping test")
	store.CreateNote(f.ID, "", "We decided to use REST endpoints for all public APIs", "decision")
	store.CreateNote(f.ID, "", "The GET /status endpoint returns server health", "note")

	doc, err := store.GenerateAPIDocs()
	if err != nil {
		t.Fatalf("GenerateAPIDocs: %v", err)
	}

	if !strings.Contains(doc, "API Decisions") {
		t.Error("expected API Decisions section")
	}
	if !strings.Contains(doc, "API Notes") {
		t.Error("expected API Notes section")
	}
}

// --- Runbook tests ---

func TestGenerateRunbook_Empty(t *testing.T) {
	store := newTestStore(t)

	doc, err := store.GenerateRunbook("")
	if err != nil {
		t.Fatalf("GenerateRunbook: %v", err)
	}

	if !strings.Contains(doc, "Operational Runbook") {
		t.Error("expected Operational Runbook header")
	}
	if !strings.Contains(doc, "No error logs") {
		t.Error("expected empty state message")
	}
}

func TestGenerateRunbook_WithBlockers(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("runbook-feat", "Runbook test")
	store.CreateNote(f.ID, "", "DB connection pool exhausted under load", "blocker")
	store.CreateNote(f.ID, "", "Increase max connections to 100", "decision")

	doc, err := store.GenerateRunbook("")
	if err != nil {
		t.Fatalf("GenerateRunbook: %v", err)
	}

	if !strings.Contains(doc, "Known Blockers") {
		t.Error("expected Known Blockers section")
	}
	if !strings.Contains(doc, "connection pool") {
		t.Error("expected blocker content")
	}
	if !strings.Contains(doc, "Related Decisions") {
		t.Error("expected Related Decisions section")
	}
}

func TestGenerateRunbook_WithErrors(t *testing.T) {
	store, db := newTestStoreWithDB(t)

	f, _ := store.CreateFeature("runbook-err", "Runbook error test")

	// Insert error log entry directly.
	w := db.Writer()
	w.Exec(`INSERT INTO error_log (id, feature_id, error_message, file_path, cause, resolution, resolved, created_at)
		VALUES ('err1', ?, 'Connection timeout', 'db/pool.go', 'Too many connections', 'Increase pool size', 1, datetime('now'))`, f.ID)

	doc, err := store.GenerateRunbook("runbook-err")
	if err != nil {
		t.Fatalf("GenerateRunbook: %v", err)
	}

	if !strings.Contains(doc, "Error Resolution Guide") {
		t.Error("expected Error Resolution Guide section")
	}
	if !strings.Contains(doc, "Connection timeout") {
		t.Error("expected error message")
	}
	if !strings.Contains(doc, "Increase pool size") {
		t.Error("expected resolution")
	}
	if !strings.Contains(doc, "Resolved") {
		t.Error("expected Resolved status")
	}
}

func TestGenerateRunbook_FeatureScope(t *testing.T) {
	store := newTestStore(t)

	f1, _ := store.CreateFeature("runbook-a", "Feature A")
	f2, _ := store.CreateFeature("runbook-b", "Feature B")

	store.CreateNote(f1.ID, "", "Blocker in feature A", "blocker")
	store.CreateNote(f2.ID, "", "Blocker in feature B", "blocker")

	doc, err := store.GenerateRunbook("runbook-a")
	if err != nil {
		t.Fatalf("GenerateRunbook: %v", err)
	}

	if !strings.Contains(doc, "Blocker in feature A") {
		t.Error("expected blocker from feature A")
	}
	if strings.Contains(doc, "Blocker in feature B") {
		t.Error("should not include blocker from feature B when scoped")
	}
}
