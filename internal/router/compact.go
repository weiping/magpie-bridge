package router

import (
	"strings"
)

// IsCompactPrompt detects whether a prompt is a context compaction or
// summarization instruction from Claude Code, Codex, Pi, or direct user command.
func IsCompactPrompt(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return false
	}
	lower := strings.ToLower(p)

	// Direct slash commands or command prefixes
	if lower == "/compact" || strings.HasPrefix(lower, "/compact ") ||
		lower == "/autocompact" || strings.HasPrefix(lower, "/autocompact ") ||
		lower == "compact" || strings.HasPrefix(lower, "compact ") ||
		lower == "autocompact" || strings.HasPrefix(lower, "autocompact ") {
		return true
	}

	// Chinese command keywords
	if strings.Contains(p, "压缩上下文") ||
		strings.Contains(p, "压缩会话") ||
		strings.Contains(p, "压缩对话") ||
		strings.Contains(p, "清理上下文") ||
		strings.Contains(p, "总结上下文") {
		return true
	}

	// Claude Code compaction prompt signatures
	if strings.Contains(p, "Summarize the transcript inside <summary></summary> tags") ||
		strings.Contains(p, "Your task is to create a detailed summary of this conversation") ||
		strings.Contains(p, "This conversation is being compacted") ||
		strings.Contains(p, "You are a context compaction engine") {
		return true
	}

	// Codex compaction prompt signatures
	if strings.Contains(p, "CONTEXT CHECKPOINT COMPACTION") ||
		strings.Contains(p, "Create a handoff summary for another LLM") ||
		strings.Contains(p, "Another language model started to solve this problem and produced a summary") ||
		strings.Contains(p, "compaction_trigger") {
		return true
	}

	// Pi compaction prompt signatures
	if strings.Contains(p, "You are a context summarization assistant") ||
		strings.Contains(p, "The messages above are a conversation to summarize") ||
		strings.Contains(p, "The messages above are NEW conversation messages to incorporate into the existing summary") ||
		strings.Contains(p, "The messages above are earlier context from an ongoing conversation") ||
		strings.Contains(p, "Create a structured context checkpoint summary") {
		return true
	}

	return false
}
