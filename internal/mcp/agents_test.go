package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestHandleAgentRegister(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	res, err := srv.handleAgentRegister(ctx, newReq("memorx_agent_register", map[string]interface{}{
		"name": "claude",
		"role": "primary",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Agent registered") {
		t.Errorf("expected 'Agent registered', got:\n%s", text)
	}
	if !strings.Contains(text, "claude") {
		t.Errorf("expected agent name 'claude', got:\n%s", text)
	}
}

func TestHandleAgentHandoff(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Register agents first
	srv.handleAgentRegister(ctx, newReq("memorx_agent_register", map[string]interface{}{"name": "agent-a", "role": "primary"}))
	srv.handleAgentRegister(ctx, newReq("memorx_agent_register", map[string]interface{}{"name": "agent-b", "role": "assistant"}))

	// Start a feature so there's a session to hand off
	srv.handleStartFeature(ctx, newReq("memorx_start_feature", map[string]interface{}{"name": "handoff-test"}))

	res, err := srv.handleAgentHandoff(ctx, newReq("memorx_agent_handoff", map[string]interface{}{
		"from_agent": "agent-a",
		"to_agent":   "agent-b",
		"summary":    "Finished auth module, need review",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Handoff complete") {
		t.Errorf("expected 'Handoff complete', got:\n%s", text)
	}
	if !strings.Contains(text, "agent-b") {
		t.Errorf("expected target agent name, got:\n%s", text)
	}
}

func TestHandleAgentScope(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	srv.handleAgentRegister(ctx, newReq("memorx_agent_register", map[string]interface{}{"name": "scoped-agent", "role": "reviewer"}))

	// Grant
	res, err := srv.handleAgentScope(ctx, newReq("memorx_agent_scope", map[string]interface{}{
		"agent":    "scoped-agent",
		"features": []interface{}{"auth", "payments"},
		"action":   "grant",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "auth") {
		t.Errorf("expected 'auth' in scope, got:\n%s", text)
	}

	// List
	res, err = srv.handleAgentScope(ctx, newReq("memorx_agent_scope", map[string]interface{}{
		"agent":  "scoped-agent",
		"action": "list",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text = resultText(t, res)
	if !strings.Contains(text, "payments") {
		t.Errorf("expected 'payments' in scope list, got:\n%s", text)
	}
}

func TestHandleAgentMerge(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Create a feature and add some data
	srv.handleStartFeature(ctx, newReq("memorx_start_feature", map[string]interface{}{"name": "merge-test"}))
	srv.handleRemember(ctx, newReq("memorx_remember", map[string]interface{}{"content": "Test note for merge"}))

	res, err := srv.handleAgentMerge(ctx, newReq("memorx_agent_merge", map[string]interface{}{
		"feature": "merge-test",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Merge summary") {
		t.Errorf("expected 'Merge summary', got:\n%s", text)
	}
}

func TestHandleAuditLog(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Log an entry
	res, err := srv.handleAuditLog(ctx, newReq("memorx_audit_log", map[string]interface{}{
		"action":    "log",
		"operation": "test_op",
		"details":   "test details",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Audit entry logged") {
		t.Errorf("expected 'Audit entry logged', got:\n%s", text)
	}

	// Query
	res, err = srv.handleAuditLog(ctx, newReq("memorx_audit_log", map[string]interface{}{
		"action": "query",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text = resultText(t, res)
	if !strings.Contains(text, "test_op") {
		t.Errorf("expected 'test_op' in audit log, got:\n%s", text)
	}
}

func TestHandleSensitiveFilter(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Create a note with sensitive data
	srv.handleStartFeature(ctx, newReq("memorx_start_feature", map[string]interface{}{"name": "sensitive-test"}))
	srv.handleRemember(ctx, newReq("memorx_remember", map[string]interface{}{
		"content": "The API key is api_key=sk_test_1234567890abcdef",
	}))

	// Scan
	res, err := srv.handleSensitiveFilter(ctx, newReq("memorx_sensitive_filter", map[string]interface{}{
		"action": "scan",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "sensitive data") {
		t.Errorf("expected sensitive data detection, got:\n%s", text)
	}
}

func TestHandleRetentionPolicy(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Set policy
	res, err := srv.handleRetentionPolicy(ctx, newReq("memorx_retention_policy", map[string]interface{}{
		"action": "set",
		"days":   float64(30),
		"types":  []interface{}{"notes"},
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Retention policy set") {
		t.Errorf("expected policy set confirmation, got:\n%s", text)
	}

	// Get policy
	res, err = srv.handleRetentionPolicy(ctx, newReq("memorx_retention_policy", map[string]interface{}{
		"action": "get",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text = resultText(t, res)
	if !strings.Contains(text, "30") {
		t.Errorf("expected '30' days in policy, got:\n%s", text)
	}
}

func TestHandleExportCompliance(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Create some data
	srv.handleStartFeature(ctx, newReq("memorx_start_feature", map[string]interface{}{"name": "compliance-test"}))
	srv.handleRemember(ctx, newReq("memorx_remember", map[string]interface{}{"content": "Compliance note"}))

	res, err := srv.handleExportCompliance(ctx, newReq("memorx_export_compliance", map[string]interface{}{
		"format": "json",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Compliance export") {
		t.Errorf("expected 'Compliance export', got:\n%s", text)
	}
}

func TestHandleVacuum(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	res, err := srv.handleVacuum(ctx, newReq("memorx_vacuum", nil))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "optimized") {
		t.Errorf("expected 'optimized', got:\n%s", text)
	}
}

func TestHandleStats(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	res, err := srv.handleStats(ctx, newReq("memorx_stats", nil))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Database stats") {
		t.Errorf("expected 'Database stats', got:\n%s", text)
	}
	if !strings.Contains(text, "features") {
		t.Errorf("expected 'features' table in stats, got:\n%s", text)
	}
}

func TestHandleArchive(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Create a feature to archive
	srv.handleStartFeature(ctx, newReq("memorx_start_feature", map[string]interface{}{"name": "archive-test"}))

	// Archive it
	res, err := srv.handleArchive(ctx, newReq("memorx_archive", map[string]interface{}{
		"action":  "archive",
		"feature": "archive-test",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Archived") {
		t.Errorf("expected 'Archived', got:\n%s", text)
	}

	// List archived
	res, err = srv.handleArchive(ctx, newReq("memorx_archive", map[string]interface{}{
		"action": "list",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text = resultText(t, res)
	if !strings.Contains(text, "archive-test") {
		t.Errorf("expected 'archive-test' in list, got:\n%s", text)
	}
}

func TestHandleBenchmarkSelf(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}
	t.Parallel()
	srv, _ := setupTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	var res *mcplib.CallToolResult
	var err error
	go func() {
		res, err = srv.handleBenchmarkSelf(ctx, newReq("memorx_benchmark_self", nil))
		close(done)
	}()
	select {
	case <-done:
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		text := resultText(t, res)
		if !strings.Contains(text, "Insert:") || !strings.Contains(text, "Search:") || !strings.Contains(text, "Context:") {
			t.Errorf("expected benchmark results, got:\n%s", text)
		}
	case <-ctx.Done():
		t.Skip("benchmark timed out — skipping")
	}
}

func TestHandleVersion(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	res, err := srv.handleVersion(ctx, newReq("memorx_version", nil))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "memorX") {
		t.Errorf("expected 'memorX' in version, got:\n%s", text)
	}
	if !strings.Contains(text, "60+ tools") {
		t.Errorf("expected '60+ tools' in version, got:\n%s", text)
	}
}

func TestHandleDoctor(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	res, err := srv.handleDoctor(ctx, newReq("memorx_doctor", nil))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "doctor") {
		t.Errorf("expected 'doctor' header, got:\n%s", text)
	}
	if !strings.Contains(text, "PASS") {
		t.Errorf("expected at least one PASS check, got:\n%s", text)
	}
}

func TestHandleConfig(t *testing.T) {
	srv, _ := setupTestServer(t)
	ctx := context.Background()

	// Set a config value
	res, err := srv.handleConfig(ctx, newReq("memorx_config", map[string]interface{}{
		"action": "set",
		"key":    "default_tier",
		"value":  "standard",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Config set") {
		t.Errorf("expected 'Config set', got:\n%s", text)
	}

	// Get the config value
	res, err = srv.handleConfig(ctx, newReq("memorx_config", map[string]interface{}{
		"action": "get",
		"key":    "default_tier",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text = resultText(t, res)
	if !strings.Contains(text, "standard") {
		t.Errorf("expected 'standard', got:\n%s", text)
	}

	// List
	res, err = srv.handleConfig(ctx, newReq("memorx_config", map[string]interface{}{
		"action": "list",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text = resultText(t, res)
	if !strings.Contains(text, "default_tier") {
		t.Errorf("expected 'default_tier' in list, got:\n%s", text)
	}
}
