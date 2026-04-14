// Package dashboard serves memorX's local web UI. It's a tiny pure-Go
// HTTP server with embedded static assets — no Node, no bundler, no
// external dependencies. The UI connects back to the server over SSE
// for real-time updates as hooks and MCP tools write to memory.
package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/arbazkhan971/memorx/internal/memory"
	"github.com/arbazkhan971/memorx/internal/storage"
)

//go:embed static/*
var staticFS embed.FS

// Event is a single live update pushed to dashboard subscribers.
type Event struct {
	Type string                 `json:"type"`
	Data map[string]any         `json:"data"`
	At   string                 `json:"at"`
}

// Broker distributes events to SSE subscribers. Package-level so hooks
// and MCP handlers can publish without plumbing a broker through every
// call site.
type Broker struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

var defaultBroker = &Broker{subs: map[chan Event]struct{}{}}

func (b *Broker) subscribe() chan Event {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) unsubscribe(ch chan Event) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *Broker) publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			// drop on slow subscribers — live stream is best-effort
		}
	}
}

// PublishEvent is the global entry point — hook handlers and MCP tools
// call this to broadcast updates to the dashboard.
//
// Events are written to a file-based log (.memory/events.jsonl) so they
// can cross process boundaries: the MCP server, hooks, and dashboard
// all run in separate processes but share the same repo-local log. The
// dashboard tails that log and re-publishes each new entry into its
// in-process broker for SSE subscribers.
func PublishEvent(typ string, data map[string]any) {
	e := Event{
		Type: typ,
		Data: data,
		At:   time.Now().UTC().Format(time.RFC3339),
	}
	defaultBroker.publish(e)
	appendEventLog(e)
}

// Server is a tiny HTTP server wired to the memory store.
type Server struct {
	store   *memory.Store
	db      *storage.DB
	gitRoot string
	broker  *Broker
}

func NewServer(db *storage.DB, gitRoot string) *Server {
	return &Server{
		store:   memory.NewStore(db),
		db:      db,
		gitRoot: gitRoot,
		broker:  defaultBroker,
	}
}

// Handler returns the HTTP handler, ready to mount on any mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}

	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/features", s.handleFeatures)
	mux.HandleFunc("/api/memories", s.handleMemories)
	mux.HandleFunc("/api/commits", s.handleCommits)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/events", s.handleSSE)
	return mux
}

// Serve blocks until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE needs long-lived writes
		IdleTimeout:  60 * time.Second,
	}
	// Start the event-log tailer so SSE subscribers see events produced
	// by other processes (MCP server, hooks).
	stop := make(chan struct{})
	go s.tailEventLog(stop)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		close(stop)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		close(stop)
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"git_root": s.gitRoot,
		"now":      time.Now().UTC().Format(time.RFC3339),
	}
	if f, err := s.store.GetActiveFeature(); err == nil {
		resp["active_feature"] = f
		if ctxData, err := s.store.GetContext(f.ID, "standard", nil); err == nil {
			resp["plan"] = ctxData.Plan
			resp["recent_notes"] = ctxData.RecentNotes
			resp["recent_commits"] = ctxData.RecentCommits
		}
	}
	var counts struct {
		Features int `json:"features"`
		Notes    int `json:"notes"`
		Facts    int `json:"facts"`
		Commits  int `json:"commits"`
		Sessions int `json:"sessions"`
	}
	r2 := s.db.Reader()
	_ = r2.QueryRow("SELECT COUNT(*) FROM features").Scan(&counts.Features)
	_ = r2.QueryRow("SELECT COUNT(*) FROM notes").Scan(&counts.Notes)
	_ = r2.QueryRow("SELECT COUNT(*) FROM facts").Scan(&counts.Facts)
	_ = r2.QueryRow("SELECT COUNT(*) FROM commits").Scan(&counts.Commits)
	_ = r2.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&counts.Sessions)
	resp["counts"] = counts
	writeJSON(w, resp)
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	features, err := s.store.ListFeatures("all")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if features == nil {
		features = []memory.Feature{}
	}
	writeJSON(w, features)
}

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	feature := r.URL.Query().Get("feature")
	limit := parseInt(r.URL.Query().Get("limit"), 50)
	if feature == "" {
		f, err := s.store.GetActiveFeature()
		if err != nil {
			writeJSON(w, []any{})
			return
		}
		feature = f.ID
	} else {
		f, err := s.store.GetFeature(feature)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		feature = f.ID
	}
	notes, err := s.store.ListNotes(feature, "", limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if notes == nil {
		notes = []memory.Note{}
	}
	writeJSON(w, notes)
}

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request) {
	limit := parseInt(r.URL.Query().Get("limit"), 30)
	rows, err := s.db.Reader().Query(`SELECT hash, message, author, intent_type, committed_at FROM commits ORDER BY committed_at DESC LIMIT ?`, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	type commit struct {
		Hash, Message, Author, IntentType, CommittedAt string
	}
	out := []commit{}
	for rows.Next() {
		var c commit
		if err := rows.Scan(&c.Hash, &c.Message, &c.Author, &c.IntentType, &c.CommittedAt); err != nil {
			continue
		}
		out = append(out, c)
	}
	writeJSON(w, out)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, []any{})
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 20)
	// Use FTS directly for the dashboard — mirrors memorx_search without
	// pulling in the search engine dependency here.
	rows, err := s.db.Reader().Query(`
		SELECT n.id, n.feature_id, n.content, n.type, n.created_at
		FROM notes_fts fts JOIN notes n ON n.rowid = fts.rowid
		WHERE notes_fts MATCH ? ORDER BY rank LIMIT ?`, q, limit)
	if err != nil {
		// Fall back to LIKE if FTS query is malformed
		rows, err = s.db.Reader().Query(`
			SELECT id, feature_id, content, type, created_at
			FROM notes WHERE content LIKE ? ORDER BY created_at DESC LIMIT ?`,
			"%"+q+"%", limit)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	defer rows.Close()
	type hit struct {
		ID, FeatureID, Content, Type, CreatedAt string
	}
	out := []hit{}
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.ID, &h.FeatureID, &h.Content, &h.Type, &h.CreatedAt); err != nil {
			continue
		}
		out = append(out, h)
	}
	writeJSON(w, out)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.broker.subscribe()
	defer s.broker.unsubscribe(ch)

	// Initial comment so clients know we're connected.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func parseInt(s string, def int) int {
	n := def
	if s == "" {
		return n
	}
	fmt.Sscanf(s, "%d", &n)
	if n <= 0 {
		return def
	}
	return n
}
