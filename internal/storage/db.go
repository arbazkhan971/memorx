package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const pragmas = "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"

type DB struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

func openDB(dsn string, maxConns int) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	return db, nil
}

func NewDB(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	writer, err := openDB(dbPath+"?"+pragmas+"&_pragma=synchronous(NORMAL)", 1)
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}

	reader, err := openDB(dbPath+"?"+pragmas, 4)
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}

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
	return errors.Join(db.reader.Close(), db.writer.Close())
}
