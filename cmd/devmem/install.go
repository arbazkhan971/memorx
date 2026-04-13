package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runInstall wires memorx into Claude Code: hooks, MCP config, plugin
// manifest, and prints next steps. It's idempotent — re-running is safe.
func runInstall(args []string) error {
	force := false
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
		}
	}

	// 1. Binary location — users typically have `memorx` on PATH after
	// `go install`. We just record where we are.
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}
	fmt.Printf("memorx install: binary at %s\n", bin)

	// 2. Claude Code global settings hook config.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home: %w", err)
	}
	settingsDir := filepath.Join(home, ".claude")
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", settingsDir, err)
	}

	settings, err := readJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", settingsPath, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}

	if err := mergeMemorxHooks(settings, bin, force); err != nil {
		return err
	}
	if err := writeJSON(settingsPath, settings); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}
	fmt.Printf("memorx install: hooks written to %s\n", settingsPath)

	// 3. MCP server registration via `claude mcp add` if available.
	if _, err := exec.LookPath("claude"); err == nil {
		out, err := exec.Command("claude", "mcp", "add", "-s", "user", "--transport", "stdio", "memorx", "--", bin).CombinedOutput()
		if err != nil {
			fmt.Printf("memorx install: claude mcp add failed (non-fatal): %v\n%s\n", err, strings.TrimSpace(string(out)))
		} else {
			fmt.Printf("memorx install: registered as MCP server with Claude Code\n")
		}
	} else {
		fmt.Printf("memorx install: `claude` CLI not found — skipping MCP registration\n")
		fmt.Printf("   run manually: claude mcp add -s user --transport stdio memorx -- %s\n", bin)
	}

	// 4. memorX settings file.
	memDir := filepath.Join(home, ".memorx")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", memDir, err)
	}
	memSettingsPath := filepath.Join(memDir, "settings.json")
	if _, err := os.Stat(memSettingsPath); os.IsNotExist(err) {
		defaultSettings := map[string]any{
			"dashboard_port":  37778,
			"auto_observe":    true,
			"privacy_mode":    "strip", // strip <private> tags
			"transcript_dir":  filepath.Join(home, ".claude", "projects"),
		}
		if err := writeJSON(memSettingsPath, defaultSettings); err != nil {
			return fmt.Errorf("write %s: %w", memSettingsPath, err)
		}
		fmt.Printf("memorx install: created %s\n", memSettingsPath)
	}

	fmt.Println()
	fmt.Println("memorx install: done! Next:")
	fmt.Println("  1. Restart Claude Code so hooks take effect")
	fmt.Println("  2. Run `memorx dashboard` to open the web viewer")
	fmt.Println("  3. In your project, ask Claude to start a feature: \"let's begin a feature called <name>\"")
	return nil
}

// mergeMemorxHooks inserts memorx hook entries into Claude Code's
// settings.json hooks array. Non-memorx hooks are left alone.
func mergeMemorxHooks(settings map[string]any, bin string, force bool) error {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}

	events := map[string]string{
		"SessionStart":      "session-start",
		"UserPromptSubmit":  "user-prompt-submit",
		"PostToolUse":       "post-tool-use",
		"Stop":              "stop",
		"SessionEnd":        "session-end",
	}
	for evt, sub := range events {
		entry := map[string]any{
			"matcher": "",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": fmt.Sprintf("%s hook %s", bin, sub),
					"timeout": 10,
				},
			},
		}
		existing, _ := hooks[evt].([]any)
		if len(existing) == 0 || force {
			hooks[evt] = []any{entry}
			continue
		}
		// Don't clobber existing non-memorx hooks — append our entry if
		// it isn't already there.
		alreadyInstalled := false
		for _, e := range existing {
			if em, ok := e.(map[string]any); ok {
				if inner, ok := em["hooks"].([]any); ok {
					for _, h := range inner {
						if hm, ok := h.(map[string]any); ok {
							if cmd, _ := hm["command"].(string); strings.Contains(cmd, "memorx hook") {
								alreadyInstalled = true
							}
						}
					}
				}
			}
		}
		if !alreadyInstalled {
			hooks[evt] = append(existing, entry)
		}
	}
	return nil
}

func readJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
