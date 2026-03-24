package memory

import (
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Wave 11: Offline Collaboration ---

// ChunkEntry is a single memory entry serialized to a JSONL chunk.
type ChunkEntry struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "note", "fact", "decision"
	Feature   string `json:"feature"`
	Content   string `json:"content"`
	NoteType  string `json:"note_type,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Predicate string `json:"predicate,omitempty"`
	Object    string `json:"object,omitempty"`
	CreatedAt string `json:"created_at"`
	Author    string `json:"author,omitempty"`
}

// ChunkManifest tracks exported chunk files.
type ChunkManifest struct {
	LastExportAt string   `json:"last_export_at"`
	Chunks       []string `json:"chunks"`
}

// GitSyncExport writes new memories since the last export as an immutable
// .jsonl.gz chunk file. Returns the path of the newly created chunk.
func (s *Store) GitSyncExport(syncPath string) (string, int, error) {
	if syncPath == "" {
		syncPath = ".memory/sync"
	}
	chunksDir := filepath.Join(syncPath, "chunks")
	if err := os.MkdirAll(chunksDir, 0755); err != nil {
		return "", 0, fmt.Errorf("create chunks dir: %w", err)
	}

	// Read manifest to find last export time.
	manifest := loadManifest(syncPath)
	var since string
	if manifest.LastExportAt != "" {
		since = manifest.LastExportAt
	}

	// Collect new entries.
	r := s.db.Reader()
	var entries []ChunkEntry

	// Notes since last export.
	noteQuery := `SELECT n.id, n.content, n.type, n.created_at, COALESCE(f.name, '') FROM notes n LEFT JOIN features f ON n.feature_id = f.id`
	noteArgs := []any{}
	if since != "" {
		noteQuery += ` WHERE n.created_at > ?`
		noteArgs = append(noteArgs, since)
	}
	noteQuery += ` ORDER BY n.created_at ASC`

	noteRows := scanRows(r, noteQuery, noteArgs, func(rows *sql.Rows) (ChunkEntry, error) {
		var e ChunkEntry
		return e, rows.Scan(&e.ID, &e.Content, &e.NoteType, &e.CreatedAt, &e.Feature)
	})
	for _, e := range noteRows {
		e.Type = "note"
		entries = append(entries, e)
	}

	// Facts since last export.
	factQuery := `SELECT fa.id, fa.subject, fa.predicate, fa.object, fa.recorded_at, COALESCE(f.name, '') FROM facts fa LEFT JOIN features f ON fa.feature_id = f.id WHERE fa.invalid_at IS NULL`
	factArgs := []any{}
	if since != "" {
		factQuery += ` AND fa.recorded_at > ?`
		factArgs = append(factArgs, since)
	}
	factQuery += ` ORDER BY fa.recorded_at ASC`

	factRows := scanRows(r, factQuery, factArgs, func(rows *sql.Rows) (ChunkEntry, error) {
		var e ChunkEntry
		return e, rows.Scan(&e.ID, &e.Subject, &e.Predicate, &e.Object, &e.CreatedAt, &e.Feature)
	})
	for _, e := range factRows {
		e.Type = "fact"
		e.Content = fmt.Sprintf("%s %s %s", e.Subject, e.Predicate, e.Object)
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return "", 0, nil
	}

	// Write chunk file.
	now := time.Now().UTC()
	chunkName := fmt.Sprintf("chunk-%s.jsonl.gz", now.Format("20060102T150405Z"))
	chunkPath := filepath.Join(chunksDir, chunkName)

	f, err := os.Create(chunkPath)
	if err != nil {
		return "", 0, fmt.Errorf("create chunk file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	enc := json.NewEncoder(gw)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			return "", 0, fmt.Errorf("encode entry: %w", err)
		}
	}

	if err := gw.Close(); err != nil {
		return "", 0, fmt.Errorf("close gzip writer: %w", err)
	}

	// Update manifest.
	manifest.LastExportAt = now.Format(time.DateTime)
	manifest.Chunks = append(manifest.Chunks, chunkName)
	if err := saveManifest(syncPath, manifest); err != nil {
		return "", 0, fmt.Errorf("save manifest: %w", err)
	}

	return chunkPath, len(entries), nil
}

// GitSyncImport reads chunk files not yet imported and inserts their
// entries into the database. Returns count of imported entries.
func (s *Store) GitSyncImport(syncPath string) (int, error) {
	if syncPath == "" {
		syncPath = ".memory/sync"
	}
	chunksDir := filepath.Join(syncPath, "chunks")

	manifest := loadManifest(syncPath)
	imported := make(map[string]bool, len(manifest.Chunks))
	for _, c := range manifest.Chunks {
		imported[c] = true
	}

	// Find chunk files on disk.
	dirEntries, err := os.ReadDir(chunksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read chunks dir: %w", err)
	}

	count := 0
	var newChunks []string
	for _, de := range dirEntries {
		name := de.Name()
		if !strings.HasSuffix(name, ".jsonl.gz") {
			continue
		}
		if imported[name] {
			continue
		}

		n, err := s.importChunkFile(filepath.Join(chunksDir, name))
		if err != nil {
			return count, fmt.Errorf("import chunk %s: %w", name, err)
		}
		count += n
		newChunks = append(newChunks, name)
	}

	// Update manifest with newly imported chunks.
	if len(newChunks) > 0 {
		manifest.Chunks = append(manifest.Chunks, newChunks...)
		manifest.LastExportAt = time.Now().UTC().Format(time.DateTime)
		if err := saveManifest(syncPath, manifest); err != nil {
			return count, fmt.Errorf("save manifest: %w", err)
		}
	}

	return count, nil
}

func (s *Store) importChunkFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	dec := json.NewDecoder(gr)
	count := 0
	for {
		var entry ChunkEntry
		if err := dec.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return count, fmt.Errorf("decode entry: %w", err)
		}

		// Ensure a feature exists for the entry.
		featureID, err := s.ensureFeatureID(entry.Feature)
		if err != nil {
			continue
		}

		// Deduplicate by ID: skip if already exists.
		switch entry.Type {
		case "note":
			var exists int
			if s.db.Reader().QueryRow(`SELECT COUNT(*) FROM notes WHERE id = ?`, entry.ID).Scan(&exists) == nil && exists > 0 {
				continue
			}
			noteType := entry.NoteType
			if noteType == "" {
				noteType = "note"
			}
			now := time.Now().UTC().Format(time.DateTime)
			_, err = s.db.Writer().Exec(`INSERT INTO notes (id, feature_id, content, type, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				entry.ID, featureID, entry.Content, noteType, entry.CreatedAt, now)
		case "fact":
			var exists int
			if s.db.Reader().QueryRow(`SELECT COUNT(*) FROM facts WHERE id = ?`, entry.ID).Scan(&exists) == nil && exists > 0 {
				continue
			}
			now := time.Now().UTC().Format(time.DateTime)
			_, err = s.db.Writer().Exec(`INSERT INTO facts (id, feature_id, subject, predicate, object, valid_at, recorded_at, confidence) VALUES (?, ?, ?, ?, ?, ?, ?, 1.0)`,
				entry.ID, featureID, entry.Subject, entry.Predicate, entry.Object, entry.CreatedAt, now)
		}
		if err == nil {
			count++
		}
	}
	return count, nil
}

