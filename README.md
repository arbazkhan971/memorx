# memorX

**Your AI picks up exactly where you left off — across commits, plans, and sessions.**

One command to install. Zero-friction capture via Claude Code hooks. A live local dashboard so you can *see* your AI's memory. Single Go binary, zero runtime dependencies, 100% local.

```bash
go install github.com/arbazkhan971/memorx/cmd/devmem@latest
memorx install          # wires Claude Code hooks + MCP server in one shot
memorx dashboard        # open http://127.0.0.1:37778 — live memory stream
```

That's it. Open Claude Code and every session automatically knows where you left off.

---

## Why memorX

Every AI coding CLI (Claude Code, Codex, Cursor, Windsurf, Gemini CLI) suffers from amnesia. Close a session, lose all context. Switch tools, start from scratch. You waste 5–10 minutes per session re-explaining your project, decisions, and progress.

memorX fixes this. One binary, works everywhere, remembers everything.

## What you get

- **Zero-friction capture** — 5 Claude Code lifecycle hooks auto-capture sessions, commits, and decisions. No tool calls required.
- **Live local web dashboard** — `memorx dashboard` opens a real-time web UI showing active feature, plan progress, memory stream, git timeline, and search. Embedded in the binary, no Node or Bun required.
- **Cross-process live event stream** — SSE-driven live updates work across the hook subcommand, the MCP server, and the dashboard via a tiny file-based event log at `.memory/events.jsonl`.
- **Auto-briefing on every session** — the `SessionStart` hook injects a "welcome back" summary into every new Claude Code conversation.
- **Transcript summarization** — on `SessionEnd`, memorX reads the Claude Code JSONL transcript and stores a rule-based summary (tool counts, edited files, commits, decisions). No LLM call, fully deterministic.
- **Git-native** — `PostToolUse` hook watches for `git commit` and auto-syncs commits with intent classification (feature/bugfix/refactor), matching commits to plan steps.
- **Plan persistence** — plans survive across sessions, auto-track progress from commits.
- **Bi-temporal facts** — tracks what's true now AND what was true before (contradiction resolution, time-travel queries).
- **Memory linking** — A-MEM/Zettelkasten-style connections between related memories.
- **3-layer progressive-disclosure search** — `memorx_search_index` → `memorx_timeline` → `memorx_get_memory`, token-efficient recall.
- **Privacy tags** — wrap anything in `<private>…</private>` and it's stripped at the storage boundary. Works uniformly across hooks, MCP tools, and import.
- **Universal MCP** — works with Claude Code, Cursor, Codex, Windsurf, Gemini CLI — any MCP client.

## Install

### One command (recommended)

```bash
go install github.com/arbazkhan971/memorx/cmd/devmem@latest
memorx install
```

`memorx install` detects Claude Code, writes hook entries to `~/.claude/settings.json`, registers the MCP server via `claude mcp add`, and creates `~/.memorx/settings.json`. Idempotent and non-destructive — re-running is safe and existing non-memorx hooks are preserved.

### Verify

```bash
memorx doctor           # self-test: DB, hooks, binary on PATH
```

### Manual setup (other MCP clients)

```bash
# Claude Code
claude mcp add -s user --transport stdio memorx -- memorx

# Cursor — add to .cursor/mcp.json:
{ "mcpServers": { "memorx": { "command": "memorx", "transport": "stdio" } } }
```

## Dashboard

```bash
memorx dashboard                # :37778 by default
memorx dashboard --port 8080    # custom port
```

The dashboard shows:
- **Active feature** with branch + plan progress bar
- **Memory counts** (features, notes, facts, commits, sessions)
- **Live event stream** (updates in real-time as hooks and MCP tools write)
- **Recent memories** with type tags (decision / blocker / note / ...)
- **Recent commits** with intent tags (feat / fix / refactor / ...)
- **Search box** — live full-text search across all notes

Pure Go `net/http` + embedded HTML via `//go:embed`. No Node, no bundler, no external process.

### How the live stream works across processes

The MCP server, hook subcommands, and dashboard all run in **separate processes**. They share updates through a tiny JSONL event log at `.memory/events.jsonl`:

1. Any process that publishes an event (hook, MCP tool) appends a line to the log.
2. The dashboard's background goroutine tails the file and re-publishes each new entry into its in-process broker.
3. SSE subscribers on `/api/events` receive the live stream.

The log is opportunistically truncated to the last 256 events when it exceeds 1 MB, so it never grows unbounded.

## How it works

