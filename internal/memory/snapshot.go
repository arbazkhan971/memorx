package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Snapshot holds the complete current state for human-readable JSON output.
type Snapshot struct {
	Project       string                `json:"project"`
	ProjectPath   string                `json:"project_path"`
	ActiveFeature *FeatureSnapshot      `json:"active_feature"`
	ActivePlan    *PlanSnapshot         `json:"active_plan"`
	Features      []FeatureSnapshot     `json:"features"`
	Consolidation ConsolidationSnapshot `json:"consolidation"`
}

// FeatureSnapshot holds lightweight feature info for the snapshot.
type FeatureSnapshot struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Branch      string `json:"branch"`
	Description string `json:"description"`
	LastActive  string `json:"last_active"`
	Sessions    int    `json:"sessions"`
	Commits     int    `json:"commits"`
}

// PlanSnapshot holds lightweight plan info for the snapshot.
type PlanSnapshot struct {
	Title       string `json:"title"`
	Progress    string `json:"progress"`
	CurrentStep string `json:"current_step"`
	StepsDone   int    `json:"steps_done"`
	StepsTotal  int    `json:"steps_total"`
}

// ConsolidationSnapshot holds consolidation engine state for the snapshot.
type ConsolidationSnapshot struct {
	EntropyScore      float64 `json:"entropy_score"`
	UnsummarizedCount int     `json:"unsummarized_count"`
	LastRun           string  `json:"last_run"`
}

// WriteSnapshot reads the current state from the database and writes
// a pretty-printed JSON file to <memDir>/current.json.
func (s *Store) WriteSnapshot(memDir, projectName, projectPath string) error {
	r := s.db.Reader()

	snap := Snapshot{
		Project:     projectName,
		ProjectPath: projectPath,
	}

	// Load all features
	features, err := s.ListFeatures("all")
	if err != nil {
		return fmt.Errorf("snapshot list features: %w", err)
	}

	for _, f := range features {
		fs := FeatureSnapshot{
			Name:        f.Name,
			Status:      f.Status,
			Branch:      f.Branch,
			Description: f.Description,
			LastActive:  f.LastActive,
			Sessions:    countRows(r, `SELECT COUNT(*) FROM sessions WHERE feature_id = ?`, f.ID),
			Commits:     countRows(r, `SELECT COUNT(*) FROM commits WHERE feature_id = ?`, f.ID),
		}
		snap.Features = append(snap.Features, fs)

		if f.Status == "active" {
			active := fs
			snap.ActiveFeature = &active
		}
	}

	// Load active plan (for the active feature)
	if snap.ActiveFeature != nil {
		var activeFeatureID string
		err := r.QueryRow(`SELECT id FROM features WHERE name = ?`, snap.ActiveFeature.Name).Scan(&activeFeatureID)
		if err == nil {
			snap.ActivePlan = loadPlanSnapshot(r, activeFeatureID)
		}
	}

	// Load consolidation state
	snap.Consolidation = loadConsolidationSnapshot(r)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("snapshot marshal: %w", err)
	}

	outPath := filepath.Join(memDir, "current.json")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("snapshot write: %w", err)
	}

	return nil
}

// countRows executes a COUNT(*) query and returns the result.
func countRows(r *sql.DB, query string, args ...interface{}) int {
	var count int
	if err := r.QueryRow(query, args...).Scan(&count); err != nil {
		return 0
	}
	return count
}

// loadPlanSnapshot loads the active plan snapshot for a feature.
func loadPlanSnapshot(r *sql.DB, featureID string) *PlanSnapshot {
	var planID, title string
	err := r.QueryRow(
		`SELECT id, title FROM plans WHERE feature_id = ? AND status = 'active' AND invalid_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		featureID,
	).Scan(&planID, &title)
	if err != nil {
		return nil
	}

	var total, done int
	r.QueryRow(`SELECT COUNT(*) FROM plan_steps WHERE plan_id = ?`, planID).Scan(&total)
	r.QueryRow(`SELECT COUNT(*) FROM plan_steps WHERE plan_id = ? AND status = 'completed'`, planID).Scan(&done)

	// Find the first non-completed step as current
	var currentStep string
	err = r.QueryRow(
		`SELECT title FROM plan_steps WHERE plan_id = ? AND status != 'completed' ORDER BY step_number LIMIT 1`,
		planID,
	).Scan(&currentStep)
	if err != nil {
		currentStep = ""
	}

	progress := fmt.Sprintf("%d/%d", done, total)

	return &PlanSnapshot{
		Title:       title,
		Progress:    progress,
		CurrentStep: currentStep,
		StepsDone:   done,
		StepsTotal:  total,
	}
}

// loadConsolidationSnapshot loads the consolidation state from the database.
func loadConsolidationSnapshot(r *sql.DB) ConsolidationSnapshot {
	cs := ConsolidationSnapshot{}
	var lastRun sql.NullString

	err := r.QueryRow(
		`SELECT entropy_score, unsummarized_count, COALESCE(last_run_at, '') FROM consolidation_state WHERE id = 1`,
	).Scan(&cs.EntropyScore, &cs.UnsummarizedCount, &lastRun)
	if err != nil {
		return cs
	}

	cs.LastRun = lastRun.String
	return cs
}
