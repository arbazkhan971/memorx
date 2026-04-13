package memory

import "regexp"

// privateTagRe matches <private>...</private> blocks (case-insensitive,
// multi-line) — used to strip sensitive content before it hits the DB.
var privateTagRe = regexp.MustCompile(`(?is)<private>.*?</private>`)

// StripPrivate removes <private>...</private> blocks from content
// entirely. The tags themselves are removed too. If everything inside a
// note is private, the result may be empty — callers should handle that.
//
// This is applied at the Store boundary (CreateNote and friends) so it
// runs uniformly across hooks, MCP tools, import, and dashboard ingestion
// paths.
func StripPrivate(content string) string {
	return privateTagRe.ReplaceAllString(content, "")
}