```
┌─ Claude Code session ─────────────────────────────────────┐
│                                                           │
│  SessionStart ──────▶ memorx hook session-start           │
│                        └▶ briefing injected into context  │
│                                                           │
│  you prompt ────────▶ memorx hook user-prompt-submit      │
│                        └▶ observation stored (auto)       │
│                                                           │
│  Claude runs Edit ──▶ (captured by transcript)            │
│                                                           │
│  Claude runs Bash ──▶ memorx hook post-tool-use           │
│    (git commit)        └▶ sync commits, classify intent,  │
│                           match to plan steps             │
│                                                           │
│  SessionEnd ────────▶ memorx hook session-end             │
│                        └▶ transcript summarized,          │
│                           session closed with summary     │
└───────────────────────────────────────────────────────────┘
                             │
                             ▼
                  .memory/memory.db (SQLite, WAL)
                  .memory/events.jsonl (live event log)
                             │
                             ▼
                 memorx dashboard (localhost:37778)
```

All hooks are subcommands of the single `memorx` binary — they share the same DB connection pool and the same code as the MCP server. No separate worker process, no port conflicts, no orphan daemons.

## CLI reference

```
memorx                    Run MCP stdio server (default, for MCP clients)
memorx install            Install hooks & MCP config into Claude Code
memorx dashboard [--port N]
                          Start local web dashboard (default :37778)
memorx hook <event>       Run a Claude Code lifecycle hook
                          Events: session-start, user-prompt-submit,
                                  post-tool-use, stop, session-end
memorx doctor             Self-test: DB, hooks, binary on PATH
memorx version            Print version
memorx help               Show this help
```

## MCP tools (76 tools)

memorX exposes **76 MCP tools** covering core memory, search, git sync, plans, sessions, analytics, time-travel, predictive intelligence, self-healing, multi-agent, compliance, and workflow integration. Highlights:

| Tool | What it does |
|------|-------------|
| `memorx_briefing` | Quick "welcome back" — what you were working on |
| `memorx_remember` | Save a note, decision, blocker, progress update, or next step. Auto-links to related memories |
| `memorx_search` | FTS5 + trigram search across all memory types |
| `memorx_search_index` | **New.** Compact hit index (~30 tokens/hit) — filter before fetching |
| `memorx_timeline` | **New.** Chronological window around a memory or the recent feed |
| `memorx_get_memory` | **New.** Full detail for a specific memory ID with links |
| `memorx_observe` | **New.** Lightweight observation capture (used by hooks) |
| `memorx_sync` | Pull git commits, classify intent, auto-match to plan steps |
| `memorx_save_plan` | Store a plan with trackable steps that survives sessions |
| `memorx_end_session` | End session with summary — next session reads it automatically |
| `memorx_health` | Memory health score (0–100) with actionable suggestions |
| `memorx_forget` | Smart cleanup: stale facts, stale notes, completed features |
| `memorx_generate_rules` | Auto-generate `AGENTS.md` from memory |

Run `memorx` (no args) to start the MCP server; every MCP-aware client can introspect the full tool list.

## Privacy

Wrap anything you don't want stored in `<private>...</private>` tags:

```
"Our API key is <private>sk-abc123</private>" → "Our API key is "
```

Stripping happens at the `Store.CreateNote` boundary (`internal/memory/privacy.go`), so it's enforced uniformly across hooks, MCP tools, import, and dashboard ingestion. Tag matching is case-insensitive and spans multi-line blocks. If a note is *entirely* private, the store rejects it rather than persisting an empty record.

**Verified tested:** privacy stripping across `memorx_remember` (MCP), `memorx_observe` (hook/MCP), and `user-prompt-submit` (hook). Zero leakage.

## Parallel Work (Multiple CLIs Simultaneously)

memorX supports concurrent access via SQLite WAL mode:

```
Terminal 1: Claude Code         Terminal 2: Cursor
┌────────────────────┐          ┌────────────────────┐
│ feature: auth-v2   │          │ feature: billing   │
│ "Token refresh     │          │ "Webhook handler   │
│  working"          │          │  done"             │
└────────┬───────────┘          └────────┬───────────┘
         └──────────┬────────────────────┘
                    ▼
            .memory/memory.db
            (WAL mode: concurrent reads, serialized writes)
```

## Architecture

```
MCP Client (Claude Code / Cursor / Codex / Windsurf)
    │ stdio
    ▼
memorX (single Go binary, ~18 MB)
    ├── CLI dispatcher (install, hook, dashboard, doctor, version)
    ├── MCP Layer (76 tools + 2 resources)
    ├── Hook subcommands (session-start, user-prompt-submit,
    │                     post-tool-use, stop, session-end)
    ├── Dashboard (net/http + embed, SSE live stream on :37778)
    ├── Cross-process event log (.memory/events.jsonl)
    ├── Session Manager (features, sessions, briefings)
    ├── Git Engine (commits, intent classification, sync)
    ├── Search Engine (FTS5 + trigram + BM25 scoring)
    ├── Plan Engine (CRUD, auto-detect, commit-to-step matching)
    ├── Memory Core (bi-temporal facts, notes, A-MEM links, privacy)
    ├── Consolidation Engine (contradictions, decay, summarization)
    ├── Analytics (dev patterns, health scoring)
    └── SQLite (WAL mode, .memory/memory.db)
```

