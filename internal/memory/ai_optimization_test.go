package memory_test

import (
	"strings"
	"testing"

	"github.com/arbazkhan971/memorx/internal/memory"
)

func TestStorePromptMemory(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-pm", "test")
	pm, err := store.StorePromptMemory(f.ID, "explain auth", "good", "clear")
	if err != nil { t.Fatalf("StorePromptMemory: %v", err) }
	if pm.ID == "" { t.Error("expected non-empty ID") }
	if pm.Effectiveness != "good" { t.Errorf("got %q", pm.Effectiveness) }
}

func TestStorePromptMemory_DefaultEffectiveness(t *testing.T) {
	store := newTestStore(t)
	pm, err := store.StorePromptMemory("", "test", "", "")
	if err != nil { t.Fatalf("StorePromptMemory: %v", err) }
	if pm.Effectiveness != "unknown" { t.Errorf("got %q", pm.Effectiveness) }
}

func TestGetEffectivePrompts(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-ep", "test")
	store.StorePromptMemory(f.ID, "good1", "good", "ok")
	store.StorePromptMemory(f.ID, "bad1", "bad", "fail")
	store.StorePromptMemory(f.ID, "good2", "good", "great")
	effective, err := store.GetEffectivePrompts(10)
	if err != nil { t.Fatalf("GetEffectivePrompts: %v", err) }
	if len(effective) != 2 { t.Fatalf("expected 2, got %d", len(effective)) }
}

func TestGetEffectivePrompts_Limit(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 5; i++ { store.StorePromptMemory("", "good", "good", "") }
	effective, err := store.GetEffectivePrompts(3)
	if err != nil { t.Fatalf("GetEffectivePrompts: %v", err) }
	if len(effective) != 3 { t.Fatalf("expected 3, got %d", len(effective)) }
}

func TestTrackTokenUsage(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-tt", "test")
	sess, _ := store.CreateSession(f.ID, "test")
	usage, err := store.TrackTokenUsage(sess.ID, "memorx_search", 1000, 500)
	if err != nil { t.Fatalf("TrackTokenUsage: %v", err) }
	if usage.ToolName != "memorx_search" { t.Errorf("got %q", usage.ToolName) }
	if usage.InputTokens != 1000 { t.Errorf("got %d", usage.InputTokens) }
}

func TestGetTokenSummary(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-ts", "test")
	sess, _ := store.CreateSession(f.ID, "test")
	store.TrackTokenUsage(sess.ID, "memorx_search", 1000, 500)
	store.TrackTokenUsage(sess.ID, "memorx_search", 2000, 1000)
	store.TrackTokenUsage(sess.ID, "memorx_get_context", 5000, 3000)
	summaries, err := store.GetTokenSummary()
	if err != nil { t.Fatalf("GetTokenSummary: %v", err) }
	if len(summaries) != 2 { t.Fatalf("expected 2, got %d", len(summaries)) }
	if summaries[0].ToolName != "memorx_get_context" { t.Errorf("got %q", summaries[0].ToolName) }
}

func TestGetTokenSummary_Empty(t *testing.T) {
	store := newTestStore(t)
	summaries, err := store.GetTokenSummary()
	if err != nil { t.Fatalf("GetTokenSummary: %v", err) }
	if len(summaries) != 0 { t.Fatalf("expected 0, got %d", len(summaries)) }
}

func TestStoreLearning(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-learn", "test")
	l, err := store.StoreLearning(f.ID, "never use mocks", "memorx_learning")
	if err != nil { t.Fatalf("StoreLearning: %v", err) }
	if l.Content != "never use mocks" { t.Errorf("got %q", l.Content) }
}

func TestGetLearnings(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-gl", "test")
	store.StoreLearning(f.ID, "l1", "t1")
	store.StoreLearning(f.ID, "l2", "t2")
	store.StoreLearning("", "global", "t3")
	all, err := store.GetLearnings("", 10)
	if err != nil { t.Fatalf("GetLearnings: %v", err) }
	if len(all) != 3 { t.Fatalf("expected 3, got %d", len(all)) }
	scoped, _ := store.GetLearnings(f.ID, 10)
	if len(scoped) != 2 { t.Fatalf("expected 2, got %d", len(scoped)) }
}

func TestGetLearnings_Limit(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 5; i++ { store.StoreLearning("", "content", "") }
	learnings, err := store.GetLearnings("", 3)
	if err != nil { t.Fatalf("GetLearnings: %v", err) }
	if len(learnings) != 3 { t.Fatalf("expected 3, got %d", len(learnings)) }
}

func TestGetContextBudget(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-cb", "test")
	store.CreateNote(f.ID, "", "short note", "note")
	store.CreateFact(f.ID, "", "db", "uses", "sqlite")
	store.StoreLearning(f.ID, "always test", "test")
	memories, used, err := store.GetContextBudget(10000, f.ID)
	if err != nil { t.Fatalf("GetContextBudget: %v", err) }
	if len(memories) == 0 { t.Fatal("expected memories") }
	if used <= 0 || used > 10000 { t.Errorf("bad used=%d", used) }
}

func TestGetContextBudget_SmallBudget(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-sb", "test")
	store.CreateNote(f.ID, "", "short", "note")
	store.CreateNote(f.ID, "", "This is a much longer note with more content", "note")
	_, used, err := store.GetContextBudget(5, f.ID)
	if err != nil { t.Fatalf("GetContextBudget: %v", err) }
	if used > 5 { t.Errorf("used %d exceeds budget 5", used) }
}

func TestGetContextBudget_PinnedFirst(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-pf", "test")
	store.CreateNote(f.ID, "", "regular", "note")
	pinned, _ := store.CreateNote(f.ID, "", "pinned note", "decision")
	store.PinMemory(pinned.ID, "note")
	memories, _, err := store.GetContextBudget(10000, f.ID)
	if err != nil { t.Fatalf("GetContextBudget: %v", err) }
	if len(memories) == 0 { t.Fatal("expected memories") }
	if !memories[0].Pinned { t.Error("expected pinned first") }
}

func TestGetContextBudget_NoFeature(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-nf", "test")
	store.CreateNote(f.ID, "", "note", "note")
	memories, _, err := store.GetContextBudget(10000, "")
	if err != nil { t.Fatalf("GetContextBudget: %v", err) }
	if len(memories) == 0 { t.Fatal("expected memories") }
}

func TestFormatTokenSummary(t *testing.T) {
	store := newTestStore(t)
	f, _ := store.CreateFeature("feat-fts", "test")
	sess, _ := store.CreateSession(f.ID, "test")
	store.TrackTokenUsage(sess.ID, "memorx_get_context", 45000, 10000)
	store.TrackTokenUsage(sess.ID, "memorx_search", 12000, 3000)
	summaries, _ := store.GetTokenSummary()
	formatted := memory.FormatTokenSummary(summaries)
	if !strings.Contains(formatted, "memorx_get_context") { t.Error("missing memorx_get_context") }
	if !strings.Contains(formatted, "memorx_search") { t.Error("missing memorx_search") }
}

func TestFormatTokenSummary_Empty(t *testing.T) {
	if f := memory.FormatTokenSummary(nil); f != "No token usage recorded." { t.Errorf("got %q", f) }
}
