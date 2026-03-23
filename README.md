# devmem

A SOTA developer memory system. Local MCP server in Go that gives any coding CLI persistent, project-scoped session/feature memory.

## What it does

- **Feature tracking** — organize work by feature ("auth-v2", "billing-fix")
- **Session continuity** — picks up where you left off, across any MCP-compatible tool
- **Git integration** — auto-syncs commits with intent classification
- **Bi-temporal facts** — tracks what's true now AND what was true before
- **Plan persistence** — plans survive across sessions, auto-track progress from commits
- **Memory linking** — A-MEM style connections between related memories
- **Background consolidation** — detects contradictions, decays stale memories, generates summaries
- **3-layer search** — FTS5 + trigram + fuzzy across all memory types

## Install

```bash
go install github.com/arbaz/devmem/cmd/devmem@latest
```

## Setup

### Claude Code
```bash
claude mcp add -s user --transport stdio devmem -- devmem
```

### Cursor
Add to `.cursor/mcp.json`:
```json
{
    "mcpServers": {
        "devmem": { "command": "devmem", "transport": "stdio" }
    }
}
```

### Windsurf / Other MCP Clients
Add `devmem` as a stdio MCP server in your tool's MCP configuration.

## Usage

devmem auto-detects your project from the git root. No configuration needed.

### Tools

| Tool | What it does |
|------|-------------|
| `devmem_status` | Project overview, active feature, plan progress |
| `devmem_list_features` | All features with status and commit breakdown |
| `devmem_start_feature` | Create or resume a feature |
| `devmem_switch_feature` | Switch to a different feature |
| `devmem_get_context` | Where you left off (compact/standard/detailed) |
| `devmem_sync` | Pull git commits, classify intent, match to plan steps |
| `devmem_remember` | Save a note, decision, blocker, or next step |
| `devmem_search` | Search across all memory types |
| `devmem_save_plan` | Store a plan with trackable steps |

### Example Flow

```
You: "What am I working on?"
-> devmem_status shows 3 features, auth-v2 is active

You: "Let's continue billing-fix"
-> devmem_switch_feature loads full context from last session

You: (work, make commits)
-> devmem_sync pulls commits, matches to plan steps

You: "Remember: webhook handler done, need tests"
-> devmem_remember stores note, auto-links to related memories

Next day, different CLI tool:
-> devmem_get_context shows exactly where you left off
```

## How it works

- Single Go binary, zero external dependencies
- SQLite database at `<project>/.memory/memory.db`
- `current.json` human-readable snapshot always up to date
- WAL mode for concurrent access (multiple tools simultaneously)
- FTS5 for full-text search with BM25 ranking

## Architecture

```
MCP Client (Claude Code / Cursor / Codex)
    | stdio
    v
devmem (Go binary)
    |-- Session Manager (features, sessions)
    |-- Git Engine (commits, intent, sync)
    |-- Search Engine (FTS5 + trigram + scoring)
    |-- Plan Engine (CRUD, auto-detect, commit matching)
    |-- Memory Core (bi-temporal facts, notes, links)
    |-- Consolidation Engine (background goroutine)
    +-- SQLite (WAL mode, .memory/memory.db)
```

## License

MIT
