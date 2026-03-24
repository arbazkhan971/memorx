package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Wave 12: Doc Automation ---

// ADR represents an Architecture Decision Record.
type ADR struct {
	Title        string
	Date         string
	Status       string
	Context      string
	Decision     string
	Consequences string
}

// GenerateADR creates Architecture Decision Records from decision notes.
// If decisionID is non-empty, generates a single ADR; otherwise generates all.
func (s *Store) GenerateADR(decisionID string) (string, error) {
	r := s.db.Reader()
	var b strings.Builder

	if decisionID != "" {
		// Single ADR
		adr, err := s.buildADR(r, decisionID)
		if err != nil {
			return "", err
		}
		b.WriteString(formatADR(adr))
		return b.String(), nil
	}

	// All decisions
	decisions := scanRows(r,
		`SELECT id, content, created_at FROM notes WHERE type = 'decision' ORDER BY created_at ASC`,
		nil,
		func(rows *sql.Rows) ([3]string, error) {
			var d [3]string
			return d, rows.Scan(&d[0], &d[1], &d[2])
		},
	)

	if len(decisions) == 0 {
		return "# Architecture Decision Records\n\n_No decisions recorded. Use memorx_remember with type=\"decision\" to start recording decisions._\n", nil
	}

	b.WriteString("# Architecture Decision Records\n\n")
	for i, d := range decisions {
		adr, err := s.buildADR(r, d[0])
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "## ADR-%03d: %s\n\n", i+1, adr.Title)
		b.WriteString(formatADRBody(adr))
		b.WriteString("---\n\n")
	}

	return b.String(), nil
}

func (s *Store) buildADR(r *sql.DB, noteID string) (*ADR, error) {
	var content, createdAt, featureID string
	err := r.QueryRow(`SELECT content, created_at, feature_id FROM notes WHERE id = ? AND type = 'decision'`, noteID).
		Scan(&content, &createdAt, &featureID)
	if err != nil {
		return nil, fmt.Errorf("decision %q not found: %w", noteID, err)
	}

	adr := &ADR{
		Title:    extractTitle(content),
		Date:     createdAt,
		Status:   "Accepted",
		Decision: content,
	}

	// Build context from related facts and notes.
	var contextParts []string
	facts := scanRows(r,
		`SELECT subject, predicate, object FROM facts WHERE feature_id = ? AND invalid_at IS NULL`,
		[]any{featureID},
		func(rows *sql.Rows) (string, error) {
			var s, p, o string
			if err := rows.Scan(&s, &p, &o); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s %s %s", s, p, o), nil
		},
	)
	if len(facts) > 0 {
		contextParts = append(contextParts, "Known facts: "+strings.Join(facts, "; "))
	}

	relatedNotes := scanRows(r,
		`SELECT content FROM notes WHERE feature_id = ? AND type = 'note' AND id != ? ORDER BY created_at DESC LIMIT 5`,
		[]any{featureID, noteID},
		func(rows *sql.Rows) (string, error) {
			var c string
			return c, rows.Scan(&c)
		},
	)
	if len(relatedNotes) > 0 {
		contextParts = append(contextParts, "Related notes: "+strings.Join(relatedNotes, "; "))
	}

	if len(contextParts) > 0 {
		adr.Context = strings.Join(contextParts, "\n\n")
	} else {
		adr.Context = "No additional context recorded."
	}

	// Build consequences from blockers and progress notes.
	var consequences []string
	blockers := scanRows(r,
		`SELECT content FROM notes WHERE feature_id = ? AND type = 'blocker' ORDER BY created_at DESC LIMIT 5`,
		[]any{featureID},
		func(rows *sql.Rows) (string, error) {
			var c string
			return c, rows.Scan(&c)
		},
	)
	for _, bl := range blockers {
		consequences = append(consequences, "Blocker: "+bl)
	}

	progress := scanRows(r,
		`SELECT content FROM notes WHERE feature_id = ? AND type = 'progress' ORDER BY created_at DESC LIMIT 5`,
		[]any{featureID},
		func(rows *sql.Rows) (string, error) {
			var c string
			return c, rows.Scan(&c)
		},
	)
	for _, pr := range progress {
		consequences = append(consequences, "Progress: "+pr)
	}

	if len(consequences) > 0 {
		adr.Consequences = strings.Join(consequences, "\n")
	} else {
		adr.Consequences = "No consequences recorded yet."
	}

	return adr, nil
}

