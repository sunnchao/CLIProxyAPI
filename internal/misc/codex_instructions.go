// Package misc provides miscellaneous utility functions and embedded data for the CLI Proxy API.
// This package contains general-purpose helpers and embedded resources that do not fit into
// more specific domain packages. It includes embedded instructional text for Codex-related operations.
package misc

import (
	"embed"
	_ "embed"
	"strings"
)

//go:embed codex_instructions
var codexInstructionsDir embed.FS

func CodexInstructionsForModel(modelName, systemInstructions string) (bool, string) {
	entries, _ := codexInstructionsDir.ReadDir("codex_instructions")

	lastPrompt := ""
	lastCodexPrompt := ""
	// lastReviewPrompt := ""
	for _, entry := range entries {
		content, _ := codexInstructionsDir.ReadFile("codex_instructions/" + entry.Name())
		if strings.HasPrefix(systemInstructions, string(content)) {
			return true, ""
		}
		if strings.HasPrefix(entry.Name(), "gpt_5_codex_prompt.md") {
			lastCodexPrompt = string(content)
		} else if strings.HasPrefix(entry.Name(), "prompt.md") {
			lastPrompt = string(content)
		} else if strings.HasPrefix(entry.Name(), "review_prompt.md") {
			// lastReviewPrompt = string(content)
		}
	}

	if strings.Contains(modelName, "codex") {
		return false, lastCodexPrompt
	} else {
		return false, lastPrompt
	}
}
