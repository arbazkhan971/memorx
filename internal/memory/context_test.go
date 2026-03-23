package memory_test

import (
	"testing"
	"time"
)

func TestGetContext_Compact(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "Feature A")

	ctx, err := store.GetContext(f.ID, "compact", nil)
	if err != nil {
		t.Fatalf("GetContext compact: %v", err)
	}

	if ctx.Feature == nil {
		t.Fatal("expected non-nil feature")
	}
	if ctx.Feature.Name != "feat-a" {
		t.Errorf("expected feature name 'feat-a', got %q", ctx.Feature.Name)
	}
}

func TestGetContext_Standard(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "Feature A")
	store.CreateNote(f.ID, "", "Note 1", "note")
	store.CreateNote(f.ID, "", "Note 2", "decision")
	store.CreateFact(f.ID, "", "db", "uses", "sqlite")

	ctx, err := store.GetContext(f.ID, "standard", nil)
	if err != nil {
		t.Fatalf("GetContext standard: %v", err)
	}

	if ctx.Feature == nil {
		t.Fatal("expected non-nil feature")
	}
	if len(ctx.RecentNotes) != 2 {
		t.Errorf("expected 2 recent notes, got %d", len(ctx.RecentNotes))
	}
	if len(ctx.ActiveFacts) != 1 {
		t.Errorf("expected 1 active fact, got %d", len(ctx.ActiveFacts))
	}
}

func TestGetContext_StandardWithAsOf(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "Feature A")
	store.CreateFact(f.ID, "", "db", "uses", "sqlite")

	// Query with a future time
	future := time.Now().Add(time.Hour)
	ctx, err := store.GetContext(f.ID, "standard", &future)
	if err != nil {
		t.Fatalf("GetContext standard with asOf: %v", err)
	}
	if len(ctx.ActiveFacts) != 1 {
		t.Errorf("expected 1 active fact with future asOf, got %d", len(ctx.ActiveFacts))
	}

	// Query with a past time
	past := time.Now().Add(-time.Hour)
	ctx, err = store.GetContext(f.ID, "standard", &past)
	if err != nil {
		t.Fatalf("GetContext standard with past asOf: %v", err)
	}
	if len(ctx.ActiveFacts) != 0 {
		t.Errorf("expected 0 active facts with past asOf, got %d", len(ctx.ActiveFacts))
	}
}

func TestGetContext_Detailed(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "Feature A")
	sess, _ := store.CreateSession(f.ID, "claude-code")
	store.CreateNote(f.ID, sess.ID, "Note 1", "note")
	store.CreateFact(f.ID, sess.ID, "db", "uses", "sqlite")

	ctx, err := store.GetContext(f.ID, "detailed", nil)
	if err != nil {
		t.Fatalf("GetContext detailed: %v", err)
	}

	if ctx.Feature == nil {
		t.Fatal("expected non-nil feature")
	}
	if len(ctx.RecentNotes) != 1 {
		t.Errorf("expected 1 note, got %d", len(ctx.RecentNotes))
	}
	if len(ctx.ActiveFacts) != 1 {
		t.Errorf("expected 1 active fact, got %d", len(ctx.ActiveFacts))
	}
	if len(ctx.SessionHistory) != 1 {
		t.Errorf("expected 1 session, got %d", len(ctx.SessionHistory))
	}
}

func TestGetContext_InvalidTier(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "Feature A")

	_, err := store.GetContext(f.ID, "invalid", nil)
	if err == nil {
		t.Fatal("expected error for invalid tier")
	}
}

func TestGetContext_FeatureNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetContext("nonexistent-id", "compact", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent feature")
	}
}
