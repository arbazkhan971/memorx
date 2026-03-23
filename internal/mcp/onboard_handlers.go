package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func (s *DevMemServer) handleOnboard(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	feature := getStringArg(req, "feature", "")
	doc, err := s.store.GenerateOnboarding(feature)
	if err != nil {
		return respondErr("Failed to generate onboarding doc: %v", err)
	}
	return mcplib.NewToolResultText(doc), nil
}

func (s *DevMemServer) handleChangelog(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	days := getIntArg(req, "days", 7)
	format := getStringArg(req, "format", "markdown")
	cl, err := s.store.GenerateChangelog(days, format)
	if err != nil {
		return respondErr("Failed to generate changelog: %v", err)
	}
	return mcplib.NewToolResultText(cl), nil
}

func (s *DevMemServer) handleShare(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	path, errRes := requireParam(req, "path")
	if errRes != nil {
		return errRes, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return respondErr("Failed to read file %s: %v", path, err)
	}

	// Try JSON parse first
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		// Try to parse as simple key-value markdown
		data = parseMarkdownExport(string(content))
		if data == nil {
			return respondErr("Failed to parse %s: not valid JSON or recognized markdown format", path)
		}
	}

	result, err := s.store.ImportSharedMemory(data)
	if err != nil {
		return respondErr("Import failed: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Import complete\n\n")
	fmt.Fprintf(&b, "- Features: %d\n", result.Features)
	fmt.Fprintf(&b, "- Notes: %d\n", result.Notes)
	fmt.Fprintf(&b, "- Facts: %d\n", result.Facts)
	if len(result.Errors) > 0 {
		fmt.Fprintf(&b, "\n**Warnings:** %d errors during import\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	return mcplib.NewToolResultText(b.String()), nil
}

// parseMarkdownExport attempts to parse a markdown export into a map
// suitable for ImportSharedMemory. Returns nil if parsing fails.
func parseMarkdownExport(content string) map[string]interface{} {
	lines := strings.Split(content, "\n")
	data := map[string]interface{}{}

	// Look for "# Feature: <name>" header
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# Feature: ") {
			data["feature"] = strings.TrimPrefix(line, "# Feature: ")
			break
		}
		if strings.HasPrefix(line, "# ") {
			// Generic header, use as feature name
			name := strings.TrimPrefix(line, "# ")
			name = strings.TrimSpace(name)
			if name != "" {
				data["feature"] = name
			}
			break
		}
	}

	if _, ok := data["feature"]; !ok {
		return nil
	}

	// Extract sections
	currentSection := ""
	var currentItems []interface{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			// Flush previous section
			if currentSection != "" && len(currentItems) > 0 {
				data[currentSection] = currentItems
			}
			section := strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			currentItems = nil
			switch {
			case strings.Contains(section, "decision"):
				currentSection = "decisions"
			case strings.Contains(section, "blocker"):
				currentSection = "blockers"
			case strings.Contains(section, "progress"):
				currentSection = "progress_notes"
			case strings.Contains(section, "fact"):
				currentSection = "facts"
			case strings.Contains(section, "note"):
				currentSection = "notes"
			default:
				currentSection = ""
			}
			continue
		}
		if currentSection != "" && strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimPrefix(trimmed, "- ")
			// Strip markdown bold timestamps like **[2024-01-15 10:00:00]**
			if idx := strings.Index(item, "** "); idx >= 0 && strings.HasPrefix(item, "**[") {
				item = item[idx+3:]
			}
			if currentSection == "facts" {
				// Try to parse "subject **predicate** object" format
				parts := strings.SplitN(item, " ", 3)
				if len(parts) >= 3 {
					pred := strings.TrimPrefix(strings.TrimSuffix(parts[1], "**"), "**")
					currentItems = append(currentItems, map[string]interface{}{
						"subject":   parts[0],
						"predicate": pred,
						"object":    parts[2],
					})
				}
			} else {
				currentItems = append(currentItems, item)
			}
		}
	}
	// Flush last section
	if currentSection != "" && len(currentItems) > 0 {
		data[currentSection] = currentItems
	}

	// Extract description from **Description:** line
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "**Description:**") {
			data["description"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "**Description:**"))
			break
		}
	}

	return data
}
