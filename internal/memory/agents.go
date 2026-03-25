package memory

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Agent types ---

type Agent struct {
	Name, Role, RegisteredAt string
}

type AgentScope struct {
	Agent, Feature string
}

type AuditEntry struct {
	ID, Operation, Details, Agent, CreatedAt string
}

type ConfigEntry struct {
	Key, Value string
}

type ArchiveFeature struct {
	FeatureName, ArchivedAt string
}

// --- Agent Registration ---

func (s *Store) RegisterAgent(name, role string) (*Agent, error) {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := s.db.Writer().Exec(
		`INSERT INTO agents (name, role, registered_at) VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET role = excluded.role`,
		name, role, now,
	)
	if err != nil {
		return nil, fmt.Errorf("register agent: %w", err)
	}
	return &Agent{Name: name, Role: role, RegisteredAt: now}, nil
}

func (s *Store) GetAgent(name string) (*Agent, error) {
	var a Agent
	err := s.db.Reader().QueryRow(`SELECT name, role, registered_at FROM agents WHERE name = ?`, name).Scan(&a.Name, &a.Role, &a.RegisteredAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return &a, nil
}

// --- Agent Handoff ---

func (s *Store) AgentHandoff(fromAgent, toAgent, summary string, endSessionID string) (string, error) {
	now := time.Now().UTC().Format(time.DateTime)
	id := uuid.New().String()
	w := s.db.Writer()

	// Store handoff record
	if _, err := w.Exec(
		`INSERT INTO agent_handoffs (id, from_agent, to_agent, summary, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, fromAgent, toAgent, summary, now,
	); err != nil {
		return "", fmt.Errorf("create handoff: %w", err)
	}

	// End current session if one is active
	if endSessionID != "" {
		_, _ = w.Exec(`UPDATE sessions SET ended_at = ?, summary = ? WHERE id = ? AND ended_at IS NULL`, now, "Handoff to "+toAgent+": "+summary, endSessionID)
	}

	// Log to audit
	_ = s.AuditLog("agent_handoff", fmt.Sprintf("%s -> %s: %s", fromAgent, toAgent, summary), fromAgent)

	return id, nil
}

// --- Agent Scope ---

func (s *Store) ManageAgentScope(agent string, features []string, action string) ([]string, error) {
	w := s.db.Writer()
	r := s.db.Reader()

	switch action {
	case "grant":
		for _, f := range features {
			if _, err := w.Exec(
				`INSERT OR IGNORE INTO agent_scopes (agent, feature) VALUES (?, ?)`, agent, f,
			); err != nil {
				return nil, fmt.Errorf("grant scope: %w", err)
			}
		}
	case "revoke":
		for _, f := range features {
			if _, err := w.Exec(`DELETE FROM agent_scopes WHERE agent = ? AND feature = ?`, agent, f); err != nil {
				return nil, fmt.Errorf("revoke scope: %w", err)
			}
		}
	case "list":
		// just return current scope
	default:
		return nil, fmt.Errorf("unknown scope action: %q", action)
	}

	rows, err := r.Query(`SELECT feature FROM agent_scopes WHERE agent = ? ORDER BY feature`, agent)
	if err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("scan scope: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// --- Agent Merge ---

func (s *Store) AgentMerge(featureName string) (int, int, int, error) {
	r := s.db.Reader()

	// Find feature
	var featureID string
	err := r.QueryRow(`SELECT id FROM features WHERE name = ?`, featureName).Scan(&featureID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("feature %q not found", featureName)
	}

	// Count sessions, notes, facts for this feature
	var sessions, notes, facts int
	r.QueryRow(`SELECT COUNT(*) FROM sessions WHERE feature_id = ?`, featureID).Scan(&sessions)
	r.QueryRow(`SELECT COUNT(*) FROM notes WHERE feature_id = ?`, featureID).Scan(&notes)
	r.QueryRow(`SELECT COUNT(*) FROM facts WHERE feature_id = ? AND invalid_at IS NULL`, featureID).Scan(&facts)

	return sessions, notes, facts, nil
}

// --- Audit Log ---

func (s *Store) AuditLog(operation, details, agent string) error {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)
	if agent == "" {
		agent = "system"
	}
	_, err := s.db.Writer().Exec(
		`INSERT INTO audit_log (id, operation, details, agent, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, operation, details, agent, now,
	)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	return nil
}

func (s *Store) QueryAuditLog(limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Reader().Query(
		`SELECT id, operation, details, agent, created_at FROM audit_log ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Operation, &e.Details, &e.Agent, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Sensitive Filter ---

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(token|secret|bearer)\s*[:=]\s*\S+`),
	regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
	regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
	regexp.MustCompile(`(?i)(sk|pk|rk)[-_][a-zA-Z0-9]{20,}`),
}

type SensitiveResult struct {
	NoteID  string
	Content string
	Matches []string
}

func (s *Store) SensitiveScan(featureID string) ([]SensitiveResult, error) {
	q := `SELECT id, content FROM notes`
	var args []any
	if featureID != "" {
		q += ` WHERE feature_id = ?`
		args = append(args, featureID)
	}
	rows, err := s.db.Reader().Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("scan notes: %w", err)
	}
	defer rows.Close()

	var results []SensitiveResult
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		var matches []string
		for _, p := range sensitivePatterns {
			if m := p.FindAllString(content, -1); len(m) > 0 {
				matches = append(matches, m...)
			}
		}
		if len(matches) > 0 {
			results = append(results, SensitiveResult{NoteID: id, Content: content, Matches: matches})
		}
	}
	return results, rows.Err()
}

