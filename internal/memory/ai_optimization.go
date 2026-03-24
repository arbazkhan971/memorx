package memory

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PromptMemory struct{ ID, FeatureID, Prompt, Effectiveness, Outcome, CreatedAt string }
type TokenUsage struct{ ID, SessionID, ToolName, CreatedAt string; InputTokens, OutputTokens int }
type TokenSummary struct{ ToolName string; TotalInput, TotalOutput, TotalCalls int }
type Learning struct{ ID, FeatureID, Content, SourceTool, CreatedAt string }
type BudgetedMemory struct{ ID, Type, Content, CreatedAt string; Score float64; Pinned bool; TokenEstimate int }

func (s *Store) StorePromptMemory(featureID, prompt, effectiveness, outcome string) (*PromptMemory, error) {
	if effectiveness == "" { effectiveness = "unknown" }
	id, now := uuid.New().String(), time.Now().UTC().Format(time.DateTime)
	if _, err := s.db.Writer().Exec(`INSERT INTO prompt_memory (id, feature_id, prompt, effectiveness, outcome, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, nullIfEmpty(featureID), prompt, effectiveness, nullIfEmpty(outcome), now); err != nil {
		return nil, fmt.Errorf("store prompt memory: %w", err)
	}
	return &PromptMemory{ID: id, FeatureID: featureID, Prompt: prompt, Effectiveness: effectiveness, Outcome: outcome, CreatedAt: now}, nil
}

func (s *Store) GetEffectivePrompts(limit int) ([]PromptMemory, error) {
	if limit <= 0 { limit = 10 }
	return collectRows(s.db.Reader(), `SELECT id, COALESCE(feature_id, ''), prompt, effectiveness, COALESCE(outcome, ''), created_at FROM prompt_memory WHERE effectiveness = 'good' ORDER BY created_at DESC LIMIT ?`, []any{limit}, func(rows *sql.Rows) (PromptMemory, error) {
		var p PromptMemory; return p, rows.Scan(&p.ID, &p.FeatureID, &p.Prompt, &p.Effectiveness, &p.Outcome, &p.CreatedAt)
	})
}

func (s *Store) TrackTokenUsage(sessionID, toolName string, inputTokens, outputTokens int) (*TokenUsage, error) {
	id, now := uuid.New().String(), time.Now().UTC().Format(time.DateTime)
	if _, err := s.db.Writer().Exec(`INSERT INTO token_usage (id, session_id, tool_name, input_tokens, output_tokens, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, nullIfEmpty(sessionID), toolName, inputTokens, outputTokens, now); err != nil {
		return nil, fmt.Errorf("track token usage: %w", err)
	}
	return &TokenUsage{ID: id, SessionID: sessionID, ToolName: toolName, InputTokens: inputTokens, OutputTokens: outputTokens, CreatedAt: now}, nil
}

func (s *Store) GetTokenSummary() ([]TokenSummary, error) {
	rows, err := s.db.Reader().Query(`SELECT tool_name, SUM(input_tokens), SUM(output_tokens), COUNT(*) FROM token_usage GROUP BY tool_name ORDER BY SUM(input_tokens) + SUM(output_tokens) DESC`)
	if err != nil { return nil, fmt.Errorf("get token summary: %w", err) }
	defer rows.Close()
	var out []TokenSummary
	for rows.Next() {
		var ts TokenSummary
		if err := rows.Scan(&ts.ToolName, &ts.TotalInput, &ts.TotalOutput, &ts.TotalCalls); err != nil { return nil, fmt.Errorf("scan token summary: %w", err) }
		out = append(out, ts)
	}
	return out, rows.Err()
}

func (s *Store) StoreLearning(featureID, content, sourceTool string) (*Learning, error) {
	id, now := uuid.New().String(), time.Now().UTC().Format(time.DateTime)
	if _, err := s.db.Writer().Exec(`INSERT INTO learnings (id, feature_id, content, source_tool, created_at) VALUES (?, ?, ?, ?, ?)`, id, nullIfEmpty(featureID), content, nullIfEmpty(sourceTool), now); err != nil {
		return nil, fmt.Errorf("store learning: %w", err)
	}
	return &Learning{ID: id, FeatureID: featureID, Content: content, SourceTool: sourceTool, CreatedAt: now}, nil
}

func (s *Store) GetLearnings(featureID string, limit int) ([]Learning, error) {
	if limit <= 0 { limit = 50 }
	q := `SELECT id, COALESCE(feature_id, ''), content, COALESCE(source_tool, ''), created_at FROM learnings`
	var args []any
	if featureID != "" { q += ` WHERE feature_id = ?`; args = append(args, featureID) }
	q += ` ORDER BY created_at DESC LIMIT ?`; args = append(args, limit)
	return collectRows(s.db.Reader(), q, args, func(rows *sql.Rows) (Learning, error) {
		var l Learning; return l, rows.Scan(&l.ID, &l.FeatureID, &l.Content, &l.SourceTool, &l.CreatedAt)
	})
}

func (s *Store) GetContextBudget(budget int, featureID string) ([]BudgetedMemory, int, error) {
	if budget <= 0 { budget = 4000 }
	r := s.db.Reader()
	var candidates []BudgetedMemory
	var ff string; var fargs []any
	if featureID != "" { ff = ` AND feature_id = ?`; fargs = []any{featureID} }
	gather := func(q string, a []any) {
		rows, err := r.Query(q, a...); if err != nil { return }; defer rows.Close()
		for rows.Next() {
			var m BudgetedMemory; var pi int
			if rows.Scan(&m.ID, &m.Type, &m.Content, &m.CreatedAt, &pi) == nil { m.Pinned = pi == 1; candidates = append(candidates, m) }
		}
	}
	gather(`SELECT id, 'note', content, created_at, COALESCE(pinned, 0) FROM notes WHERE 1=1`+ff+` AND pinned = 1 ORDER BY created_at DESC LIMIT 50`, fargs)
	gather(`SELECT id, 'fact', subject || ' ' || predicate || ' ' || object, recorded_at, COALESCE(pinned, 0) FROM facts WHERE invalid_at IS NULL`+ff+` AND pinned = 1 ORDER BY recorded_at DESC LIMIT 50`, fargs)
	gather(`SELECT id, 'note', content, created_at, COALESCE(pinned, 0) FROM notes WHERE 1=1`+ff+` AND (pinned = 0 OR pinned IS NULL) ORDER BY created_at DESC LIMIT 50`, fargs)
	gather(`SELECT id, 'fact', subject || ' ' || predicate || ' ' || object, recorded_at, COALESCE(pinned, 0) FROM facts WHERE invalid_at IS NULL`+ff+` AND (pinned = 0 OR pinned IS NULL) ORDER BY recorded_at DESC LIMIT 50`, fargs)
	lq := `SELECT id, 'learning', content, created_at FROM learnings`; var la []any
	if featureID != "" { lq += ` WHERE feature_id = ?`; la = append(la, featureID) }
	lq += ` ORDER BY created_at DESC LIMIT 20`
	if rows, err := r.Query(lq, la...); err == nil { defer rows.Close()
		for rows.Next() { var m BudgetedMemory; if rows.Scan(&m.ID, &m.Type, &m.Content, &m.CreatedAt) == nil { candidates = append(candidates, m) } }
	}
	now := time.Now().UTC()
	for i := range candidates { candidates[i].TokenEstimate = estimateTokens(candidates[i].Content); candidates[i].Score = scoreBudgetCandidate(candidates[i], now) }
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	seen := make(map[string]bool); var deduped []BudgetedMemory
	for _, c := range candidates { if !seen[c.ID] { seen[c.ID] = true; deduped = append(deduped, c) } }
	var selected []BudgetedMemory; usedTokens := 0
	for _, c := range deduped { if usedTokens+c.TokenEstimate > budget { continue }; selected = append(selected, c); usedTokens += c.TokenEstimate }
	return selected, usedTokens, nil
}

func estimateTokens(content string) int { n := len(content) / 4; if n < 1 { n = 1 }; return n }
func scoreBudgetCandidate(m BudgetedMemory, now time.Time) float64 {
	score := 1.0; if m.Pinned { score += 10.0 }
	switch m.Type { case "learning": score *= 3.0; case "fact": score *= 1.5 }
	if t, err := time.Parse(time.DateTime, m.CreatedAt); err == nil { days := now.Sub(t).Hours() / 24.0; if days < 0 { days = 0 }; score *= math.Exp(-0.693 * days / 14.0) }
	return score
}
func FormatTokenSummary(summaries []TokenSummary) string {
	if len(summaries) == 0 { return "No token usage recorded." }
	var b strings.Builder; b.WriteString("Top token consumers: ")
	for i, ts := range summaries { total := ts.TotalInput + ts.TotalOutput; if i > 0 { b.WriteString(", ") }; fmt.Fprintf(&b, "%s (%s)", ts.ToolName, fmtTokenCount(total)) }
	return b.String()
}
func fmtTokenCount(n int) string { if n >= 1000 { return fmt.Sprintf("%dK", n/1000) }; return fmt.Sprintf("%d", n) }
