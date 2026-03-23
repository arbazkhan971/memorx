package memory_test

import (
	"testing"
)

func TestCreateNote(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	sess, _ := store.CreateSession(f.ID, "claude-code")

	note, err := store.CreateNote(f.ID, sess.ID, "Decided to use SQLite for storage", "decision")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if note.ID == "" {
		t.Error("expected non-empty ID")
	}
	if note.Content != "Decided to use SQLite for storage" {
		t.Errorf("expected content match, got %q", note.Content)
	}
	if note.Type != "decision" {
		t.Errorf("expected type 'decision', got %q", note.Type)
	}
	if note.FeatureID != f.ID {
		t.Errorf("expected feature ID %q, got %q", f.ID, note.FeatureID)
	}
	if note.SessionID != sess.ID {
		t.Errorf("expected session ID %q, got %q", sess.ID, note.SessionID)
	}
}

func TestCreateNote_DefaultType(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")

	note, err := store.CreateNote(f.ID, "", "A general note", "")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if note.Type != "note" {
		t.Errorf("expected default type 'note', got %q", note.Type)
	}
}

func TestListNotes_All(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	store.CreateNote(f.ID, "", "Note 1", "note")
	store.CreateNote(f.ID, "", "Note 2", "decision")
	store.CreateNote(f.ID, "", "Note 3", "blocker")

	notes, err := store.ListNotes(f.ID, "", 10)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
}

func TestListNotes_FilterByType(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	store.CreateNote(f.ID, "", "Note 1", "note")
	store.CreateNote(f.ID, "", "Decision 1", "decision")
	store.CreateNote(f.ID, "", "Decision 2", "decision")

	decisions, err := store.ListNotes(f.ID, "decision", 10)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(decisions))
	}
	for _, n := range decisions {
		if n.Type != "decision" {
			t.Errorf("expected type 'decision', got %q", n.Type)
		}
	}
}

func TestListNotes_Limit(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	store.CreateNote(f.ID, "", "Note 1", "note")
	store.CreateNote(f.ID, "", "Note 2", "note")
	store.CreateNote(f.ID, "", "Note 3", "note")

	notes, err := store.ListNotes(f.ID, "", 2)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes (limited), got %d", len(notes))
	}
}

func TestListNotes_FilterByFeature(t *testing.T) {
	store := newTestStore(t)

	fa, _ := store.CreateFeature("feat-a", "A")
	fb, _ := store.CreateFeature("feat-b", "B")
	store.CreateNote(fa.ID, "", "Note for A", "note")
	store.CreateNote(fb.ID, "", "Note for B", "note")

	notes, err := store.ListNotes(fa.ID, "", 10)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 note for feat-a, got %d", len(notes))
	}
	if notes[0].Content != "Note for A" {
		t.Errorf("expected 'Note for A', got %q", notes[0].Content)
	}
}

func TestGetNote(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	created, _ := store.CreateNote(f.ID, "", "A note", "note")

	got, err := store.GetNote(created.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: %q vs %q", got.ID, created.ID)
	}
	if got.Content != "A note" {
		t.Errorf("Content mismatch: %q", got.Content)
	}
}

func TestGetNote_NotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetNote("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent note")
	}
}
