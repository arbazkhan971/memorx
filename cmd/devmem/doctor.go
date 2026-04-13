package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runDoctor prints a diagnostic report: binary on PATH, git repo, DB
// reachable, hooks wired up, claude CLI present.
func runDoctor(_ []string) error {
	type check struct {
		name   string
		ok     bool
		detail string
	}
	var checks []check

	// Binary location
	bin, _ := os.Executable()
	checks = append(checks, check{"binary", bin != "", bin})

	// memorx on PATH?
	if p, err := exec.LookPath("memorx"); err == nil {
		checks = append(checks, check{"on PATH", true, p})
	} else {
		checks = append(checks, check{"on PATH", false, "memorx not found in $PATH — run `go install` or add the binary to PATH"})
	}

	// Git repo + DB
	db, gitRoot, closeDB, err := openProjectDB()
	if err != nil {
		checks = append(checks, check{"project DB", false, err.Error()})
	} else {
		defer closeDB()
		checks = append(checks, check{"project DB", true, filepath.Join(gitRoot, ".memory", "memory.db")})
		// Row counts sanity
		var nFeatures, nNotes int
		_ = db.Reader().QueryRow("SELECT COUNT(*) FROM features").Scan(&nFeatures)
		_ = db.Reader().QueryRow("SELECT COUNT(*) FROM notes").Scan(&nNotes)
		checks = append(checks, check{"memory rows", true, fmt.Sprintf("features=%d notes=%d", nFeatures, nNotes)})
	}

	// Claude Code settings hooks
	home, _ := os.UserHomeDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if b, err := os.ReadFile(settingsPath); err == nil {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if strings.Contains(string(b), "memorx hook") {
			checks = append(checks, check{"hooks installed", true, settingsPath})
		} else {
			checks = append(checks, check{"hooks installed", false, "run `memorx install` to wire hooks into Claude Code"})
		}
	} else {
		checks = append(checks, check{"hooks installed", false, "no " + settingsPath + " — run `memorx install`"})
	}

	// `claude` CLI?
	if p, err := exec.LookPath("claude"); err == nil {
		checks = append(checks, check{"claude CLI", true, p})
	} else {
		checks = append(checks, check{"claude CLI", false, "not found (optional; used for automatic MCP registration)"})
	}

	// Print
	failures := 0
	fmt.Println("memorx doctor")
	fmt.Println("-------------")
	for _, c := range checks {
		mark := "OK  "
		if !c.ok {
			mark = "FAIL"
			failures++
		}
		fmt.Printf("  [%s] %-20s %s\n", mark, c.name, c.detail)
	}
	fmt.Println()
	if failures == 0 {
		fmt.Println("all checks passed.")
	} else {
		fmt.Printf("%d check(s) failed.\n", failures)
	}
	return nil
}