func extractTitle(content string) string {
	lines := strings.SplitN(content, "\n", 2)
	title := strings.TrimSpace(lines[0])
	title = strings.TrimLeft(title, "# ")
	if len(title) > 80 {
		title = title[:80] + "..."
	}
	if title == "" {
		title = "Untitled Decision"
	}
	return title
}

func formatADR(adr *ADR) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# ADR: %s\n\n", adr.Title)
	b.WriteString(formatADRBody(adr))
	return b.String()
}

func formatADRBody(adr *ADR) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Date:** %s\n", adr.Date)
	fmt.Fprintf(&b, "**Status:** %s\n\n", adr.Status)
	fmt.Fprintf(&b, "### Context\n\n%s\n\n", adr.Context)
	fmt.Fprintf(&b, "### Decision\n\n%s\n\n", adr.Decision)
	fmt.Fprintf(&b, "### Consequences\n\n%s\n\n", adr.Consequences)
	return b.String()
}

// GenerateReadme creates or updates a README from project map + memory.
func (s *Store) GenerateReadme(gitRoot, output string) (string, error) {
	if output == "" {
		output = filepath.Join(gitRoot, "README.md")
	}

	var b strings.Builder
	r := s.db.Reader()

	// Project name.
	projectName := filepath.Base(gitRoot)
	pm, pmErr := s.GetProjectMap()
	if pmErr == nil && pm != nil {
		projectName = filepath.Base(pm.Root)
	}

	b.WriteString(fmt.Sprintf("# %s\n\n", projectName))

	// Tech stack from facts and project map.
	b.WriteString("## Tech Stack\n\n")
	techWritten := false
	if pm != nil && len(pm.Languages) > 0 {
		for lang, count := range pm.Languages {
			fmt.Fprintf(&b, "- %s (%d files)\n", lang, count)
			techWritten = true
		}
	}
	techFacts := scanRows(r,
		`SELECT DISTINCT subject, predicate, object FROM facts WHERE invalid_at IS NULL AND (predicate = 'uses' OR predicate = 'runs_on' OR predicate = 'built_with' OR predicate = 'framework')`,
		nil,
		func(rows *sql.Rows) (string, error) {
			var s, p, o string
			if err := rows.Scan(&s, &p, &o); err != nil {
				return "", err
			}
			return fmt.Sprintf("- %s %s %s", s, p, o), nil
		},
	)
	for _, tf := range techFacts {
		b.WriteString(tf + "\n")
		techWritten = true
	}
	if !techWritten {
		b.WriteString("_Not yet documented._\n")
	}
	b.WriteString("\n")

	// Architecture from project map.
	if pm != nil && len(pm.Directories) > 0 {
		b.WriteString("## Architecture\n\n")
		b.WriteString("```\n")
		for _, d := range pm.Directories {
			fmt.Fprintf(&b, "%s/\n", d)
		}
		b.WriteString("```\n\n")
	}

	// Features.
	features, _ := s.ListFeatures("all")
	if len(features) > 0 {
		b.WriteString("## Features\n\n")
		for _, f := range features {
			status := f.Status
			if status == "done" {
				status = "completed"
			}
			fmt.Fprintf(&b, "- **%s** [%s]", f.Name, status)
			if f.Description != "" {
				fmt.Fprintf(&b, " - %s", f.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Recent changes.
	commits := scanRows(r,
		`SELECT hash, message, committed_at FROM commits ORDER BY committed_at DESC LIMIT 5`,
		nil,
		func(rows *sql.Rows) (CommitInfo, error) {
			var c CommitInfo
			return c, rows.Scan(&c.Hash, &c.Message, &c.CommittedAt)
		},
	)
	if len(commits) > 0 {
		b.WriteString("## Recent Changes\n\n")
		for _, c := range commits {
			hash := c.Hash
			if len(hash) > 7 {
				hash = hash[:7]
			}
			fmt.Fprintf(&b, "- `%s` %s\n", hash, c.Message)
		}
		b.WriteString("\n")
	}

	// Setup instructions.
	b.WriteString("## Setup\n\n")
	setupFacts := scanRows(r,
		`SELECT object FROM facts WHERE invalid_at IS NULL AND (subject = 'setup' OR subject = 'install' OR predicate = 'requires' OR predicate = 'setup')`,
		nil,
		func(rows *sql.Rows) (string, error) {
			var o string
			return o, rows.Scan(&o)
		},
	)
	if len(setupFacts) > 0 {
		for _, sf := range setupFacts {
			fmt.Fprintf(&b, "- %s\n", sf)
		}
	} else {
		b.WriteString("_Add setup instructions by recording facts with memorx_remember._\n")
	}
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("---\n_Generated by memorX on %s_\n", time.Now().UTC().Format("2006-01-02")))

	content := b.String()

	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(output, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write README: %w", err)
	}

	return content, nil
}

// GenerateAPIDocs searches notes and facts for API-related content and formats as docs.
func (s *Store) GenerateAPIDocs() (string, error) {
	r := s.db.Reader()
	var b strings.Builder

	b.WriteString("# API Documentation\n\n")
	b.WriteString(fmt.Sprintf("_Auto-generated from memory on %s_\n\n", time.Now().UTC().Format("2006-01-02")))

	apiKeywords := []string{"endpoint", "route", "API", "GET", "POST", "PUT", "DELETE", "PATCH", "handler"}

	// Search notes for API-related content.
	var conditions []string
	for _, kw := range apiKeywords {
		conditions = append(conditions, fmt.Sprintf("content LIKE '%%%s%%'", kw))
	}
	noteQuery := fmt.Sprintf(
		`SELECT id, content, type, created_at FROM notes WHERE (%s) ORDER BY created_at ASC`,
		strings.Join(conditions, " OR "),
	)

	type apiNote struct {
		id, content, noteType, createdAt string
	}
	notes := scanRows(r, noteQuery, nil, func(rows *sql.Rows) (apiNote, error) {
		var n apiNote
		return n, rows.Scan(&n.id, &n.content, &n.noteType, &n.createdAt)
	})

	// Search facts for API-related content.
	var factConditions []string
	for _, kw := range apiKeywords {
		factConditions = append(factConditions, fmt.Sprintf("subject LIKE '%%%s%%' OR predicate LIKE '%%%s%%' OR object LIKE '%%%s%%'", kw, kw, kw))
	}
	factQuery := fmt.Sprintf(
		`SELECT id, subject, predicate, object FROM facts WHERE invalid_at IS NULL AND (%s) ORDER BY recorded_at ASC`,
		strings.Join(factConditions, " OR "),
	)

	type apiFact struct {
		id, subject, predicate, object string
	}
	facts := scanRows(r, factQuery, nil, func(rows *sql.Rows) (apiFact, error) {
		var f apiFact
		return f, rows.Scan(&f.id, &f.subject, &f.predicate, &f.object)
	})

	if len(notes) == 0 && len(facts) == 0 {
		b.WriteString("_No API-related content found in memory. Record API decisions and facts to populate this document._\n")
		return b.String(), nil
	}

	// Format endpoints from facts.
	if len(facts) > 0 {
		b.WriteString("## Endpoints (from facts)\n\n")
		b.WriteString("| Subject | Relation | Detail |\n")
		b.WriteString("|---------|----------|--------|\n")
		for _, f := range facts {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", f.subject, f.predicate, f.object)
		}
		b.WriteString("\n")
	}

	// Format API notes grouped by type.
	if len(notes) > 0 {
		var decisions, others []apiNote
		for _, n := range notes {
			if n.noteType == "decision" {
				decisions = append(decisions, n)
			} else {
				others = append(others, n)
			}
		}

		if len(decisions) > 0 {
			b.WriteString("## API Decisions\n\n")
			for _, n := range decisions {
				fmt.Fprintf(&b, "- %s _(recorded %s)_\n", strings.ReplaceAll(n.content, "\n", " "), n.createdAt)
			}
			b.WriteString("\n")
		}

		if len(others) > 0 {
			b.WriteString("## API Notes\n\n")
			for _, n := range others {
				fmt.Fprintf(&b, "- [%s] %s\n", n.noteType, strings.ReplaceAll(n.content, "\n", " "))
			}
			b.WriteString("\n")
		}
	}

	return b.String(), nil
}

// GenerateRunbook creates an operational runbook from error_log + decisions.
func (s *Store) GenerateRunbook(feature string) (string, error) {
	r := s.db.Reader()
	var b strings.Builder

	b.WriteString("# Operational Runbook\n\n")
	b.WriteString(fmt.Sprintf("_Auto-generated from memory on %s_\n\n", time.Now().UTC().Format("2006-01-02")))

	// Error log entries with resolutions.
	errorQuery := `SELECT id, error_message, COALESCE(file_path, ''), COALESCE(cause, ''), COALESCE(resolution, ''), resolved, created_at, COALESCE(session_id, '') FROM error_log`
	errorArgs := []any{}
	if feature != "" {
		f, err := s.GetFeature(feature)
		if err == nil {
			errorQuery += ` WHERE feature_id = ?`
			errorArgs = append(errorArgs, f.ID)
		}
	}
	errorQuery += ` ORDER BY created_at DESC`

	type errorEntry struct {
		id, message, filePath, cause, resolution, sessionID, createdAt string
		resolved                                                       int
	}
	errors := scanRows(r, errorQuery, errorArgs, func(rows *sql.Rows) (errorEntry, error) {
		var e errorEntry
		return e, rows.Scan(&e.id, &e.message, &e.filePath, &e.cause, &e.resolution, &e.resolved, &e.createdAt, &e.sessionID)
	})

	// Related decisions (blocker notes).
	blockerQuery := `SELECT content, created_at FROM notes WHERE type = 'blocker'`
	blockerArgs := []any{}
	if feature != "" {
		f, err := s.GetFeature(feature)
		if err == nil {
			blockerQuery += ` AND feature_id = ?`
			blockerArgs = append(blockerArgs, f.ID)
		}
	}
	blockerQuery += ` ORDER BY created_at DESC LIMIT 20`

	type blockerEntry struct {
		content, createdAt string
	}
	blockers := scanRows(r, blockerQuery, blockerArgs, func(rows *sql.Rows) (blockerEntry, error) {
		var bl blockerEntry
		return bl, rows.Scan(&bl.content, &bl.createdAt)
	})

	// Decision notes for resolution context.
	decisionQuery := `SELECT content, created_at FROM notes WHERE type = 'decision'`
	decisionArgs := []any{}
	if feature != "" {
		f, err := s.GetFeature(feature)
		if err == nil {
			decisionQuery += ` AND feature_id = ?`
			decisionArgs = append(decisionArgs, f.ID)
		}
	}
	decisionQuery += ` ORDER BY created_at DESC LIMIT 20`

	type decisionEntry struct {
		content, createdAt string
	}
	relDecisions := scanRows(r, decisionQuery, decisionArgs, func(rows *sql.Rows) (decisionEntry, error) {
		var d decisionEntry
		return d, rows.Scan(&d.content, &d.createdAt)
	})

	if len(errors) == 0 && len(blockers) == 0 && len(relDecisions) == 0 {
		b.WriteString("_No error logs, blockers, or decisions found. The runbook will populate as you record errors and decisions._\n")
		return b.String(), nil
	}

	// Format errors as runbook entries.
	if len(errors) > 0 {
		b.WriteString("## Error Resolution Guide\n\n")
		for i, e := range errors {
			fmt.Fprintf(&b, "### %d. %s\n\n", i+1, truncateStr(e.message, 100))
			if e.filePath != "" {
				fmt.Fprintf(&b, "**File:** `%s`\n", e.filePath)
			}
			fmt.Fprintf(&b, "**Recorded:** %s\n", e.createdAt)

			status := "Unresolved"
			if e.resolved == 1 {
				status = "Resolved"
			}
			fmt.Fprintf(&b, "**Status:** %s\n\n", status)

			if e.cause != "" {
				fmt.Fprintf(&b, "**Cause:** %s\n\n", e.cause)
			}
			if e.resolution != "" {
				fmt.Fprintf(&b, "**Resolution:** %s\n\n", e.resolution)
			}
			if e.resolved == 1 && e.resolution != "" {
				fmt.Fprintf(&b, "> If this error recurs -> %s (from error #%s, session %s)\n\n",
					e.resolution, safePrefix(e.id, 8), safePrefix(e.sessionID, 8))
			}
		}
	}

	// Format blockers.
	if len(blockers) > 0 {
		b.WriteString("## Known Blockers\n\n")
		for _, bl := range blockers {
			fmt.Fprintf(&b, "- [%s] %s\n", bl.createdAt, strings.ReplaceAll(bl.content, "\n", " "))
		}
		b.WriteString("\n")
	}

	// Format related decisions.
	if len(relDecisions) > 0 {
		b.WriteString("## Related Decisions\n\n")
		for _, d := range relDecisions {
			fmt.Fprintf(&b, "- [%s] %s\n", d.createdAt, strings.ReplaceAll(d.content, "\n", " "))
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
