package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/arbazkhan971/memorx/internal/dashboard"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// Progressive-disclosure search tools — port of claude-mem's 3-layer
// search pattern. These wrap the existing search engine to return
// results at three different levels of detail so callers can use a
// "filter-then-fetch" workflow instead of always getting full content.

// handleSearchIndex returns compact hit metadata only — ID, type, a
// short snippet, and timestamp. Cheapest layer: ~30 tokens per hit.
func (s *DevMemServer) handleSearchIndex(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	query, errRes := requireParam(req, "query")
	if errRes != nil {
		return errRes, nil
	}
	scope := getStringArg(req, "scope", "current_feature")
	types := getStringSliceArg(req, "types")
	limit := 25
	if a := req.GetArguments(); a != nil {
		if v, ok := a["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
	}

	var featureID string
	if scope == "current_feature" {
		f, err := s.store.GetActiveFeature()
		if err != nil {
			return respondErr("No active feature. Use scope='all_features' first.")
		}
		featureID = f.ID
	}
	results, err := s.searchEngine.Search(query, scope, types, featureID, limit)
	if err != nil {
		return respondErr("Search failed: %v", err)
	}

	type indexHit struct {
		ID      string  `json:"id"`
		Type    string  `json:"type"`
		Feature string  `json:"feature,omitempty"`
		Snippet string  `json:"snippet"`
		Score   float64 `json:"score"`
		At      string  `json:"at"`
	}
	out := make([]indexHit, 0, len(results))
	for _, r := range results {
		out = append(out, indexHit{
			ID:      r.ID,
			Type:    r.Type,
			Feature: r.FeatureName,
			Snippet: truncate(r.Content, 60),
			Score:   r.Relevance,
			At:      r.CreatedAt,
		})
	}
	b, _ := json.MarshalIndent(map[string]any{
		"query": query,
		"count": len(out),
		"hits":  out,
		"next":  "call memorx_get_memory with an id for full content, or memorx_timeline for chronological context",
	}, "", "  ")
	return mcplib.NewToolResultText(string(b)), nil
}

// handleTimeline returns a chronological window of memories around a
// reference point (an ID or a timestamp). Middle layer: shows what
// happened before/after something for context.
func (s *DevMemServer) handleTimeline(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	aroundID := getStringArg(req, "around_id", "")
	window := 10
	if a := req.GetArguments(); a != nil {
		if v, ok := a["window"].(float64); ok && v > 0 {
			window = int(v)
		}
	}

	var anchorTime, featureID string
	if aroundID != "" {
		err := s.db.Reader().QueryRow(`SELECT created_at, feature_id FROM notes WHERE id = ?`, aroundID).Scan(&anchorTime, &featureID)
		if err != nil {
			return respondErr("Note %q not found", aroundID)
		}
	} else {
		f, err := s.store.GetActiveFeature()
		if err != nil {
			return respondErr("No active feature and no around_id provided")
		}
		featureID = f.ID
	}

	var rows *sql.Rows
	var err error
	if anchorTime != "" {
		rows, err = s.db.Reader().Query(`
			SELECT id, type, content, created_at FROM (
			  SELECT id, type, content, created_at FROM notes
			    WHERE feature_id = ? AND created_at <= ? ORDER BY created_at DESC LIMIT ?
			) UNION ALL SELECT id, type, content, created_at FROM (
			  SELECT id, type, content, created_at FROM notes
			    WHERE feature_id = ? AND created_at > ? ORDER BY created_at ASC LIMIT ?
			) ORDER BY created_at ASC`,
			featureID, anchorTime, window, featureID, anchorTime, window)
	} else {
		rows, err = s.db.Reader().Query(`
			SELECT id, type, content, created_at FROM notes
			WHERE feature_id = ? ORDER BY created_at DESC LIMIT ?`, featureID, window*2)
	}
	if err != nil {
		return respondErr("Timeline query failed: %v", err)
	}
	defer rows.Close()

	type entry struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
		At      string `json:"at"`
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.Type, &e.Content, &e.At); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	b, _ := json.MarshalIndent(map[string]any{
		"feature_id": featureID,
		"anchor":     aroundID,
		"count":      len(entries),
		"timeline":   entries,
	}, "", "  ")
	return mcplib.NewToolResultText(string(b)), nil
}

// handleGetMemory fetches the full content for a specific memory ID.
// Top layer: full detail, used only after filtering with search_index.
func (s *DevMemServer) handleGetMemory(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	id, errRes := requireParam(req, "id")
	if errRes != nil {
		return errRes, nil
	}
	note, err := s.store.GetNote(id)
	if err != nil {
		return respondErr("Memory %q not found: %v", id, err)
	}
	links, _ := s.store.GetLinks(id, "note")

	out := map[string]any{
		"id":         note.ID,
		"feature_id": note.FeatureID,
		"session_id": note.SessionID,
		"type":       note.Type,
		"content":    note.Content,
		"created_at": note.CreatedAt,
		"updated_at": note.UpdatedAt,
		"links":      links,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return mcplib.NewToolResultText(string(b)), nil
}

// handleObserve is the lightweight capture path used by hooks. It writes
// a note with an "obs:" prefix so it's identifiable as hook-captured,
// without auto-linking or plan promotion, keeping it cheap to call on
// every tool invocation.
func (s *DevMemServer) handleObserve(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	content, errRes := requireParam(req, "content")
	if errRes != nil {
		return errRes, nil
	}
	source := getStringArg(req, "source", "hook")

	f, err := s.store.GetActiveFeature()
	if err != nil {
		return respondErr("No active feature — observations need a feature.")
	}
	sess, _ := s.store.GetCurrentSession()
	sessionID := ""
	if sess != nil {
		sessionID = sess.ID
	}

	tagged := strings.TrimSpace("obs: " + source + ": " + content)
	note, err := s.store.CreateNote(f.ID, sessionID, tagged, "note")
	if err != nil {
		return respondErr("Observe failed: %v", err)
	}

	dashboard.PublishEvent("observation", map[string]any{
		"id":      note.ID,
		"feature": f.Name,
		"type":    note.Type,
		"content": note.Content,
	})
	return respond("Observed (%s): %s", note.ID, truncate(note.Content, 80))
}
