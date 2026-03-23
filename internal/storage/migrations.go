package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

// Migrate applies all pending schema migrations.
func Migrate(db *DB) error {
	w := db.Writer()

	// Check current version
	currentVersion := 0
	row := w.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'")
	var name string
	if err := row.Scan(&name); err == nil {
		// schema_version table exists, get current version
		row = w.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
		row.Scan(&currentVersion)
	}

	if currentVersion < 1 {
		if err := applyV1(w); err != nil {
			return fmt.Errorf("apply v1 migration: %w", err)
		}
	}

	return nil
}

func applyV1(w *sql.DB) error {
	tx, err := w.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Apply core schema (uses IF NOT EXISTS)
	stmts := strings.Split(schemaV1, ";")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema statement: %w\nstatement: %s", err, stmt)
		}
	}

	// Apply FTS5 tables (no IF NOT EXISTS support)
	if err := applyFTS(tx); err != nil {
		return fmt.Errorf("apply FTS: %w", err)
	}

	// Record version
	if _, err := tx.Exec("INSERT OR IGNORE INTO schema_version (version) VALUES (1)"); err != nil {
		return fmt.Errorf("record version: %w", err)
	}

	return tx.Commit()
}

func applyFTS(tx *sql.Tx) error {
	stmts := strings.Split(ftsSchemaV1, ";")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		// Check if FTS table already exists
		tableName := extractFTSTableName(stmt)
		if tableName != "" {
			var exists string
			err := tx.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&exists)
			if err == nil {
				continue // table already exists
			}
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("exec FTS statement: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}

func extractFTSTableName(stmt string) string {
	stmt = strings.TrimSpace(strings.ToLower(stmt))
	if !strings.HasPrefix(stmt, "create virtual table") {
		return ""
	}
	parts := strings.Fields(stmt)
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
