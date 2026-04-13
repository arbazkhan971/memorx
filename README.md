# memorX

**Your AI picks up exactly where you left off — across commits, plans, and sessions.**

One command to install. Zero-friction capture via Claude Code hooks. A live local dashboard so you can *see* your AI's memory. Single Go binary, zero runtime dependencies, 100% local.

```bash
memorx install          # wires Claude Code hooks + MCP server in one shot
memorx dashboard        # open http://127.0.0.1:37778 — live memory stream
```

That's it. Open Claude Code and every session automatically knows where you left off.

---

## Why memorX

Every AI coding CLI (Claude Code, Codex, Cursor, Windsurf, Gemini CLI) suffers from amnesia. Close a session, lose all context. Switch tools, start from scratch. You waste 5–10 minutes per session re-explaining your project, decisions, and progress.

memorX fixes this. One binary, works everywhere, remembers everything.

## What you get

- **Zero-friction capture** — Claude Code lifecycle hooks auto-capture sessions, commits, and decisions. No tool calls required.
- **Live local dashboard** — `memorx dashboard` opens a real-time web UI showing active feature, plan progress, memory stream, and git timeline. Embedded in the binary, no Node or Bun required.
- **Auto-briefing on every session** — a SessionStart hook injects a "welcome back" summary into every new Claude Code conversation.
- **Git-native** — auto-syncs commits with intent classification (feature/bugfix/refactor), matches commits to plan steps.
- **Plan persistence** — plans survive across sessions, auto-track progress from commits.
- **Bi-temporal facts** — tracks what's true now AND what was true before (contradiction resolution, time-travel queries).
- **Memory linking** — A-MEM/Zettelkasten-style connections between related memories.
- **3-layer progressive-disclosure search** — `search_index` → `timeline` → `get_memory`, ~10× token savings on recall.
- **Privacy tags** — wrap anything in `<private>…</private>` and it's stripped at the storage boundary. Never persisted.
- **Session summaries** — on SessionEnd, auto-summarize the transcript (rule-based, no LLM call).
- **Universal MCP** — works with Claude Code, Cursor, Codex, Windsurf, Gemini CLI — any MCP client.

## Install

### One command (recommended)

```bash
go install github.com/arbazkhan971/memorx/cmd/devmem@latest
memorx install
```

`memorx install` detects Claude Code, writes hooks into `~/.claude/settings.json`, registers the MCP server via `claude mcp add`, and sets up `~/.memorx/settings.json`. Idempotent — re-run any time.

### Manual

```bash
# Claude Code
claude mcp add -s user --transport stdio memorx -- memorx

# Cursor — add to .cursor/mcp.json:
{ "mcpServers": { "memorx": { "command": "memorx", "transport": "stdio" } } }
```

### Verify

```bash
memorx doctor           # self-test: DB, hooks, binary on PATH
```

## Dashboard

```bash
memorx dashboard                # :37778 by default
memorx dashboard --port 8080    # custom port
```

The dashboard shows your active feature, plan progress, memory counts, recent notes, recent commits, a search box, and a **live event stream** that updates in real-time via SSE as hooks and MCP tools write to memory. Pure Go `net/http` + embedded HTML — no Node, no bundler, no external process.

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
                             │
                             ▼
                 memorx dashboard (localhost:37778)
```

All hooks are subcommands of the single `memorx` binary — they share the same DB connection pool and the same code as the MCP server. No separate worker process, no port conflicts, no orphan daemons.

## Tools (88 MCP tools)

memorX exposes **88 MCP tools** across core memory, search, git sync, plans, sessions, analytics, time-travel, predictive intelligence, self-healing, multi-agent, compliance, and workflow integration. Highlights:

| Tool | What it does |
|------|-------------|
| `memorx_briefing` | Quick "welcome back" — what you were working on |
| `memorx_remember` | Save a note, decision, blocker. Auto-links to related memories |
| `memorx_search` | 3-layer FTS5 + trigram search across all memory types |
| `memorx_search_index` | **New.** Compact hit index (~30 tokens/hit) — filter before fetching |
| `memorx_timeline` | **New.** Chronological window around a memory or the recent feed |
| `memorx_get_memory` | **New.** Full detail for a specific memory ID |
| `memorx_observe` | **New.** Lightweight observation capture (used by hooks) |
| `memorx_sync` | Pull git commits, classify intent, auto-match to plan steps |
| `memorx_save_plan` | Store a plan with trackable steps that survives sessions |
| `memorx_end_session` | End session with summary — next session reads it automatically |
| `memorx_health` | Memory health score (0–100) with actionable suggestions |
| `memorx_forget` | Smart cleanup: stale facts, stale notes, completed features |
| `memorx_generate_rules` | Auto-generate `AGENTS.md` from memory |

Full tool reference: `memorx` (no args) starts the MCP server; every MCP-aware client can introspect the tool list.

## Privacy

Wrap anything you don't want stored in `<private>...</private>` tags:

```
"Our API key is <private>sk-abc123</private>" → "Our API key is "
```

Stripping happens at the Store boundary (`internal/memory/privacy.go`), so it's enforced uniformly across hooks, MCP tools, import, and dashboard ingestion. If a note is *entirely* private, the store rejects it rather than persisting an empty record.

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
memorX (single Go binary, ~18MB)
    ├── CLI dispatcher (install, hook, dashboard, doctor, version)
    ├── MCP Layer (88 tools + 2 resources)
    ├── Hook subcommands (session-start, post-tool-use, session-end, ...)
    ├── Dashboard (net/http + embed, SSE live stream on :37778)
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
- **Single binary** — no Docker, no daemon, no external dependencies (no Node, no Bun, no Python, no Chroma)
- **SQLite + WAL** — concurrent access from multiple tools, sub-millisecond writes
- **FTS5 + trigram** — full-text search with BM25 ranking, fuzzy fallback, no embedding model needed
- **Bi-temporal** — every fact has `valid_at`/`invalid_at`, enables "what was true last week?" queries
- **stdio transport** — MCP client spawns the binary, communicates via stdin/stdout
- **`//go:embed` dashboard** — HTML/JS bundled into the binary, dashboard is a subcommand of the same executable

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

Estimated savings: ~$265/month for heavy users (3+ sessions/day)
```

The new progressive-disclosure search (`memorx_search_index` → `memorx_get_memory`) adds another ~10× savings on recall by filtering before fetching full content.

## Comparison

| Feature | memorX | claude-mem | Mem0 | Zep | KeepGoing |
|---------|--------|------------|------|-----|-----------|
| Automatic capture via hooks | Yes | Yes | No | No | Yes |
| Live local web dashboard | Yes | Yes | No | No | No |
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

## License

MIT