**Key design choices:**
- **Single binary** — no Docker, no daemon, no Node, no Bun, no Python, no Chroma
- **SQLite + WAL** — concurrent access from multiple tools, sub-millisecond writes
- **FTS5 + trigram** — full-text search with BM25 ranking, fuzzy fallback, no embedding model needed
- **Bi-temporal** — every fact has `valid_at`/`invalid_at`, enables "what was true last week?" queries
- **stdio transport** — MCP client spawns the binary, communicates via stdin/stdout
- **`//go:embed` dashboard** — HTML/CSS/JS bundled into the binary at compile time

**Direct Go dependencies:** `mark3labs/mcp-go` (MCP SDK), `modernc.org/sqlite` (pure-Go SQLite, no CGO), `google/uuid`. Everything else is stdlib.

## Benchmark

memorX ships with a 70-scenario benchmark across 7 developer memory abilities:

```bash
make benchmark
```

```
Ability                  Score    Accuracy   Scenarios
─────────────────────    ─────    ────────   ─────────
Decision Recall          100.0%   100.0%     10/10
Knowledge Updates        100.0%   100.0%     10/10
Plan Tracking            100.0%   100.0%     10/10
Abstention               100.0%   100.0%      7/7
Session Continuity        99.3%   100.0%     15/15
Temporal Reasoning        99.0%   100.0%     10/10
Cross-Feature             98.8%   100.0%      8/8

Overall: 99.6% score | 100% accuracy | <1ms latency
```

## Token Savings

```
Without memorX: 5,000–10,000 tokens per session re-explaining context
With memorX:    200–500 tokens via memorx_briefing + memorx_search_index
```

The new progressive-disclosure search (`memorx_search_index` → `memorx_get_memory`) adds another ~10× savings on recall by filtering before fetching full content.

## Comparison

| Feature | memorX | claude-mem | Mem0 | Zep | KeepGoing |
|---------|--------|------------|------|-----|-----------|
| Automatic capture via hooks | Yes | Yes | No | No | Yes |
| Live local web dashboard | Yes | Yes | No | No | No |
| Cross-process live event stream | Yes | Yes | No | No | No |
| Session/feature tracking | Yes | Partial | No | No | Partial |
| Git commit integration | Yes | No | No | No | Partial |
| Plan persistence | Yes | No | No | No | No |
| Bi-temporal facts | Yes | No | No | Yes | No |
| Memory linking | Yes | No | Yes | Yes | No |
| 3-layer progressive search | Yes | Yes | No | No | No |
| Privacy tags | Yes | Yes | No | No | No |
| Single binary, zero deps | Yes | No | No | No | No |
| Works with any MCP client | Yes | Partial | No | No | No |
| 100% local, no cloud | Yes | Yes | No | No | Yes |
| Built-in benchmark | Yes | No | No | No | No |

## Tested

Every feature listed above has been verified end-to-end:

- **MCP stdio backward compat** — `memorx` (no args) handles `initialize` + `tools/list` (76 tools) + `tools/call` correctly
- **All 5 hooks** — `session-start`, `user-prompt-submit`, `post-tool-use`, `stop`, `session-end` all run cleanly with the Claude Code JSON payload format on stdin
- **Smart installer** — writes valid `~/.claude/settings.json` hook entries, registers the MCP server, is idempotent, preserves existing non-memorx hooks and top-level settings
- **Dashboard** — all 6 endpoints (`/`, `/api/status`, `/api/features`, `/api/memories`, `/api/commits`, `/api/search`, `/api/events`) serve correctly; empty responses return `[]` not `null`
- **Cross-process SSE** — events published by the MCP server process and by hook subcommands both reach dashboard SSE subscribers via `.memory/events.jsonl`
- **Privacy stripping** — secrets in `<private>…</private>` blocks are stripped uniformly through `memorx_remember` (MCP), `memorx_observe` (hook/MCP), and `user-prompt-submit` (hook). Zero leakage across all three paths
- **Transcript summarizer** — parses real Claude Code JSONL format, correctly extracts user-turn count, tool counts, edited files, commits, and decision patterns
- **Doctor** — happy and sad paths both produce useful output
- **Unit tests** — all passing in `memory`, `search`, `storage`, `plans`, `hooks`, `dashboard`, `git`, and `consolidation` packages

## License

MIT
