package memory_test

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arbazkhan971/memorx/internal/memory"
)

// --- Wave 11: Offline Collaboration tests ---

func TestGitSyncExport_CreatesChunkFile(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	// Create feature and some data.
	f, err := store.CreateFeature("sync-feat", "Sync test")
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	store.CreateNote(f.ID, "", "note for sync", "note")
	store.CreateFact(f.ID, "", "db", "uses", "postgres")

	syncPath := filepath.Join(dir, "sync")
	chunkPath, count, err := store.GitSyncExport(syncPath)
	if err != nil {
		t.Fatalf("GitSyncExport: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 entries exported, got %d", count)
	}
	if chunkPath == "" {
		t.Fatal("expected non-empty chunk path")
	}
	if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
		t.Fatalf("chunk file does not exist: %s", chunkPath)
	}

	// Verify manifest was created.
	manifestPath := filepath.Join(syncPath, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatal("manifest.json not created")
	}
}

func TestGitSyncExport_NoDataReturnsZero(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	syncPath := filepath.Join(dir, "sync")
	_, count, err := store.GitSyncExport(syncPath)
	if err != nil {
		t.Fatalf("GitSyncExport: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 entries for empty DB, got %d", count)
	}
}

func TestGitSyncExport_IncrementalOnly(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "sync")

	f, _ := store.CreateFeature("incr-feat", "Incremental test")
	store.CreateNote(f.ID, "", "first note", "note")

	// First export.
	_, count1, err := store.GitSyncExport(syncPath)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	if count1 != 1 {
		t.Errorf("first export: expected 1, got %d", count1)
	}

	// Second export without new data.
	_, count2, err := store.GitSyncExport(syncPath)
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if count2 != 0 {
		t.Errorf("second export: expected 0 (no new data), got %d", count2)
	}
}

func TestGitSyncImport_ReadsChunks(t *testing.T) {
	store1 := newTestStore(t)
	store2 := newTestStore(t)
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "sync")

	// Export from store1.
	f, _ := store1.CreateFeature("import-feat", "Import test")
	store1.CreateNote(f.ID, "", "shared note", "decision")
	store1.CreateFact(f.ID, "", "api", "uses", "REST")

	_, count, err := store1.GitSyncExport(syncPath)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 exported, got %d", count)
	}

	// Import into store2 (needs a clean manifest so it reads our chunks).
	// Remove the manifest so store2 sees chunks as new.
	os.Remove(filepath.Join(syncPath, "manifest.json"))

	imported, err := store2.GitSyncImport(syncPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported != 2 {
		t.Errorf("expected 2 imported, got %d", imported)
	}
}

func TestGitSyncImport_Deduplicates(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "sync")

	f, _ := store.CreateFeature("dedup-feat", "Dedup test")
	store.CreateNote(f.ID, "", "note to dedup", "note")

	// Export.
	store.GitSyncExport(syncPath)

	// Remove manifest so import sees the chunk.
	os.Remove(filepath.Join(syncPath, "manifest.json"))

	// Import same chunk - should skip existing entries.
	imported, err := store.GitSyncImport(syncPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported != 0 {
		t.Errorf("expected 0 imported (all duplicates), got %d", imported)
	}
}

func TestGitSyncImport_NoChunksDir(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	count, err := store.GitSyncImport(filepath.Join(dir, "nonexistent"))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for missing dir, got %d", count)
	}
}

