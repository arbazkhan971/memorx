package mcp

import (
	"context"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// --- Wave 11: Offline Collaboration handlers ---

func (s *DevMemServer) handleGitSync(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action, errRes := requireParam(req, "action")
	if errRes != nil {
		return errRes, nil
	}
	syncPath := getStringArg(req, "path", "")

	switch action {
	case "export":
		chunkPath, count, err := s.store.GitSyncExport(syncPath)
		if err != nil {
			return respondErr("Git sync export failed: %v", err)
		}
		if count == 0 {
			return respond("# Git Sync Export\n\nNo new memories to export since last sync.")
		}
		return respond("# Git Sync Export\n\n- **Entries exported:** %d\n- **Chunk file:** %s\n\nChunks are immutable and append-only. Commit and push the sync directory to share with teammates.", count, chunkPath)

	case "import":
		count, err := s.store.GitSyncImport(syncPath)
		if err != nil {
			return respondErr("Git sync import failed: %v", err)
		}
		if count == 0 {
			return respond("# Git Sync Import\n\nNo new chunks to import. Already up to date.")
		}
		return respond("# Git Sync Import\n\n- **Entries imported:** %d\n\nNew memories have been merged into the local database.", count)

	default:
		return respondErr("Invalid action %q. Use 'export' or 'import'.", action)
	}
}

func (s *DevMemServer) handleTeamDecisions(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	action, errRes := requireParam(req, "action")
	if errRes != nil {
		return errRes, nil
	}
	path := getStringArg(req, "path", "")

	switch action {
	case "export":
		outPath, count, err := s.store.TeamDecisionsExport(path)
		if err != nil {
			return respondErr("Decision export failed: %v", err)
		}
		if count == 0 {
			return respond("# Team Decisions Export\n\nNo decisions to export. Use memorx_remember with type=\"decision\" to record decisions first.")
		}
		return respond("# Team Decisions Export\n\n- **Decisions exported:** %d\n- **File:** %s\n\nShare this file with teammates for import.", count, outPath)

	case "import":
		imported, skipped, err := s.store.TeamDecisionsImport(path)
		if err != nil {
			return respondErr("Decision import failed: %v", err)
		}
		return respond("# Team Decisions Import\n\n- **Imported:** %d new decisions\n- **Skipped:** %d (already exist, by content hash)", imported, skipped)

	default:
		return respondErr("Invalid action %q. Use 'export' or 'import'.", action)
	}
}

func (s *DevMemServer) handleConflictDetect(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	conflicts, err := s.store.DetectConflicts()
	if err != nil {
		return respondErr("Conflict detection failed: %v", err)
	}

	if len(conflicts) == 0 {
		return respond("# Conflict Detection\n\nNo contradictions found. Team memory is consistent.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Conflict Detection\n\n**%d contradiction(s) found:**\n\n", len(conflicts))
	for i, c := range conflicts {
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, c.Topic)
		fmt.Fprintf(&b, "- **A:** %s (from %s)\n", c.NoteA, c.ValueA)
		fmt.Fprintf(&b, "- **B:** %s (from %s)\n\n", c.NoteB, c.ValueB)
	}
	b.WriteString("_Resolve contradictions by updating or invalidating one of the conflicting entries._\n")
	return mcplib.NewToolResultText(b.String()), nil
}

// --- Wave 12: Doc Automation handlers ---

func (s *DevMemServer) handleGenerateADR(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	decisionID := getStringArg(req, "decision_id", "")
	doc, err := s.store.GenerateADR(decisionID)
	if err != nil {
		return respondErr("Failed to generate ADR: %v", err)
	}
	return mcplib.NewToolResultText(doc), nil
}

func (s *DevMemServer) handleGenerateReadme(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	output := getStringArg(req, "output", "")
	content, err := s.store.GenerateReadme(s.gitRoot, output)
	if err != nil {
		return respondErr("Failed to generate README: %v", err)
	}

	outPath := output
	if outPath == "" {
		outPath = s.gitRoot + "/README.md"
	}
	return respond("# README Generated\n\nWritten to: %s\n\n---\n\n%s", outPath, content)
}

func (s *DevMemServer) handleGenerateAPIDocs(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	doc, err := s.store.GenerateAPIDocs()
	if err != nil {
		return respondErr("Failed to generate API docs: %v", err)
	}
	return mcplib.NewToolResultText(doc), nil
}

func (s *DevMemServer) handleGenerateRunbook(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	feature := getStringArg(req, "feature", "")
	doc, err := s.store.GenerateRunbook(feature)
	if err != nil {
		return respondErr("Failed to generate runbook: %v", err)
	}
	return mcplib.NewToolResultText(doc), nil
}
