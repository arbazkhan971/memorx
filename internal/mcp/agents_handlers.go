package mcp

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- Multi-Agent Memory ---

func (s *DevMemServer) handleAgentRegister(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	name, errRes := requireParam(req, "name")
	if errRes != nil {
		return errRes, nil
	}
	role := getStringArg(req, "role", "primary")
	agent, err := s.store.RegisterAgent(name, role)
	if err != nil {
		return respondErr("Failed to register agent: %v", err)
	}
	return respond("# Agent registered\n\n- Name: %s\n- Role: %s\n- Registered: %s", agent.Name, agent.Role, agent.RegisteredAt)
}

func (s *DevMemServer) handleAgentHandoff(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	fromAgent, errRes := requireParam(req, "from_agent")
	if errRes != nil {
		return errRes, nil
	}
	toAgent, errRes := requireParam(req, "to_agent")
	if errRes != nil {
		return errRes, nil
	}
	summary := getStringArg(req, "summary", "")

	id, err := s.store.AgentHandoff(fromAgent, toAgent, summary, s.currentSessionID)
	if err != nil {
		return respondErr("Handoff failed: %v", err)
	}

	// Start new session for target agent
	if feature, fErr := s.store.GetActiveFeature(); fErr == nil {
		if sess, sErr := s.store.CreateSession(feature.ID, "agent:"+toAgent); sErr == nil {
			s.currentSessionID = sess.ID
		}
	}

	return respond("# Handoff complete\n\n- ID: %s\n- From: %s -> To: %s\n- Summary: %s\n- New session started for %s", id[:8], fromAgent, toAgent, summary, toAgent)
}