func TestGitSyncChunkIsValidGzip(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()
	syncPath := filepath.Join(dir, "sync")

	f, _ := store.CreateFeature("gzip-feat", "Gzip test")
	store.CreateNote(f.ID, "", "test note", "note")

	chunkPath, _, err := store.GitSyncExport(syncPath)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Verify the chunk is valid gzip containing valid JSONL.
	file, err := os.Open(chunkPath)
	if err != nil {
		t.Fatalf("open chunk: %v", err)
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	dec := json.NewDecoder(gr)
	var entry memory.ChunkEntry
	if err := dec.Decode(&entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if entry.Type != "note" {
		t.Errorf("expected type 'note', got %q", entry.Type)
	}
	if entry.Content != "test note" {
		t.Errorf("expected content 'test note', got %q", entry.Content)
	}
}

// --- Team Decisions tests ---

func TestTeamDecisionsExport_WritesFile(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	f, _ := store.CreateFeature("dec-export", "Decision export test")
	store.CreateNote(f.ID, "", "Use REST for API", "decision")
	store.CreateNote(f.ID, "", "Use PostgreSQL for DB", "decision")

	outPath := filepath.Join(dir, "decisions.jsonl")
	path, count, err := store.TeamDecisionsExport(outPath)
	if err != nil {
		t.Fatalf("TeamDecisionsExport: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 decisions exported, got %d", count)
	}
	if path != outPath {
		t.Errorf("expected path %s, got %s", outPath, path)
	}

	// Verify file content.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestTeamDecisionsExport_NoDecisions(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	outPath := filepath.Join(dir, "empty.jsonl")
	_, count, err := store.TeamDecisionsExport(outPath)
	if err != nil {
		t.Fatalf("TeamDecisionsExport: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 decisions, got %d", count)
	}
}

func TestTeamDecisionsImport_InsertsNew(t *testing.T) {
	store1 := newTestStore(t)
	store2 := newTestStore(t)
	dir := t.TempDir()

	f, _ := store1.CreateFeature("import-dec", "Import test")
	store1.CreateNote(f.ID, "", "Decision A: Use GraphQL", "decision")
	store1.CreateNote(f.ID, "", "Decision B: Use Docker", "decision")

	outPath := filepath.Join(dir, "decisions.jsonl")
	store1.TeamDecisionsExport(outPath)

	imported, skipped, err := store2.TeamDecisionsImport(outPath)
	if err != nil {
		t.Fatalf("TeamDecisionsImport: %v", err)
	}
	if imported != 2 {
		t.Errorf("expected 2 imported, got %d", imported)
	}
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
}

func TestTeamDecisionsImport_DeduplicatesByHash(t *testing.T) {
	store := newTestStore(t)
	dir := t.TempDir()

	f, _ := store.CreateFeature("dedup-dec", "Dedup test")
	store.CreateNote(f.ID, "", "Use REST API", "decision")

	outPath := filepath.Join(dir, "decisions.jsonl")
	store.TeamDecisionsExport(outPath)

	// Import same file - should skip duplicates.
	imported, skipped, err := store.TeamDecisionsImport(outPath)
	if err != nil {
		t.Fatalf("TeamDecisionsImport: %v", err)
	}
	if imported != 0 {
		t.Errorf("expected 0 imported (duplicate), got %d", imported)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
}

// --- Conflict Detection tests ---

func TestDetectConflicts_NoConflicts(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("no-conflict", "No conflicts")
	store.CreateFact(f.ID, "", "db", "uses", "postgres")
	store.CreateFact(f.ID, "", "cache", "uses", "redis")

	conflicts, err := store.DetectConflicts()
	if err != nil {
		t.Fatalf("DetectConflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(conflicts))
	}
}

func TestDetectConflicts_FactConflicts(t *testing.T) {
	store, db := newTestStoreWithDB(t)

	f, _ := store.CreateFeature("fact-conflict", "Fact conflict test")

	// Insert two active facts with same subject+predicate directly.
	w := db.Writer()
	w.Exec(`INSERT INTO facts (id, feature_id, subject, predicate, object, valid_at, recorded_at, confidence) VALUES ('fc1', ?, 'api', 'protocol', 'REST', datetime('now'), datetime('now'), 1.0)`, f.ID)
	w.Exec(`INSERT INTO facts (id, feature_id, subject, predicate, object, valid_at, recorded_at, confidence) VALUES ('fc2', ?, 'api', 'protocol', 'gRPC', datetime('now'), datetime('now'), 1.0)`, f.ID)

	conflicts, err := store.DetectConflicts()
	if err != nil {
		t.Fatalf("DetectConflicts: %v", err)
	}
	if len(conflicts) < 1 {
		t.Fatalf("expected at least 1 conflict, got %d", len(conflicts))
	}

	// Verify the conflict mentions the right values.
	found := false
	for _, c := range conflicts {
		if (c.ValueA == "REST" && c.ValueB == "gRPC") || (c.ValueA == "gRPC" && c.ValueB == "REST") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected conflict between REST and gRPC, got %+v", conflicts)
	}
}

func TestDetectConflicts_DecisionConflicts(t *testing.T) {
	store := newTestStore(t)

	// Create decisions from different features with opposing tech choices.
	f1, _ := store.CreateFeature("team-a", "Team A")
	f2, _ := store.CreateFeature("team-b", "Team B")

	store.CreateNote(f1.ID, "", "We decided to use REST for the user service API communication layer", "decision")
	store.CreateNote(f2.ID, "", "We decided to use gRPC for the user service API communication layer", "decision")

	conflicts, err := store.DetectConflicts()
	if err != nil {
		t.Fatalf("DetectConflicts: %v", err)
	}

	// Should detect the REST vs gRPC contradiction.
	if len(conflicts) < 1 {
		t.Errorf("expected at least 1 decision conflict, got %d", len(conflicts))
	}
}
