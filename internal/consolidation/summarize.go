package consolidation

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type noteRecord struct {
	id, content, noteType, createdAt string
}

type summaryRecord struct {
	id, content, coversFrom, coversTo string
}

func (e *Engine) GenerateSummaries(featureID string) (int, error) {
	summariesCreated := 0
	scope := "feature:" + featureID

	unsummarized, err := e.getUnsummarizedNotes(featureID)
	if err != nil {
		return 0, err
	}
	if len(unsummarized) >= e.cfg.MaxUnsummarized {
		notes := unsummarized
		if len(notes) > e.cfg.MaxUnsummarized {
			notes = notes[:e.cfg.MaxUnsummarized]
		}
		coversFrom, coversTo := dateRange(notes)
		if err := e.insertSummary(scope, buildSummaryContent(notes), 0, coversFrom, coversTo); err != nil {
			return 0, fmt.Errorf("insert gen-0 summary: %w", err)
		}
		summariesCreated++
	}

	gen0Summaries, err := e.getSummariesByGeneration(scope, 0)
	if err != nil {
		return summariesCreated, err
	}
	if len(gen0Summaries) >= 5 {
		toRoll := gen0Summaries[:5]
		var parts []string
		allFrom, allTo := toRoll[0].coversFrom, toRoll[0].coversTo
		for _, s := range toRoll {
			parts = append(parts, s.content)
			if s.coversFrom < allFrom {
				allFrom = s.coversFrom
			}
			if s.coversTo > allTo {
				allTo = s.coversTo
			}
		}
		if err := e.insertSummary(scope, truncate(strings.Join(parts, "\n\n---\n\n"), 2000), 1, allFrom, allTo); err != nil {
			return summariesCreated, fmt.Errorf("insert gen-1 summary: %w", err)
		}
		summariesCreated++
	}
	return summariesCreated, nil
}

func (e *Engine) getUnsummarizedNotes(featureID string) ([]noteRecord, error) {
	rows, err := e.db.Reader().Query(
		`SELECT n.id, n.content, n.type, n.created_at FROM notes n
		 WHERE n.feature_id=? AND NOT EXISTS (
			SELECT 1 FROM summaries s WHERE s.scope='feature:'||n.feature_id AND s.covers_from<=n.created_at AND s.covers_to>=n.created_at
		 ) ORDER BY n.created_at DESC`, featureID,
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

func (e *Engine) getSummariesByGeneration(scope string, generation int) ([]summaryRecord, error) {
	rows, err := e.db.Reader().Query(
		`SELECT id, content, covers_from, covers_to FROM summaries WHERE scope=? AND generation=? ORDER BY covers_from ASC`,
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

func (e *Engine) insertSummary(scope, content string, generation int, coversFrom, coversTo string) error {
	_, err := e.db.Writer().Exec(
		`INSERT INTO summaries (id, scope, content, generation, token_count, covers_from, covers_to, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New().String(), scope, content, generation, len(content)/4, coversFrom, coversTo, time.Now().UTC().Format(time.DateTime),
	)
	return err
}

func buildSummaryContent(notes []noteRecord) string {
	typeOrder := map[string]int{"decision": 0, "blocker": 1, "progress": 2, "next_step": 3, "note": 4}
	sorted := make([]noteRecord, len(notes))
	copy(sorted, notes)
	sort.Slice(sorted, func(i, j int) bool {
		if ti, tj := typeOrder[sorted[i].noteType], typeOrder[sorted[j].noteType]; ti != tj {
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
				sections = append(sections, fmt.Sprintf("[%s]\n%s", strings.ToUpper(currentType), strings.Join(currentNotes, "\n")))
			}
			currentType = n.noteType
			currentNotes = nil
		}
		currentNotes = append(currentNotes, "- "+n.content)
	}
	if len(currentNotes) > 0 {
		sections = append(sections, fmt.Sprintf("[%s]\n%s", strings.ToUpper(currentType), strings.Join(currentNotes, "\n")))
	}
	return truncate(strings.Join(sections, "\n\n"), 2000)
}

func dateRange(notes []noteRecord) (string, string) {
	if len(notes) == 0 {
		now := time.Now().UTC().Format(time.DateTime)
		return now, now
	}
	minDate, maxDate := notes[0].createdAt, notes[0].createdAt
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

func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "..."
}
