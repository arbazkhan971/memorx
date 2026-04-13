package main

import (
	"fmt"
	"log"
	"os"

	"github.com/arbazkhan971/memorx/internal/git"
	memorx "github.com/arbazkhan971/memorx/internal/mcp"
	"github.com/arbazkhan971/memorx/internal/storage"
)

// version is stamped at build time via -ldflags "-X main.version=..."
var version = "dev"

const usage = `memorx - SOTA developer memory, single Go binary.

Usage:
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

The default (no args) launches the MCP server over stdio — this is what
Claude Code, Cursor, Codex and Windsurf invoke when memorx is wired as an
MCP server. All other subcommands are for installation, hooks and the
local dashboard.
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runMCPServer()
		return
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "version", "-v", "--version":
		fmt.Printf("memorx %s\n", version)
	case "install":
		if err := runInstall(rest); err != nil {
			log.Fatalf("install: %v", err)
		}
	case "dashboard":
		if err := runDashboard(rest); err != nil {
			log.Fatalf("dashboard: %v", err)
		}
	case "hook":
		if err := runHook(rest); err != nil {
			// Hooks should never hard-fail Claude Code sessions — log and exit 0.
			fmt.Fprintf(os.Stderr, "memorx hook: %v\n", err)
			os.Exit(0)
		}
	case "doctor":
		if err := runDoctor(rest); err != nil {
			log.Fatalf("doctor: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

// openProjectDB finds the git root, ensures .memory exists, opens the
// SQLite DB and runs migrations. Returns db, gitRoot, close func.
func openProjectDB() (*storage.DB, string, func(), error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", nil, fmt.Errorf("get working directory: %w", err)
	}
	gitRoot, err := git.FindGitRoot(cwd)
	if err != nil {
		return nil, "", nil, fmt.Errorf("not a git repository: %w", err)
	}
	memDir, err := git.EnsureMemoryDir(gitRoot)
	if err != nil {
		return nil, "", nil, fmt.Errorf("setup memory directory: %w", err)
	}
	db, err := storage.NewDB(memDir + "/memory.db")
	if err != nil {
		return nil, "", nil, fmt.Errorf("open database: %w", err)
	}
	if err := storage.Migrate(db); err != nil {
		db.Close()
		return nil, "", nil, fmt.Errorf("migrate database: %w", err)
	}
	return db, gitRoot, func() { db.Close() }, nil
}

func runMCPServer() {
	db, gitRoot, closeDB, err := openProjectDB()
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer closeDB()

	memDir, _ := git.EnsureMemoryDir(gitRoot)
	fmt.Fprintf(os.Stderr, "memorx: initialized at %s (project: %s)\n", memDir, git.ProjectName(gitRoot))

	srv := memorx.NewServer(db, gitRoot)
	if err := srv.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
