package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB manages SQLite connections with WAL mode.
// Single writer + multiple readers for concurrent MCP client access.
type DB struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

func NewDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	writer, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	writer.SetMaxOpenConns(1)

	reader, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	reader.SetMaxOpenConns(4)

	// Verify WAL mode is active
	var journalMode string
	if err := writer.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		writer.Close()
		reader.Close()
		return nil, fmt.Errorf("check journal mode: %w", err)
	}

	return &DB{writer: writer, reader: reader, path: dbPath}, nil
}

func (db *DB) Writer() *sql.DB { return db.writer }
func (db *DB) Reader() *sql.DB { return db.reader }
func (db *DB) Path() string    { return db.path }

func (db *DB) Close() error {
	var firstErr error
	if err := db.reader.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := db.writer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
