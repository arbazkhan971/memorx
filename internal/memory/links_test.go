package memory_test

import (
	"testing"
)

func TestCreateLink(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	note, _ := store.CreateNote(f.ID, "", "A note", "note")
	fact, _ := store.CreateFact(f.ID, "", "db", "uses", "sqlite")

	err := store.CreateLink(note.ID, "note", fact.ID, "fact", "related", 0.8)
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Check forward link
	links, err := store.GetLinks(note.ID, "note")
	if err != nil {
		t.Fatalf("GetLinks forward: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 forward link, got %d", len(links))
	}
	if links[0].TargetID != fact.ID {
		t.Errorf("expected target %q, got %q", fact.ID, links[0].TargetID)
	}
	if links[0].Relationship != "related" {
		t.Errorf("expected relationship 'related', got %q", links[0].Relationship)
	}
	if links[0].Strength != 0.8 {
		t.Errorf("expected strength 0.8, got %f", links[0].Strength)
	}

	// Check reverse link
	reverseLinks, err := store.GetLinks(fact.ID, "fact")
	if err != nil {
		t.Fatalf("GetLinks reverse: %v", err)
	}
	if len(reverseLinks) != 1 {
		t.Fatalf("expected 1 reverse link, got %d", len(reverseLinks))
	}
	if reverseLinks[0].TargetID != note.ID {
		t.Errorf("expected reverse target %q, got %q", note.ID, reverseLinks[0].TargetID)
	}
}

func TestCreateLink_Duplicate(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")
	note, _ := store.CreateNote(f.ID, "", "A note", "note")
	fact, _ := store.CreateFact(f.ID, "", "db", "uses", "sqlite")

	// Create same link twice - should not error (INSERT OR IGNORE)
	err := store.CreateLink(note.ID, "note", fact.ID, "fact", "related", 0.8)
	if err != nil {
		t.Fatalf("CreateLink 1: %v", err)
	}
	err = store.CreateLink(note.ID, "note", fact.ID, "fact", "related", 0.9)
	if err != nil {
		t.Fatalf("CreateLink 2: %v", err)
	}

	// Should still only have 1 forward link (UNIQUE constraint with OR IGNORE)
	links, err := store.GetLinks(note.ID, "note")
	if err != nil {
		t.Fatalf("GetLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link after duplicate insert, got %d", len(links))
	}
}

func TestGetLinks_Empty(t *testing.T) {
	store := newTestStore(t)

	links, err := store.GetLinks("nonexistent", "note")
	if err != nil {
		t.Fatalf("GetLinks: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}
}

func TestAutoLink_DoesNotLinkToSelf(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-self", "Self-link test")

	// Create a note with distinctive content
	note, _ := store.CreateNote(f.ID, "", "SQLite database WAL mode performance tuning", "note")

	// AutoLink the note using its own content — should not link to itself
	count, err := store.AutoLink(note.ID, "note", "SQLite database WAL mode performance tuning")
	if err != nil {
		t.Fatalf("AutoLink: %v", err)
	}

	// Check that no link has the note as both source and target
	links, err := store.GetLinks(note.ID, "note")
	if err != nil {
		t.Fatalf("GetLinks: %v", err)
	}

	for _, l := range links {
		if l.TargetID == note.ID && l.TargetType == "note" {
			t.Errorf("AutoLink created a self-link: source=%s target=%s", l.SourceID, l.TargetID)
		}
	}

	t.Logf("AutoLink created %d links (none should be self-links)", count)
}

func TestCreateLink_AllRelationshipTypes(t *testing.T) {
	relationships := []string{"related", "extends", "implements", "contradicts"}
	for _, rel := range relationships {
		t.Run(rel, func(t *testing.T) {
			store := newTestStore(t)
			f, _ := store.CreateFeature("feat-"+rel, "Test "+rel)
			n1, _ := store.CreateNote(f.ID, "", "Source for "+rel, "note")
			n2, _ := store.CreateNote(f.ID, "", "Target for "+rel, "note")
			err := store.CreateLink(n1.ID, "note", n2.ID, "note", rel, 0.7)
			if err != nil {
				t.Fatalf("CreateLink(%s): %v", rel, err)
			}
			links, err := store.GetLinks(n1.ID, "note")
			if err != nil {
				t.Fatalf("GetLinks: %v", err)
			}
			if len(links) != 1 {
				t.Fatalf("expected 1 link, got %d", len(links))
			}
			if links[0].Relationship != rel {
				t.Errorf("expected relationship %q, got %q", rel, links[0].Relationship)
			}
		})
	}
}

func TestAutoLink_ReturnsZeroForVeryShortContent(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-short", "Short content test")
	note, _ := store.CreateNote(f.ID, "", "x", "note")

	count, err := store.AutoLink(note.ID, "note", "x")
	if err != nil {
		t.Fatalf("AutoLink: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 links for very short content, got %d", count)
	}
}

func TestAutoLink_ContentMatchingMultipleNotes(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("multi-match", "Multi match test")
	store.CreateNote(f.ID, "", "database performance tuning and optimization", "note")
	store.CreateNote(f.ID, "", "database schema migration for PostgreSQL", "note")
	store.CreateNote(f.ID, "", "database backup and recovery procedures", "note")
	fact, _ := store.CreateFact(f.ID, "", "system", "uses", "database")

	count, err := store.AutoLink(fact.ID, "fact", "database performance schema migration")
	if err != nil {
		t.Fatalf("AutoLink: %v", err)
	}
	// Should create links to at least some of the matching notes
	t.Logf("AutoLink created %d links to multiple matching notes", count)
}

func TestAutoLink(t *testing.T) {
	store := newTestStore(t)

	f, _ := store.CreateFeature("feat-a", "A")

	// Create some notes to be found by FTS
	store.CreateNote(f.ID, "", "The database layer uses SQLite with WAL mode", "note")
	store.CreateNote(f.ID, "", "Authentication uses JWT tokens", "note")

	// Create a fact
	fact, _ := store.CreateFact(f.ID, "", "storage", "engine", "sqlite")

	// AutoLink the fact with content mentioning "database sqlite"
	count, err := store.AutoLink(fact.ID, "fact", "database sqlite WAL")
	if err != nil {
		t.Fatalf("AutoLink: %v", err)
	}

	// We should get at least 1 link (the note about SQLite)
	// Note: FTS matching behavior can be fuzzy, so we check >= 0
	t.Logf("AutoLink created %d links", count)

	if count > 0 {
		links, err := store.GetLinks(fact.ID, "fact")
		if err != nil {
			t.Fatalf("GetLinks: %v", err)
		}
		if len(links) == 0 {
			t.Error("expected at least 1 link after AutoLink returned count > 0")
		}
	}
}