func (s *Store) SensitiveRedact(featureID string) (int, error) {
	results, err := s.SensitiveScan(featureID)
	if err != nil {
		return 0, err
	}
	w := s.db.Writer()
	count := 0
	for _, r := range results {
		redacted := r.Content
		for _, p := range sensitivePatterns {
			redacted = p.ReplaceAllString(redacted, "[REDACTED]")
		}
		if redacted != r.Content {
			if _, err := w.Exec(`UPDATE notes SET content = ?, updated_at = ? WHERE id = ?`, redacted, time.Now().UTC().Format(time.DateTime), r.NoteID); err == nil {
				count++
			}
		}
	}
	return count, nil
}

// --- Retention Policy ---

type RetentionPolicy struct {
	Days  int
	Types []string
}

func (s *Store) SetRetentionPolicy(days int, types []string) error {
	w := s.db.Writer()
	data, _ := json.Marshal(RetentionPolicy{Days: days, Types: types})
	_, err := w.Exec(
		`INSERT INTO config (key, value) VALUES ('retention_policy', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		string(data),
	)
	return err
}

func (s *Store) GetRetentionPolicy() (*RetentionPolicy, error) {
	var val string
	err := s.db.Reader().QueryRow(`SELECT value FROM config WHERE key = 'retention_policy'`).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no retention policy set")
	}
	if err != nil {
		return nil, fmt.Errorf("get retention policy: %w", err)
	}
	var p RetentionPolicy
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		return nil, fmt.Errorf("parse retention policy: %w", err)
	}
	return &p, nil
}

func (s *Store) ApplyRetentionPolicy() (int, error) {
	p, err := s.GetRetentionPolicy()
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -p.Days).Format(time.DateTime)
	w := s.db.Writer()
	total := 0
	for _, t := range p.Types {
		var res sql.Result
		switch t {
		case "notes":
			res, err = w.Exec(`DELETE FROM notes WHERE created_at < ? AND pinned = 0`, cutoff)
		case "facts":
			res, err = w.Exec(`DELETE FROM facts WHERE recorded_at < ? AND pinned = 0`, cutoff)
		case "commits":
			res, err = w.Exec(`DELETE FROM commits WHERE synced_at < ?`, cutoff)
		}
		if err == nil && res != nil {
			n, _ := res.RowsAffected()
			total += int(n)
		}
	}
	return total, nil
}

// --- Export Compliance ---

type ComplianceExport struct {
	Notes    []Note
	Facts    []Fact
	Audit    []AuditEntry
	Feature  string
	ExportAt string
}

func (s *Store) ExportCompliance(featureName, format string) (string, error) {
	r := s.db.Reader()
	var featureID string
	if featureName != "" {
		if err := r.QueryRow(`SELECT id FROM features WHERE name = ?`, featureName).Scan(&featureID); err != nil {
			return "", fmt.Errorf("feature %q not found", featureName)
		}
	}

	// Collect notes
	noteQ := `SELECT ` + noteCols + ` FROM notes`
	var noteArgs []any
	if featureID != "" {
		noteQ += ` WHERE feature_id = ?`
		noteArgs = append(noteArgs, featureID)
	}
	noteQ += ` ORDER BY created_at DESC`
	notes, err := collectRows(r, noteQ, noteArgs, func(rows *sql.Rows) (Note, error) { return scanNote(rows) })
	if err != nil {
		return "", fmt.Errorf("collect notes: %w", err)
	}

	// Collect facts
	factQ := `SELECT ` + factColumns + ` FROM facts`
	var factArgs []any
	if featureID != "" {
		factQ += ` WHERE feature_id = ?`
		factArgs = append(factArgs, featureID)
	}
	factQ += ` ORDER BY recorded_at DESC`
	facts, err := collectRows(r, factQ, factArgs, func(rows *sql.Rows) (Fact, error) { return scanFact(rows) })
	if err != nil {
		return "", fmt.Errorf("collect facts: %w", err)
	}

	audit, _ := s.QueryAuditLog(100)

	if format == "csv" {
		return exportCSV(notes, facts, audit)
	}
	return exportJSON(notes, facts, audit, featureName)
}

func exportJSON(notes []Note, facts []Fact, audit []AuditEntry, feature string) (string, error) {
	export := ComplianceExport{
		Notes:    notes,
		Facts:    facts,
		Audit:    audit,
		Feature:  feature,
		ExportAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(data), nil
}

func exportCSV(notes []Note, facts []Fact, audit []AuditEntry) (string, error) {
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"type", "id", "content", "created_at"})
	for _, n := range notes {
		_ = w.Write([]string{"note", n.ID, n.Content, n.CreatedAt})
	}
	for _, f := range facts {
		_ = w.Write([]string{"fact", f.ID, f.Subject + " " + f.Predicate + " " + f.Object, f.RecordedAt})
	}
	for _, a := range audit {
		_ = w.Write([]string{"audit", a.ID, a.Operation + ": " + a.Details, a.CreatedAt})
	}
	w.Flush()
	return buf.String(), nil
}

// --- Vacuum ---

func (s *Store) Vacuum() (int64, int64, error) {
	path := s.db.Path()
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, fmt.Errorf("stat db: %w", err)
	}
	before := fi.Size()

	w := s.db.Writer()
	if _, err := w.Exec("VACUUM"); err != nil {
		return 0, 0, fmt.Errorf("vacuum: %w", err)
	}
	if _, err := w.Exec("ANALYZE"); err != nil {
		return 0, 0, fmt.Errorf("analyze: %w", err)
	}

	fi, err = os.Stat(path)
	if err != nil {
		return before, before, nil
	}
	return before, fi.Size(), nil
}

// --- Stats ---

type DBStats struct {
	Tables   map[string]int
	FileSize int64
}

func (s *Store) Stats() (*DBStats, error) {
	r := s.db.Reader()
	stats := &DBStats{Tables: make(map[string]int)}

	tables := []string{"features", "sessions", "notes", "facts", "commits", "plans", "plan_steps", "memory_links", "agents", "agent_scopes", "audit_log", "config"}
	for _, t := range tables {
		var count int
		if err := r.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", t)).Scan(&count); err == nil {
			stats.Tables[t] = count
		}
	}

	fi, err := os.Stat(s.db.Path())
	if err == nil {
		stats.FileSize = fi.Size()
	}
	return stats, nil
}

// --- Archive ---

func (s *Store) ArchiveFeature(featureName string) error {
	w := s.db.Writer()
	now := time.Now().UTC().Format(time.DateTime)

	// Verify feature exists
	var featureID string
	if err := s.db.Reader().QueryRow(`SELECT id FROM features WHERE name = ?`, featureName).Scan(&featureID); err != nil {
		return fmt.Errorf("feature %q not found", featureName)
	}

	// Mark feature as done
	if _, err := w.Exec(`UPDATE features SET status = 'done' WHERE id = ?`, featureID); err != nil {
		return fmt.Errorf("mark feature done: %w", err)
	}

	// Record in archive_features table
	if _, err := w.Exec(`INSERT OR IGNORE INTO archive_features (feature_name, archived_at) VALUES (?, ?)`, featureName, now); err != nil {
		return fmt.Errorf("record archive: %w", err)
	}

	_ = s.AuditLog("archive", fmt.Sprintf("Archived feature: %s", featureName), "system")
	return nil
}

func (s *Store) RestoreFeature(featureName string) error {
	w := s.db.Writer()

	if _, err := w.Exec(`UPDATE features SET status = 'paused' WHERE name = ?`, featureName); err != nil {
		return fmt.Errorf("restore feature: %w", err)
	}
	if _, err := w.Exec(`DELETE FROM archive_features WHERE feature_name = ?`, featureName); err != nil {
		return fmt.Errorf("remove archive record: %w", err)
	}

	_ = s.AuditLog("restore", fmt.Sprintf("Restored feature: %s", featureName), "system")
	return nil
}

func (s *Store) ListArchivedFeatures() ([]ArchiveFeature, error) {
	rows, err := s.db.Reader().Query(`SELECT feature_name, archived_at FROM archive_features ORDER BY archived_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list archived: %w", err)
	}
	defer rows.Close()
	var out []ArchiveFeature
	for rows.Next() {
		var a ArchiveFeature
		if err := rows.Scan(&a.FeatureName, &a.ArchivedAt); err != nil {
			return nil, fmt.Errorf("scan archive: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- Benchmark ---

func (s *Store) BenchmarkSelf() (insertAvg, searchAvg, contextAvg float64, err error) {
	w := s.db.Writer()
	r := s.db.Reader()

	// Create a temp feature for benchmarking
	benchID := uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)
	if _, err = w.Exec(`INSERT INTO features (id, name, description, status, created_at, last_active) VALUES (?, ?, '', 'active', ?, ?)`, benchID, "_bench_"+benchID[:8], now, now); err != nil {
		return 0, 0, 0, fmt.Errorf("create bench feature: %w", err)
	}
	defer func() {
		w.Exec(`DELETE FROM notes WHERE feature_id = ?`, benchID)
		w.Exec(`DELETE FROM sessions WHERE feature_id = ?`, benchID)
		w.Exec(`DELETE FROM features WHERE id = ?`, benchID)
	}()

	// Benchmark inserts (100 notes)
	start := time.Now()
	for i := 0; i < 100; i++ {
		id := uuid.New().String()
		w.Exec(`INSERT INTO notes (id, feature_id, content, type, created_at, updated_at) VALUES (?, ?, ?, 'note', ?, ?)`, id, benchID, fmt.Sprintf("Benchmark note %d", i), now, now)
	}
	insertAvg = float64(time.Since(start).Microseconds()) / 100.0 / 1000.0

	// Benchmark searches (100 queries)
	start = time.Now()
	for i := 0; i < 100; i++ {
		r.QueryRow(`SELECT COUNT(*) FROM notes WHERE feature_id = ? AND content LIKE ?`, benchID, fmt.Sprintf("%%note %d%%", i%100))
	}
	searchAvg = float64(time.Since(start).Microseconds()) / 100.0 / 1000.0

	// Benchmark context loads (100 reads)
	start = time.Now()
	for i := 0; i < 100; i++ {
		r.QueryRow(`SELECT COUNT(*) FROM notes WHERE feature_id = ?`, benchID)
		r.QueryRow(`SELECT COUNT(*) FROM facts WHERE feature_id = ?`, benchID)
	}
	contextAvg = float64(time.Since(start).Microseconds()) / 100.0 / 1000.0

	return insertAvg, searchAvg, contextAvg, nil
}

// --- Doctor ---

type DoctorCheck struct {
	Name   string
	Passed bool
	Detail string
}

func (s *Store) Doctor() ([]DoctorCheck, error) {
	var checks []DoctorCheck

	// Check 1: DB exists
	if _, err := os.Stat(s.db.Path()); err == nil {
		checks = append(checks, DoctorCheck{"DB exists", true, s.db.Path()})
	} else {
		checks = append(checks, DoctorCheck{"DB exists", false, "Database file not found"})
	}

	// Check 2: Migrations current
	r := s.db.Reader()
	var maxVersion int
	if err := r.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&maxVersion); err == nil {
		if maxVersion >= 8 {
			checks = append(checks, DoctorCheck{"Migrations", true, fmt.Sprintf("v%d", maxVersion)})
		} else {
			checks = append(checks, DoctorCheck{"Migrations", false, fmt.Sprintf("v%d (expected >= 8)", maxVersion)})
		}
	} else {
		checks = append(checks, DoctorCheck{"Migrations", false, "Cannot read schema_version"})
	}

	// Check 3: FTS indexes
	for _, fts := range []string{"notes_fts", "commits_fts", "facts_fts"} {
		var name string
		if err := r.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", fts).Scan(&name); err == nil {
			checks = append(checks, DoctorCheck{fts, true, "OK"})
		} else {
			checks = append(checks, DoctorCheck{fts, false, "Missing"})
		}
	}

	// Check 4: WAL mode
	var journalMode string
	if err := r.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err == nil {
		if strings.ToLower(journalMode) == "wal" {
			checks = append(checks, DoctorCheck{"WAL mode", true, "Enabled"})
		} else {
			checks = append(checks, DoctorCheck{"WAL mode", false, journalMode})
		}
	}

	// Check 5: Integrity
	var intResult string
	if err := r.QueryRow("PRAGMA integrity_check").Scan(&intResult); err == nil {
		if intResult == "ok" {
			checks = append(checks, DoctorCheck{"Integrity", true, "No corruption"})
		} else {
			checks = append(checks, DoctorCheck{"Integrity", false, intResult})
		}
	}

	return checks, nil
}

// --- Config ---

func (s *Store) SetConfig(key, value string) error {
	_, err := s.db.Writer().Exec(
		`INSERT INTO config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) GetConfig(key string) (string, error) {
	var val string
	err := s.db.Reader().QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("config key %q not set", key)
	}
	return val, err
}

func (s *Store) ListConfig() ([]ConfigEntry, error) {
	rows, err := s.db.Reader().Query(`SELECT key, value FROM config ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list config: %w", err)
	}
	defer rows.Close()
	var out []ConfigEntry
	for rows.Next() {
		var e ConfigEntry
		if err := rows.Scan(&e.Key, &e.Value); err != nil {
			return nil, fmt.Errorf("scan config: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
