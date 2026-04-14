package hooks

import (
	"os"
	"path/filepath"
	"time"

	gitpkg "github.com/arbazkhan971/memorx/internal/git"
	"github.com/arbazkhan971/memorx/internal/memory"
	"github.com/arbazkhan971/memorx/internal/storage"
)

// SyncCommits pulls any new git commits since the last sync and records
// them into memorX. Returns the number of new commits ingested.
//
// This wraps git.SyncCommits so hooks can call it with just a Store,
// without needing to reach into storage internals.
func SyncCommits(store *memory.Store, gitRoot, featureID, sessionID string) (int, error) {
	db := extractDB(store)
	if db == nil {
		return 0, nil
	}
	// Default to "last 7 days" — matches memorx_sync default.
	since := time.Now().Add(-7 * 24 * time.Hour)
	res, err := gitpkg.SyncCommits(db, gitRoot, featureID, sessionID, since)
	if err != nil {
		return 0, err
	}
	return res.NewCommits, nil
}

// extractDB is a small accessor so cmd/devmem doesn't need to reach into
// the memory.Store struct directly. The store already holds a *storage.DB
// internally; we just need to expose it via a tiny accessor.
func extractDB(store *memory.Store) *storage.DB {
	return store.DB()
}

// TranscriptDir returns the conventional directory where Claude Code
// stores JSONL session transcripts for the given git root.
func TranscriptDir(gitRoot string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}
