package consolidation

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// noteRecord holds a note row used during summarization.
type noteRecord struct {
	id        string
	content   string
	noteType  string
	createdAt string
}

// GenerateSummaries creates heuristic summaries for a feature's unsummarized notes.
// V1 implementation: no LLM, just concatenation and truncation.
// Returns the count of summaries created.
func (e *Engine) GenerateSummaries(featureID string) (int, error) {
	summariesCreated := 0
	scope := "feature:" + featureID

	// Step 1: Count notes for this feature not covered by a summary
	unsummarized, err := e.getUnsummarizedNotes(featureID)
	if err != nil {
		return 0, err
	}

	// Step 2: If enough unsummarized notes, create a gen-0 summary
	if len(unsummarized) >= e.cfg.MaxUnsummarized {
		// Take the last MaxUnsummarized notes (most recent unsummarized)
		notes := unsummarized
		if len(notes) > e.cfg.MaxUnsummarized {
			notes = notes[:e.cfg.MaxUnsummarized]
		}

		content := buildSummaryContent(notes)
		coversFrom, coversTo := dateRange(notes)

		err := e.insertSummary(scope, content, 0, coversFrom, coversTo)
		if err != nil {
			return 0, fmt.Errorf("insert gen-0 summary: %w", err)
		}
		summariesCreated++
	}

	// Step 3: Count gen-0 summaries for this scope
	gen0Summaries, err := e.getSummariesByGeneration(scope, 0)
	if err != nil {
		return summariesCreated, err
	}

	// Step 4: If enough gen-0 summaries, create a gen-1 summary
	if len(gen0Summaries) >= 5 {
		// Take the 5 oldest gen-0 summaries
		toRoll := gen0Summaries[:5]

		var parts []string
		var allFrom, allTo string
		for i, s := range toRoll {
			parts = append(parts, s.content)
			if i == 0 {
				allFrom = s.coversFrom
				allTo = s.coversTo
			}
			if s.coversFrom < allFrom {
				allFrom = s.coversFrom
			}
			if s.coversTo > allTo {
				allTo = s.coversTo
			}
		}

		content := truncate(strings.Join(parts, "\n\n---\n\n"), 2000)
		tokenCount := len(content) / 4 // rough estimate

		id := uuid.New().String()
		now := time.Now().UTC().Format(time.DateTime)
		_, err := e.db.Writer().Exec(
			`INSERT INTO summaries (id, scope, content, generation, token_count, covers_from, covers_to, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, scope, content, 1, tokenCount, allFrom, allTo, now,
		)
		if err != nil {
			return summariesCreated, fmt.Errorf("insert gen-1 summary: %w", err)
		}
		summariesCreated++
	}

	return summariesCreated, nil
}

// getUnsummarizedNotes returns notes for a feature that are not covered by any summary.
// Results are ordered by created_at DESC (most recent first).
func (e *Engine) getUnsummarizedNotes(featureID string) ([]noteRecord, error) {
	rows, err := e.db.Reader().Query(
		`SELECT n.id, n.content, n.type, n.created_at
		 FROM notes n
		 WHERE n.feature_id = ?
		 AND NOT EXISTS (
			SELECT 1 FROM summaries s
			WHERE s.scope = 'feature:' || n.feature_id
			AND s.covers_from <= n.created_at
			AND s.covers_to >= n.created_at
		 )
		 ORDER BY n.created_at DESC`,
		featureID,
	)
	if err != nil {
		return nil, fmt.Errorf("query unsummarized notes: %w", err)
	}
	defer rows.Close()

	var notes []noteRecord
	for rows.Next() {
		var n noteRecord
		if err := rows.Scan(&n.id, &n.content, &n.noteType, &n.createdAt); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// summaryRecord holds a summary row.
type summaryRecord struct {
	id         string
	content    string
	coversFrom string
	coversTo   string
}

// getSummariesByGeneration returns summaries for a scope at a given generation,
// ordered by covers_from ASC (oldest first).
func (e *Engine) getSummariesByGeneration(scope string, generation int) ([]summaryRecord, error) {
	rows, err := e.db.Reader().Query(
		`SELECT id, content, covers_from, covers_to
		 FROM summaries
		 WHERE scope = ? AND generation = ?
		 ORDER BY covers_from ASC`,
		scope, generation,
	)
	if err != nil {
		return nil, fmt.Errorf("query summaries: %w", err)
	}
	defer rows.Close()

	var summaries []summaryRecord
	for rows.Next() {
		var s summaryRecord
		if err := rows.Scan(&s.id, &s.content, &s.coversFrom, &s.coversTo); err != nil {
			return nil, fmt.Errorf("scan summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// insertSummary inserts a new summary record.
func (e *Engine) insertSummary(scope, content string, generation int, coversFrom, coversTo string) error {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)
	tokenCount := len(content) / 4 // rough estimate: ~4 chars per token

	_, err := e.db.Writer().Exec(
		`INSERT INTO summaries (id, scope, content, generation, token_count, covers_from, covers_to, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, scope, content, generation, tokenCount, coversFrom, coversTo, now,
	)
	return err
}

// buildSummaryContent concatenates notes grouped by type and truncates.
// Order: decisions, blockers, progress, next_step, note.
func buildSummaryContent(notes []noteRecord) string {
	typeOrder := map[string]int{
		"decision":  0,
		"blocker":   1,
		"progress":  2,
		"next_step": 3,
		"note":      4,
	}

	// Sort by type priority, then by created_at
	sorted := make([]noteRecord, len(notes))
	copy(sorted, notes)
	sort.Slice(sorted, func(i, j int) bool {
		ti := typeOrder[sorted[i].noteType]
		tj := typeOrder[sorted[j].noteType]
		if ti != tj {
			return ti < tj
		}
		return sorted[i].createdAt < sorted[j].createdAt
	})

	var sections []string
	currentType := ""
	var currentNotes []string

	for _, n := range sorted {
		if n.noteType != currentType {
			if len(currentNotes) > 0 {
				header := strings.ToUpper(currentType)
				sections = append(sections, fmt.Sprintf("[%s]\n%s", header, strings.Join(currentNotes, "\n")))
			}
			currentType = n.noteType
			currentNotes = nil
		}
		currentNotes = append(currentNotes, "- "+n.content)
	}
	if len(currentNotes) > 0 {
		header := strings.ToUpper(currentType)
		sections = append(sections, fmt.Sprintf("[%s]\n%s", header, strings.Join(currentNotes, "\n")))
	}

	content := strings.Join(sections, "\n\n")
	return truncate(content, 2000)
}

// dateRange returns the min and max created_at from a slice of notes.
func dateRange(notes []noteRecord) (string, string) {
	if len(notes) == 0 {
		now := time.Now().UTC().Format(time.DateTime)
		return now, now
	}

	minDate := notes[0].createdAt
	maxDate := notes[0].createdAt
	for _, n := range notes[1:] {
		if n.createdAt < minDate {
			minDate = n.createdAt
		}
		if n.createdAt > maxDate {
			maxDate = n.createdAt
		}
	}
	return minDate, maxDate
}

// truncate cuts a string to approximately maxChars characters.
func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "..."
}