func (s *Store) ensureFeatureID(name string) (string, error) {
	if name == "" {
		name = "imported"
	}
	f, err := s.GetFeature(name)
	if err == nil {
		return f.ID, nil
	}
	f, err = s.CreateFeature(name, "Imported via sync")
	if err != nil {
		return "", err
	}
	return f.ID, nil
}

func loadManifest(syncPath string) *ChunkManifest {
	data, err := os.ReadFile(filepath.Join(syncPath, "manifest.json"))
	if err != nil {
		return &ChunkManifest{}
	}
	var m ChunkManifest
	if json.Unmarshal(data, &m) != nil {
		return &ChunkManifest{}
	}
	return &m
}

func saveManifest(syncPath string, m *ChunkManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(syncPath, "manifest.json"), data, 0644)
}

// --- Team Decisions ---

// DecisionEntry represents a decision for export/import.
type DecisionEntry struct {
	ID          string `json:"id"`
	Feature     string `json:"feature"`
	Content     string `json:"content"`
	Timestamp   string `json:"timestamp"`
	Author      string `json:"author,omitempty"`
	ContentHash string `json:"content_hash"`
}

// TeamDecisionsExport writes all decision notes to a .jsonl file.
func (s *Store) TeamDecisionsExport(path string) (string, int, error) {
	if path == "" {
		path = "decisions.jsonl"
	}

	r := s.db.Reader()
	rows := scanRows(r,
		`SELECT n.id, n.content, n.created_at, COALESCE(f.name, '')
		 FROM notes n
		 LEFT JOIN features f ON n.feature_id = f.id
		 WHERE n.type = 'decision'
		 ORDER BY n.created_at ASC`,
		nil,
		func(rows *sql.Rows) (DecisionEntry, error) {
			var d DecisionEntry
			return d, rows.Scan(&d.ID, &d.Content, &d.Timestamp, &d.Feature)
		},
	)

	if len(rows) == 0 {
		return path, 0, nil
	}

	// Ensure output directory exists.
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		os.MkdirAll(dir, 0755)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", 0, fmt.Errorf("create decisions file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for i := range rows {
		rows[i].ContentHash = contentHash(rows[i].Content)
		if err := enc.Encode(rows[i]); err != nil {
			return "", 0, fmt.Errorf("encode decision: %w", err)
		}
	}

	return path, len(rows), nil
}

