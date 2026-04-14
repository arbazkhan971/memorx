package dashboard

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/arbazkhan971/memorx/internal/git"
)

// Cross-process event log.
//
// The dashboard, MCP server and hook subcommands all run in separate
// processes, so in-memory pub/sub can't deliver events between them. We
// use a JSONL log at .memory/events.jsonl as the cross-process channel:
//
//   - PublishEvent appends a line to the log (from any process).
//   - The dashboard Serve() goroutine tails the file and re-publishes
//     each new line into the in-process broker so SSE subscribers see it.
//
// The file is truncated when it exceeds maxEventLogBytes so it never
// grows unbounded. In practice the log stays under a few hundred KB.

const (
	maxEventLogBytes = 1 << 20 // 1 MB ceiling before truncation
	truncateKeepLast = 256     // keep last N lines after truncation
)

var (
	eventLogPath string
	eventLogMu   sync.Mutex
	eventLogOnce sync.Once
)

// initEventLog locates .memory/events.jsonl relative to the current
// working directory's git root. Called lazily on first PublishEvent so
// we don't do filesystem work at import time.
func initEventLog() {
	eventLogOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			return
		}
		root, err := git.FindGitRoot(cwd)
		if err != nil {
			return
		}
		memDir := filepath.Join(root, ".memory")
		if err := os.MkdirAll(memDir, 0o755); err != nil {
			return
		}
		eventLogPath = filepath.Join(memDir, "events.jsonl")
	})
}

func appendEventLog(e Event) {
	initEventLog()
	if eventLogPath == "" {
		return
	}
	eventLogMu.Lock()
	defer eventLogMu.Unlock()

	f, err := os.OpenFile(eventLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))

	// Opportunistic truncation when the file exceeds the ceiling.
	if st, err := f.Stat(); err == nil && st.Size() > maxEventLogBytes {
		truncateEventLog()
	}
}

// truncateEventLog keeps only the last N lines of the event log. Caller
// must hold eventLogMu.
func truncateEventLog() {
	data, err := os.ReadFile(eventLogPath)
	if err != nil {
		return
	}
	lines := splitLines(data)
	if len(lines) <= truncateKeepLast {
		return
	}
	lines = lines[len(lines)-truncateKeepLast:]
	// Rewrite atomically via temp file.
	tmp := eventLogPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, ln := range lines {
		_, _ = w.Write(ln)
		_, _ = w.WriteString("\n")
	}
	_ = w.Flush()
	_ = f.Close()
	_ = os.Rename(tmp, eventLogPath)
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// tailEventLog watches the event log for new lines and republishes them
// into the in-process broker so SSE subscribers see cross-process events.
// Stops when stop is closed.
func (s *Server) tailEventLog(stop <-chan struct{}) {
	initEventLog()
	if eventLogPath == "" {
		return
	}
	// Seek to end — we only care about events produced after the
	// dashboard starts. Older events are still in the DB and come via
	// the /api/* endpoints.
	var offset int64
	if st, err := os.Stat(eventLogPath); err == nil {
		offset = st.Size()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			st, err := os.Stat(eventLogPath)
			if err != nil || st.Size() == offset {
				continue
			}
			// If the file shrank (truncation), reset to zero.
			if st.Size() < offset {
				offset = 0
			}
			f, err := os.Open(eventLogPath)
			if err != nil {
				continue
			}
			if _, err := f.Seek(offset, 0); err != nil {
				f.Close()
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 64*1024), 1<<20)
			for sc.Scan() {
				var e Event
				if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
					s.broker.publish(e)
				}
			}
			offset = st.Size()
			f.Close()
		}
	}
}
