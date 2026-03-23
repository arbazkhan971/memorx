package memory_test

import (
	"strings"
	"testing"
)

func TestGenerateOnboarding_Empty(t *testing.T) {
	store := newTestStore(t)

	doc, err := store.GenerateOnboarding("")
	if err != nil {
		t.Fatalf("GenerateOnboarding: %v", err)
	}

	for _, section := range []string{
		"# Project Onboarding:",
		"## Key Decisions",
		"## Known Facts",
		"## Current Work",
		"## Known Issues",
		"## Recent Changes",
		"## Recent Sessions",
	} {
		if !strings.Contains(doc, section) {
			t.Errorf("expected section %q in output, got:\n%s", section, doc)
		}
	}
}

func TestGenerateOnboarding_WithData(t *testing.T) {
	store := newTestStore(t)

	f, err := store.CreateFeature("auth-v2", "Authentication v2 system")
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	_, err = store.CreateNote(f.ID, "", "Use JWT tokens for auth", "decision")
	if err != nil {
		t.Fatalf("CreateNote decision: %v", err)
	}
	_, err = store.CreateNote(f.ID, "", "Token expiry bug on Safari", "blocker")
	if err != nil {
		t.Fatalf("CreateNote blocker: %v", err)
	}
	_, err = store.CreateFact(f.ID, "", "auth", "uses", "JWT")
	if err != nil {
		t.Fatalf("CreateFact: %v", err)
	}

	doc, err := store.GenerateOnboarding("")
	if err != nil {
		t.Fatalf("GenerateOnboarding: %v", err)
	}

	checks := []string{
		"Use JWT tokens for auth",
		"Token expiry bug on Safari",
		"auth uses JWT",
		"auth-v2",
	}
	for _, want := range checks {
		if !strings.Contains(doc, want) {
			t.Errorf("expected %q in output, got:\n%s", want, doc)
		}
	}
}

func TestGenerateOnboarding_FeatureScoped(t *testing.T) {
	store := newTestStore(t)

	f1, _ := store.CreateFeature("feature-a", "Feature A")
	f2, _ := store.CreateFeature("feature-b", "Feature B")

	store.CreateNote(f1.ID, "", "Decision for A", "decision")
	store.CreateNote(f2.ID, "", "Decision for B", "decision")

	doc, err := store.GenerateOnboarding("feature-a")
	if err != nil {
		t.Fatalf("GenerateOnboarding scoped: %v", err)
	}

	if !strings.Contains(doc, "Decision for A") {
		t.Error("expected decision from feature-a")
	}
	// Scoped should not include decision from feature-b
	if strings.Contains(doc, "Decision for B") {
		t.Error("should not include decisions from feature-b when scoped to feature-a")
	}
}

func TestGenerateOnboarding_NonexistentFeature(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GenerateOnboarding("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent feature")
	}
}

func TestGenerateChangelog_Empty(t *testing.T) {
	store := newTestStore(t)

	cl, err := store.GenerateChangelog(7, "markdown")
	if err != nil {
		t.Fatalf("GenerateChangelog: %v", err)
	}

	if !strings.Contains(cl, "## Changelog") {
		t.Errorf("expected Changelog header, got:\n%s", cl)
	}
	if !strings.Contains(cl, "No changes this period") {
		t.Errorf("expected empty state message, got:\n%s", cl)
	}
}

func TestGenerateChangelog_WithDecisions(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("billing", "Billing system")
	store.CreateNote(f.ID, "", "Use Stripe webhook signatures", "decision")

	cl, err := store.GenerateChangelog(7, "markdown")
	if err != nil {
		t.Fatalf("GenerateChangelog: %v", err)
	}

	if !strings.Contains(cl, "billing") {
		t.Errorf("expected feature name 'billing' in changelog, got:\n%s", cl)
	}
	if !strings.Contains(cl, "decision") {
		t.Errorf("expected decision mention in changelog, got:\n%s", cl)
	}
}

func TestGenerateChangelog_SlackFormat(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("api-v2", "API V2")
	store.CreateNote(f.ID, "", "Use gRPC for internal services", "decision")

	cl, err := store.GenerateChangelog(7, "slack")
	if err != nil {
		t.Fatalf("GenerateChangelog slack: %v", err)
	}

	if !strings.Contains(cl, "*Changelog") {
		t.Errorf("expected slack-formatted header, got:\n%s", cl)
	}
	if !strings.Contains(cl, "*api-v2*") {
		t.Errorf("expected slack-bold feature name, got:\n%s", cl)
	}
}

func TestGenerateChangelog_DefaultDays(t *testing.T) {
	store := newTestStore(t)

	// Should not error with zero days (defaults to 7)
	_, err := store.GenerateChangelog(0, "")
	if err != nil {
		t.Fatalf("GenerateChangelog with zero days: %v", err)
	}
}

func TestImportSharedMemory_Basic(t *testing.T) {
	store := newTestStore(t)

	data := map[string]interface{}{
		"feature":     "imported-feature",
		"description": "A feature imported from a shared file",
		"decisions":   []interface{}{"Decision 1", "Decision 2"},
		"blockers":    []interface{}{"Blocker 1"},
		"facts": []interface{}{
			map[string]interface{}{"subject": "api", "predicate": "uses", "object": "REST"},
		},
	}

	result, err := store.ImportSharedMemory(data)
	if err != nil {
		t.Fatalf("ImportSharedMemory: %v", err)
	}

	if result.Features != 1 {
		t.Errorf("expected 1 feature, got %d", result.Features)
	}
	if result.Notes != 3 { // 2 decisions + 1 blocker
		t.Errorf("expected 3 notes, got %d", result.Notes)
	}
	if result.Facts != 1 {
		t.Errorf("expected 1 fact, got %d", result.Facts)
	}

	// Verify the feature exists
	f, err := store.GetFeature("imported-feature")
	if err != nil {
		t.Fatalf("GetFeature: %v", err)
	}
	if f.Description != "A feature imported from a shared file" {
		t.Errorf("expected description, got %q", f.Description)
	}

	// Verify decisions
	notes, _ := store.ListNotes(f.ID, "decision", 10)
	if len(notes) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(notes))
	}
}

func TestImportSharedMemory_MissingFeatureName(t *testing.T) {
	store := newTestStore(t)

	data := map[string]interface{}{
		"decisions": []interface{}{"some decision"},
	}

	_, err := store.ImportSharedMemory(data)
	if err == nil {
		t.Error("expected error when feature name is missing")
	}
}

func TestImportSharedMemory_ExistingFeature(t *testing.T) {
	store := newTestStore(t)

	// Create the feature first
	store.CreateFeature("existing-feat", "Already exists")

	data := map[string]interface{}{
		"feature":  "existing-feat",
		"notes":    []interface{}{"Imported note"},
	}

	result, err := store.ImportSharedMemory(data)
	if err != nil {
		t.Fatalf("ImportSharedMemory: %v", err)
	}

	// Should not create a new feature
	if result.Features != 0 {
		t.Errorf("expected 0 new features (already existed), got %d", result.Features)
	}
	if result.Notes != 1 {
		t.Errorf("expected 1 note, got %d", result.Notes)
	}
}

func TestIntentPrefix(t *testing.T) {
	// Test via changelog format which uses intentPrefix internally
	store := newTestStore(t)

	// Just verify the changelog doesn't error with no data
	_, err := store.GenerateChangelog(7, "markdown")
	if err != nil {
		t.Fatalf("GenerateChangelog: %v", err)
	}
}