// TeamDecisionsImport reads a decisions .jsonl file, inserting missing
// decisions by content hash deduplication.
func (s *Store) TeamDecisionsImport(path string) (int, int, error) {
	if path == "" {
		path = "decisions.jsonl"
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open decisions file: %w", err)
	}
	defer f.Close()

	// Build a set of existing decision content hashes.
	existingHashes := make(map[string]bool)
	existingRows := scanRows(s.db.Reader(),
		`SELECT content FROM notes WHERE type = 'decision'`, nil,
		func(rows *sql.Rows) (string, error) {
			var c string
			return c, rows.Scan(&c)
		},
	)
	for _, c := range existingRows {
		existingHashes[contentHash(c)] = true
	}

	dec := json.NewDecoder(f)
	imported, skipped := 0, 0
	for {
		var entry DecisionEntry
		if err := dec.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return imported, skipped, fmt.Errorf("decode decision: %w", err)
		}

		hash := entry.ContentHash
		if hash == "" {
			hash = contentHash(entry.Content)
		}

		if existingHashes[hash] {
			skipped++
			continue
		}

		featureID, err := s.ensureFeatureID(entry.Feature)
		if err != nil {
			skipped++
			continue
		}

		now := time.Now().UTC().Format(time.DateTime)
		id := uuid.New().String()
		_, err = s.db.Writer().Exec(
			`INSERT INTO notes (id, feature_id, content, type, created_at, updated_at) VALUES (?, ?, ?, 'decision', ?, ?)`,
			id, featureID, entry.Content, now, now,
		)
		if err == nil {
			imported++
			existingHashes[hash] = true
		} else {
			skipped++
		}
	}

	return imported, skipped, nil
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return fmt.Sprintf("%x", h[:16])
}

// --- Conflict Detection ---

// ConflictPair describes two contradicting decisions.
type ConflictPair struct {
	NoteA  string `json:"note_a"`
	NoteB  string `json:"note_b"`
	Topic  string `json:"topic"`
	ValueA string `json:"value_a"`
	ValueB string `json:"value_b"`
}