func (s *DevMemServer) handleAgentScope(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	agent, errRes := requireParam(req, "agent")
	if errRes != nil {
		return errRes, nil
	}
	action := getStringArg(req, "action", "list")
	features := getStringSliceArg(req, "features")

	scopes, err := s.store.ManageAgentScope(agent, features, action)
	if err != nil {
		return respondErr("Scope management failed: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Agent scope: %s [%s]\n\n", agent, action)
	if len(scopes) == 0 {
		b.WriteString("No features in scope.\n")
	} else {
		b.WriteString("**Features:**\n")
		for _, f := range scopes {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	return mcplib.NewToolResultText(b.String()), nil
}

func (s *DevMemServer) handleAgentMerge(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	feature, errRes := requireParam(req, "feature")
	if errRes != nil {
		return errRes, nil
	}

	sessions, notes, facts, err := s.store.AgentMerge(feature)
	if err != nil {
		return respondErr("Merge failed: %v", err)
	}

	return respond("# Merge summary: %s\n\n- Sessions: %d\n- Notes: %d\n- Active facts: %d\n\nAll memories consolidated under feature %q.", feature, sessions, notes, facts, feature)
}

// --- Security & Compliance ---

func (s *DevMemServer) handleAuditLog(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action := getStringArg(req, "action", "query")

	switch action {
	case "log":
		op := getStringArg(req, "operation", "manual")
		details := getStringArg(req, "details", "")
		if err := s.store.AuditLog(op, details, "user"); err != nil {
			return respondErr("Failed to log: %v", err)
		}
		return respond("# Audit entry logged\n\n- Operation: %s\n- Details: %s", op, details)

	case "query":
		limit := getIntArg(req, "limit", 20)
		entries, err := s.store.QueryAuditLog(limit)
		if err != nil {
			return respondErr("Failed to query audit log: %v", err)
		}
		if len(entries) == 0 {
			return respond("# Audit log\n\nNo entries.")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Audit log (%d entries)\n\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "- **%s** [%s] %s — %s\n", e.CreatedAt, e.Agent, e.Operation, truncate(e.Details, 80))
		}
		return mcplib.NewToolResultText(b.String()), nil

	default:
		return respondErr("Unknown action %q. Use 'query' or 'log'.", action)
	}
}

func (s *DevMemServer) handleSensitiveFilter(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action := getStringArg(req, "action", "scan")
	featureID := ""
	if fname := getStringArg(req, "feature", ""); fname != "" {
		fid, errRes := s.resolveFeatureID(fname)
		if errRes != nil {
			return errRes, nil
		}
		featureID = fid
	}

	switch action {
	case "scan":
		results, err := s.store.SensitiveScan(featureID)
		if err != nil {
			return respondErr("Scan failed: %v", err)
		}
		if len(results) == 0 {
			return respond("# Sensitive scan\n\nNo sensitive data detected.")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Sensitive scan: %d note(s) with sensitive data\n\n", len(results))
		for _, r := range results {
			fmt.Fprintf(&b, "- Note %s: %d match(es) — %s\n", r.NoteID[:8], len(r.Matches), strings.Join(r.Matches, ", "))
		}
		return mcplib.NewToolResultText(b.String()), nil

	case "redact":
		count, err := s.store.SensitiveRedact(featureID)
		if err != nil {
			return respondErr("Redact failed: %v", err)
		}
		return respond("# Redaction complete\n\n%d note(s) redacted.", count)

	default:
		return respondErr("Unknown action %q. Use 'scan' or 'redact'.", action)
	}
}

func (s *DevMemServer) handleRetentionPolicy(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action := getStringArg(req, "action", "get")

	switch action {
	case "set":
		days := getIntArg(req, "days", 90)
		types := getStringSliceArg(req, "types")
		if len(types) == 0 {
			types = []string{"notes", "facts"}
		}
		if err := s.store.SetRetentionPolicy(days, types); err != nil {
			return respondErr("Failed to set policy: %v", err)
		}
		return respond("# Retention policy set\n\n- Delete after: %d days\n- Types: %s", days, strings.Join(types, ", "))

	case "get":
		p, err := s.store.GetRetentionPolicy()
		if err != nil {
			return respond("# Retention policy\n\nNo policy set. Use action='set' to configure.")
		}
		return respond("# Retention policy\n\n- Delete after: %d days\n- Types: %s", p.Days, strings.Join(p.Types, ", "))

	case "apply":
		count, err := s.store.ApplyRetentionPolicy()
		if err != nil {
			return respondErr("Failed to apply policy: %v", err)
		}
		return respond("# Retention policy applied\n\n%d record(s) deleted.", count)

	default:
		return respondErr("Unknown action %q. Use 'set', 'get', or 'apply'.", action)
	}
}

func (s *DevMemServer) handleExportCompliance(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	feature := getStringArg(req, "feature", "")
	format := getStringArg(req, "format", "json")

	data, err := s.store.ExportCompliance(feature, format)
	if err != nil {
		return respondErr("Export failed: %v", err)
	}

	scope := "all features"
	if feature != "" {
		scope = feature
	}
	return respond("# Compliance export (%s, %s)\n\n```\n%s\n```", scope, format, data)
}

// --- Performance & Scale ---

func (s *DevMemServer) handleVacuum(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	before, after, err := s.store.Vacuum()
	if err != nil {
		return respondErr("Vacuum failed: %v", err)
	}

	beforeMB := float64(before) / 1024 / 1024
	afterMB := float64(after) / 1024 / 1024
	pct := 0.0
	if before > 0 {
		pct = (1.0 - float64(after)/float64(before)) * 100
	}

	return respond("Database optimized: %.1fMB -> %.1fMB (%.0f%% reduction)", beforeMB, afterMB, pct)
}

func (s *DevMemServer) handleStats(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	stats, err := s.store.Stats()
	if err != nil {
		return respondErr("Stats failed: %v", err)
	}

	var b strings.Builder
	b.WriteString("# Database stats\n\n")
	fmt.Fprintf(&b, "**File size:** %.2f MB\n\n", float64(stats.FileSize)/1024/1024)
	b.WriteString("| Table | Rows |\n|-------|------|\n")
	for t, c := range stats.Tables {
		fmt.Fprintf(&b, "| %s | %d |\n", t, c)
	}
	return mcplib.NewToolResultText(b.String()), nil
}

func (s *DevMemServer) handleArchive(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action := getStringArg(req, "action", "list")

	switch action {
	case "archive":
		feature, errRes := requireParam(req, "feature")
		if errRes != nil {
			return errRes, nil
		}
		if err := s.store.ArchiveFeature(feature); err != nil {
			return respondErr("Archive failed: %v", err)
		}
		return respond("# Archived: %s\n\nFeature marked as done and archived.", feature)

	case "restore":
		feature, errRes := requireParam(req, "feature")
		if errRes != nil {
			return errRes, nil
		}
		if err := s.store.RestoreFeature(feature); err != nil {
			return respondErr("Restore failed: %v", err)
		}
		return respond("# Restored: %s\n\nFeature restored to paused state.", feature)

	case "list":
		archived, err := s.store.ListArchivedFeatures()
		if err != nil {
			return respondErr("List failed: %v", err)
		}
		if len(archived) == 0 {
			return respond("# Archived features\n\nNo archived features.")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Archived features (%d)\n\n", len(archived))
		for _, a := range archived {
			fmt.Fprintf(&b, "- %s (archived: %s)\n", a.FeatureName, a.ArchivedAt)
		}
		return mcplib.NewToolResultText(b.String()), nil

	default:
		return respondErr("Unknown action %q. Use 'archive', 'restore', or 'list'.", action)
	}
}

func (s *DevMemServer) handleBenchmarkSelf(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	insertAvg, searchAvg, contextAvg, err := s.store.BenchmarkSelf()
	if err != nil {
		return respondErr("Benchmark failed: %v", err)
	}
	return respond("Insert: %.1fms avg | Search: %.1fms avg | Context: %.1fms avg", insertAvg, searchAvg, contextAvg)
}

// --- Ecosystem ---

func (s *DevMemServer) handleVersion(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	// Count registered tools (approximate from known count)
	goVer := runtime.Version()
	return respond("memorX v3.0.0 | 60+ tools | %s | SQLite 3.45", goVer)
}

func (s *DevMemServer) handleDoctor(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	checks, err := s.store.Doctor()
	if err != nil {
		return respondErr("Doctor failed: %v", err)
	}

	allPassed := true
	var b strings.Builder
	b.WriteString("# memorx doctor\n\n")
	for _, c := range checks {
		icon := "PASS"
		if !c.Passed {
			icon = "FAIL"
			allPassed = false
		}
		fmt.Fprintf(&b, "- [%s] %s: %s\n", icon, c.Name, c.Detail)
	}
	if allPassed {
		b.WriteString("\nAll checks passed.")
	}
	return mcplib.NewToolResultText(b.String()), nil
}

func (s *DevMemServer) handleConfig(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action := getStringArg(req, "action", "list")

	switch action {
	case "set":
		key, errRes := requireParam(req, "key")
		if errRes != nil {
			return errRes, nil
		}
		value := getStringArg(req, "value", "")
		if err := s.store.SetConfig(key, value); err != nil {
			return respondErr("Failed to set config: %v", err)
		}
		return respond("# Config set\n\n- %s = %s", key, value)

	case "get":
		key, errRes := requireParam(req, "key")
		if errRes != nil {
			return errRes, nil
		}
		val, err := s.store.GetConfig(key)
		if err != nil {
			return respond("# Config\n\nKey %q not set.", key)
		}
		return respond("# Config\n\n- %s = %s", key, val)

	case "list":
		entries, err := s.store.ListConfig()
		if err != nil {
			return respondErr("Failed to list config: %v", err)
		}
		if len(entries) == 0 {
			return respond("# Config\n\nNo configuration set.")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Config (%d entries)\n\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s = %s\n", e.Key, e.Value)
		}
		return mcplib.NewToolResultText(b.String()), nil

	default:
		return respondErr("Unknown action %q. Use 'get', 'set', or 'list'.", action)
	}
}
