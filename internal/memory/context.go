package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// Context holds assembled context for a feature at a given tier.
type Context struct {
	Feature        *Feature
	Summary        string
	Plan           *PlanInfo
	RecentCommits  []CommitInfo
	RecentNotes    []Note
	ActiveFacts    []Fact
	Links          []MemoryLink
	SessionHistory []Session
	FilesTouched   []string
}

// PlanInfo holds lightweight plan data for context assembly.
type PlanInfo struct {
	Title         string
	Status        string
	TotalSteps    int
	CompletedStep int
}

// CommitInfo holds lightweight commit data for context assembly.
type CommitInfo struct {
	Hash      string
	Message   string
	Author    string
	CommittedAt string
}

// GetContext assembles context for a feature at the specified tier.
// Tiers: "compact", "standard", "detailed"
func (s *Store) GetContext(featureID, tier string, asOf *time.Time) (*Context, error) {
	ctx := &Context{}
	r := s.db.Reader()

	// Always load the feature
	feature := &Feature{}
	err := r.QueryRow(
		`SELECT id, name, description, status, COALESCE(branch, ''), created_at, last_active
		 FROM features WHERE id = ?`, featureID,
	).Scan(&feature.ID, &feature.Name, &feature.Description, &feature.Status, &feature.Branch, &feature.CreatedAt, &feature.LastActive)
	if err != nil {
		return nil, fmt.Errorf("get feature for context: %w", err)
	}
	ctx.Feature = feature

	// Load summary (if any)
	var summary string
	err = r.QueryRow(
		`SELECT content FROM summaries WHERE scope = ? ORDER BY generation DESC, created_at DESC LIMIT 1`,
		featureID,
	).Scan(&summary)
	if err == nil {
		ctx.Summary = summary
	}

	// Load plan progress
	ctx.Plan = s.loadPlanInfo(r, featureID)

	switch tier {
	case "compact":
		// Summary + last commit + plan progress
		ctx.RecentCommits = s.loadRecentCommits(r, featureID, 1)

	case "standard":
		// Above + last 5 commits + last 3 notes + active facts
		ctx.RecentCommits = s.loadRecentCommits(r, featureID, 5)
		notes, _ := s.ListNotes(featureID, "", 3)
		ctx.RecentNotes = notes
		if asOf != nil {
			ctx.ActiveFacts, _ = s.QueryFactsAsOf(featureID, *asOf)
		} else {
			ctx.ActiveFacts, _ = s.GetActiveFacts(featureID)
		}

	case "detailed":
		// Above + all decisions + session history + linked memories
		ctx.RecentCommits = s.loadRecentCommits(r, featureID, 100)
		notes, _ := s.ListNotes(featureID, "", 100)
		ctx.RecentNotes = notes
		if asOf != nil {
			ctx.ActiveFacts, _ = s.QueryFactsAsOf(featureID, *asOf)
		} else {
			ctx.ActiveFacts, _ = s.GetActiveFacts(featureID)
		}
		ctx.SessionHistory, _ = s.ListSessions(featureID, 100)
		ctx.Links = s.loadFeatureLinks(r, featureID)
		ctx.FilesTouched = s.loadFilesTouched(r, featureID)

	default:
		return nil, fmt.Errorf("unknown context tier: %q (valid: compact, standard, detailed)", tier)
	}

	return ctx, nil
}

// loadPlanInfo loads the active plan info for a feature.
func (s *Store) loadPlanInfo(r *sql.DB, featureID string) *PlanInfo {
	pi := &PlanInfo{}
	err := r.QueryRow(
		`SELECT title, status FROM plans WHERE feature_id = ? AND status = 'active' ORDER BY created_at DESC LIMIT 1`,
		featureID,
	).Scan(&pi.Title, &pi.Status)
	if err != nil {
		return nil
	}

	// Count steps
	var planID string
	err = r.QueryRow(
		`SELECT id FROM plans WHERE feature_id = ? AND status = 'active' ORDER BY created_at DESC LIMIT 1`,
		featureID,
	).Scan(&planID)
	if err != nil {
		return pi
	}

	r.QueryRow(`SELECT COUNT(*) FROM plan_steps WHERE plan_id = ?`, planID).Scan(&pi.TotalSteps)
	r.QueryRow(`SELECT COUNT(*) FROM plan_steps WHERE plan_id = ? AND status = 'completed'`, planID).Scan(&pi.CompletedStep)

	return pi
}

// loadRecentCommits loads recent commits for a feature.
func (s *Store) loadRecentCommits(r *sql.DB, featureID string, limit int) []CommitInfo {
	rows, err := r.Query(
		`SELECT hash, message, author, committed_at
		 FROM commits WHERE feature_id = ?
		 ORDER BY committed_at DESC LIMIT ?`,
		featureID, limit,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var commits []CommitInfo
	for rows.Next() {
		var c CommitInfo
		if err := rows.Scan(&c.Hash, &c.Message, &c.Author, &c.CommittedAt); err != nil {
			continue
		}
		commits = append(commits, c)
	}
	return commits
}

// loadFeatureLinks loads all links related to items belonging to a feature.
func (s *Store) loadFeatureLinks(r *sql.DB, featureID string) []MemoryLink {
	// Get links from notes belonging to this feature
	rows, err := r.Query(
		`SELECT ml.id, ml.source_id, ml.source_type, ml.target_id, ml.target_type,
		        ml.relationship, ml.strength, ml.created_at
		 FROM memory_links ml
		 WHERE ml.source_id IN (
			SELECT id FROM notes WHERE feature_id = ?
			UNION SELECT id FROM facts WHERE feature_id = ?
			UNION SELECT id FROM commits WHERE feature_id = ?
		 )
		 ORDER BY ml.strength DESC, ml.created_at DESC
		 LIMIT 50`,
		featureID, featureID, featureID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var links []MemoryLink
	for rows.Next() {
		var l MemoryLink
		if err := rows.Scan(&l.ID, &l.SourceID, &l.SourceType, &l.TargetID, &l.TargetType,
			&l.Relationship, &l.Strength, &l.CreatedAt); err != nil {
			continue
		}
		links = append(links, l)
	}
	return links
}

// loadFilesTouched loads unique files changed in commits for a feature.
func (s *Store) loadFilesTouched(r *sql.DB, featureID string) []string {
	rows, err := r.Query(
		`SELECT DISTINCT file_path FROM semantic_changes sc
		 JOIN commits c ON sc.commit_hash = c.hash
		 WHERE c.feature_id = ?
		 ORDER BY file_path`,
		featureID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			continue
		}
		files = append(files, f)
	}
	return files
}