// DetectConflicts finds contradicting decisions and facts.
// For facts: same subject+predicate but different object.
// For decisions: looks for common topic keywords with opposing choices.
func (s *Store) DetectConflicts() ([]ConflictPair, error) {
	r := s.db.Reader()
	var conflicts []ConflictPair

	// 1. Fact-level conflicts: same subject+predicate, different object, both active.
	factRows := scanRows(r,
		`SELECT f1.subject, f1.predicate, f1.object, f2.object
		 FROM facts f1
		 JOIN facts f2 ON f1.subject = f2.subject AND f1.predicate = f2.predicate
		 WHERE f1.invalid_at IS NULL AND f2.invalid_at IS NULL
		   AND f1.id < f2.id AND f1.object != f2.object`,
		nil,
		func(rows *sql.Rows) (ConflictPair, error) {
			var c ConflictPair
			return c, rows.Scan(&c.Topic, &c.NoteA, &c.ValueA, &c.ValueB)
		},
	)
	for _, fc := range factRows {
		conflicts = append(conflicts, ConflictPair{
			Topic:  fmt.Sprintf("%s %s", fc.Topic, fc.NoteA),
			ValueA: fc.ValueA,
			ValueB: fc.ValueB,
			NoteA:  fmt.Sprintf("Fact: %s %s %s", fc.Topic, fc.NoteA, fc.ValueA),
			NoteB:  fmt.Sprintf("Fact: %s %s %s", fc.Topic, fc.NoteA, fc.ValueB),
		})
	}

	// 2. Decision-level conflicts: decisions about same topic with different choices.
	decisions := scanRows(r,
		`SELECT n.id, n.content, COALESCE(f.name, 'unknown')
		 FROM notes n
		 LEFT JOIN features f ON n.feature_id = f.id
		 WHERE n.type = 'decision'
		 ORDER BY n.created_at DESC
		 LIMIT 200`,
		nil,
		func(rows *sql.Rows) ([3]string, error) {
			var d [3]string
			return d, rows.Scan(&d[0], &d[1], &d[2])
		},
	)

	// Extract topic keywords from decisions and look for contradictions.
	type decInfo struct {
		id, content, feature string
		keywords             map[string]bool
	}
	var decs []decInfo
	for _, d := range decisions {
		kw := extractKeywords(d[1])
		decs = append(decs, decInfo{id: d[0], content: d[1], feature: d[2], keywords: kw})
	}

	// Compare pairs that share topic keywords but have opposing signals.
	seen := make(map[string]bool)
	for i := 0; i < len(decs); i++ {
		for j := i + 1; j < len(decs); j++ {
			overlap := keywordOverlap(decs[i].keywords, decs[j].keywords)
			if len(overlap) == 0 {
				continue
			}
			topic := strings.Join(overlap, ", ")
			key := decs[i].id + ":" + decs[j].id
			if seen[key] {
				continue
			}
			// Check if they express different choices (heuristic: different feature authors).
			if decs[i].feature != decs[j].feature && hasConflictSignal(decs[i].content, decs[j].content) {
				seen[key] = true
				conflicts = append(conflicts, ConflictPair{
					Topic:  topic,
					NoteA:  truncateStr(decs[i].content, 120),
					NoteB:  truncateStr(decs[j].content, 120),
					ValueA: decs[i].feature,
					ValueB: decs[j].feature,
				})
			}
		}
	}

	return conflicts, nil
}

// extractKeywords pulls significant words from a decision string.
func extractKeywords(s string) map[string]bool {
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true,
		"has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true,
		"might": true, "must": true, "shall": true, "can": true, "to": true,
		"of": true, "in": true, "for": true, "on": true, "with": true, "at": true,
		"by": true, "from": true, "as": true, "into": true, "about": true,
		"and": true, "but": true, "or": true, "not": true, "no": true,
		"so": true, "if": true, "then": true, "than": true, "that": true,
		"this": true, "it": true, "we": true, "use": true, "using": true,
		"decided": true, "decision": true, "choose": true, "chose": true,
	}
	words := strings.Fields(strings.ToLower(s))
	kw := make(map[string]bool)
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}#")
		if len(w) > 2 && !stopwords[w] {
			kw[w] = true
		}
	}
	return kw
}

func keywordOverlap(a, b map[string]bool) []string {
	var overlap []string
	for k := range a {
		if b[k] {
			overlap = append(overlap, k)
		}
	}
	sort.Strings(overlap)
	// Only consider overlap meaningful if at least 2 shared keywords.
	if len(overlap) < 2 {
		return nil
	}
	if len(overlap) > 3 {
		overlap = overlap[:3]
	}
	return overlap
}

// hasConflictSignal checks if two decision texts likely express different choices.
func hasConflictSignal(a, b string) bool {
	choiceWords := []string{"rest", "grpc", "graphql", "postgres", "mysql", "sqlite", "mongo",
		"react", "vue", "angular", "svelte", "jwt", "oauth", "session",
		"microservice", "monolith", "serverless", "docker", "kubernetes",
		"redis", "memcached", "kafka", "rabbitmq", "nats"}
	aLower := strings.ToLower(a)
	bLower := strings.ToLower(b)

	aChoices := make(map[string]bool)
	bChoices := make(map[string]bool)
	for _, cw := range choiceWords {
		if strings.Contains(aLower, cw) {
			aChoices[cw] = true
		}
		if strings.Contains(bLower, cw) {
			bChoices[cw] = true
		}
	}

	// Conflict if they mention different tech choices.
	for c := range aChoices {
		if !bChoices[c] {
			if len(bChoices) > 0 {
				return true
			}
		}
	}
	return false
}

func truncateStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
