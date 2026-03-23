package plans

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arbaz/devmem/internal/storage"
	"github.com/google/uuid"
)

// StepInput represents a step to be created with a new plan.
type StepInput struct {
	Title       string
	Description string
}

// Plan represents a development plan for a feature.
type Plan struct {
	ID         string
	FeatureID  string
	SessionID  string
	Title      string
	Content    string
	Status     string
	SourceTool string
	ValidAt    string
	InvalidAt  string
	CreatedAt  string
	UpdatedAt  string
}

// PlanStep represents a single step within a plan.
type PlanStep struct {
	ID            string
	PlanID        string
	Title         string
	Description   string
	Status        string
	CompletedAt   string
	LinkedCommits string
	StepNumber    int
}

const planCols = `id, feature_id, COALESCE(session_id,''), title, content, status, COALESCE(source_tool,'unknown'), COALESCE(valid_at,''), COALESCE(invalid_at,''), created_at, updated_at`

const stepCols = `id, plan_id, title, COALESCE(description, ''), status, COALESCE(completed_at, ''), COALESCE(linked_commits, '[]'), step_number`

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanPlan(s scanner) (Plan, error) {
	var p Plan
	err := s.Scan(&p.ID, &p.FeatureID, &p.SessionID, &p.Title, &p.Content, &p.Status, &p.SourceTool,
		&p.ValidAt, &p.InvalidAt, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func scanStep(s scanner) (PlanStep, error) {
	var st PlanStep
	err := s.Scan(&st.ID, &st.PlanID, &st.Title, &st.Description, &st.Status, &st.CompletedAt, &st.LinkedCommits, &st.StepNumber)
	return st, err
}

// Manager provides plan CRUD operations with bi-temporal versioning.
type Manager struct {
	db *storage.DB
}

// NewManager creates a new Manager backed by the given DB.
func NewManager(db *storage.DB) *Manager {
	return &Manager{db: db}
}

// CreatePlan creates a new plan with steps. If an active plan exists for the
// feature, it is superseded (invalid_at set to now, status set to superseded).
// Completed steps from the old plan are copied to the new plan.
func (m *Manager) CreatePlan(featureID, sessionID, title, content, sourceTool string, steps []StepInput) (*Plan, error) {
	now := time.Now().UTC().Format(time.DateTime)
	planID := uuid.New().String()

	tx, err := m.db.Writer().Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check for existing active plan and supersede it
	var oldPlanID string
	err = tx.QueryRow(
		`SELECT id FROM plans WHERE feature_id = ? AND invalid_at IS NULL AND status = 'active'`,
		featureID,
	).Scan(&oldPlanID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing plan: %w", err)
	}

	var completedSteps []PlanStep
	if oldPlanID != "" {
		// Supersede old plan
		if _, err = tx.Exec(
			`UPDATE plans SET invalid_at = ?, status = 'superseded', updated_at = ? WHERE id = ?`,
			now, now, oldPlanID,
		); err != nil {
			return nil, fmt.Errorf("supersede old plan: %w", err)
		}

		// Gather completed steps from old plan
		rows, err := tx.Query(
			`SELECT `+stepCols+` FROM plan_steps WHERE plan_id = ? AND status = 'completed' ORDER BY step_number`,
			oldPlanID,
		)
		if err != nil {
			return nil, fmt.Errorf("query old completed steps: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			cs, err := scanStep(rows)
			if err != nil {
				return nil, fmt.Errorf("scan old step: %w", err)
			}
			completedSteps = append(completedSteps, cs)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate old steps: %w", err)
		}
	}

	// Insert new plan
	if _, err = tx.Exec(
		`INSERT INTO plans (id, feature_id, session_id, title, content, status, source_tool, valid_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)`,
		planID, featureID, sessionID, title, content, sourceTool, now, now, now,
	); err != nil {
		return nil, fmt.Errorf("insert plan: %w", err)
	}

	// Sync to plans_fts
	var rowid int64
	if err = tx.QueryRow(`SELECT rowid FROM plans WHERE id = ?`, planID).Scan(&rowid); err != nil {
		return nil, fmt.Errorf("get plan rowid: %w", err)
	}
	if _, err = tx.Exec(
		`INSERT INTO plans_fts(rowid, title, content) VALUES (?, ?, ?)`,
		rowid, title, content,
	); err != nil {
		return nil, fmt.Errorf("sync plans_fts: %w", err)
	}

	// Copy completed steps from old plan, then insert new steps
	stepNum := 1
	for _, cs := range completedSteps {
		if _, err = tx.Exec(
			`INSERT INTO plan_steps (id, plan_id, step_number, title, description, status, completed_at, linked_commits)
			 VALUES (?, ?, ?, ?, ?, 'completed', ?, ?)`,
			uuid.New().String(), planID, stepNum, cs.Title, cs.Description, cs.CompletedAt, cs.LinkedCommits,
		); err != nil {
			return nil, fmt.Errorf("copy completed step: %w", err)
		}
		stepNum++
	}
	for _, s := range steps {
		if _, err = tx.Exec(
			`INSERT INTO plan_steps (id, plan_id, step_number, title, description, status, linked_commits)
			 VALUES (?, ?, ?, ?, ?, 'pending', '[]')`,
			uuid.New().String(), planID, stepNum, s.Title, s.Description,
		); err != nil {
			return nil, fmt.Errorf("insert step: %w", err)
		}
		stepNum++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return &Plan{
		ID:         planID,
		FeatureID:  featureID,
		SessionID:  sessionID,
		Title:      title,
		Content:    content,
		Status:     "active",
		SourceTool: sourceTool,
		ValidAt:    now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// GetActivePlan returns the current active plan for a feature.
func (m *Manager) GetActivePlan(featureID string) (*Plan, error) {
	p, err := scanPlan(m.db.Reader().QueryRow(
		`SELECT `+planCols+` FROM plans WHERE feature_id = ? AND invalid_at IS NULL AND status = 'active'`, featureID))
	switch {
	case err == sql.ErrNoRows:
		return nil, fmt.Errorf("no active plan for feature %q", featureID)
	case err != nil:
		return nil, fmt.Errorf("get active plan: %w", err)
	}
	return &p, nil
}

// ListPlans returns all plans for a feature, including superseded ones.
func (m *Manager) ListPlans(featureID string) ([]Plan, error) {
	rows, err := m.db.Reader().Query(
		`SELECT `+planCols+` FROM plans WHERE feature_id = ? ORDER BY created_at DESC`,
		featureID,
	)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// UpdateStepStatus updates a step's status and sets completed_at if completed.
func (m *Manager) UpdateStepStatus(stepID, status string) error {
	now := time.Now().UTC().Format(time.DateTime)

	var completedAt *string
	if status == "completed" {
		completedAt = &now
	}

	result, err := m.db.Writer().Exec(
		`UPDATE plan_steps SET status = ?, completed_at = ? WHERE id = ?`,
		status, completedAt, stepID,
	)
	if err != nil {
		return fmt.Errorf("update step status: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("step %q not found", stepID)
	}
	return nil
}

// GetPlanSteps returns all steps for a plan, ordered by step number.
func (m *Manager) GetPlanSteps(planID string) ([]PlanStep, error) {
	rows, err := m.db.Reader().Query(
		`SELECT `+stepCols+` FROM plan_steps WHERE plan_id = ? ORDER BY step_number`,
		planID,
	)
	if err != nil {
		return nil, fmt.Errorf("get plan steps: %w", err)
	}
	defer rows.Close()

	var steps []PlanStep
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

// LinkCommitToStep appends a commit hash to the step's linked_commits JSON array.
func (m *Manager) LinkCommitToStep(stepID, commitHash string) error {
	var raw string
	switch err := m.db.Reader().QueryRow(
		`SELECT COALESCE(linked_commits, '[]') FROM plan_steps WHERE id = ?`, stepID,
	).Scan(&raw); {
	case err == sql.ErrNoRows:
		return fmt.Errorf("step %q not found", stepID)
	case err != nil:
		return fmt.Errorf("read linked_commits: %w", err)
	}

	var commits []string
	if err := json.Unmarshal([]byte(raw), &commits); err != nil {
		commits = []string{}
	}
	commits = append(commits, commitHash)

	updated, err := json.Marshal(commits)
	if err != nil {
		return fmt.Errorf("marshal linked_commits: %w", err)
	}
	if _, err = m.db.Writer().Exec(
		`UPDATE plan_steps SET linked_commits = ? WHERE id = ?`, string(updated), stepID,
	); err != nil {
		return fmt.Errorf("update linked_commits: %w", err)
	}
	return nil
}
