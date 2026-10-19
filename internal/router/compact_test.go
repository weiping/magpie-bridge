package router

import (
	"testing"
)

func TestIsCompactPrompt(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		// Slash commands
		{"slash compact", "/compact", true},
		{"slash compact with arg", "/compact focus on auth", true},
		{"slash autocompact", "/autocompact", true},
		{"slash autocompact with arg", "/autocompact 100000", true},
		{"bare compact", "compact", true},
		{"bare compact with arg", "compact conversation history", true},
		{"bare autocompact", "autocompact", true},

		// Chinese phrases
		{"chinese 1", "帮我压缩上下文", true},
		{"chinese 2", "请压缩会话，保留当前状态", true},
		{"chinese 3", "压缩对话", true},
		{"chinese 4", "总结上下文", true},
		{"chinese 5", "清理上下文", true},

		// Claude Code auto-compact signatures
		{"claude code summary tags", "Summarize the transcript inside <summary></summary> tags. Include relevant information...", true},
		{"claude code task detailed summary", "Your task is to create a detailed summary of this conversation. This summary will be placed at the start of a continuing session...", true},
		{"claude code conversation being compacted", "This conversation is being compacted. Your reply replaces everything above: the next context window opens with what you write inside <summary></summary> tags...", true},
		{"claude code context compaction engine", "You are a context compaction engine. Your summary will REPLACE the original messages in the conversation context window.", true},

		// Codex auto-compact signatures
		{"codex context checkpoint compaction", "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.\n\nInclude:\n- Current progress and key decisions made", true},
		{"codex handoff summary", "Create a handoff summary for another LLM that will resume the task.", true},
		{"codex another language model summary", "Another language model started to solve this problem and produced a summary of its thinking process.", true},
		{"codex compaction trigger", "compaction_trigger: please summarize", true},

		// Pi auto-compact signatures
		{"pi summarization assistant", "You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary...", true},
		{"pi conversation to summarize", "The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.", true},
		{"pi new messages to incorporate", "The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.", true},
		{"pi earlier context", "The messages above are earlier context from an ongoing conversation. Later messages are stored separately...", true},
		{"pi structured context checkpoint", "Create a structured context checkpoint summary following the format.", true},

		// Negative cases
		{"empty string", "", false},
		{"whitespace", "   ", false},
		{"ordinary question", "how to write a loop in python", false},
		{"compact in sentence", "how to write a compact sql query", false},
		{"compact code", "make this code more compact", false},
		{"compact disk", "history of the compact disc", false},
		{"summary of an article", "give me a summary of article.pdf", false},
		{"git diff", "git diff HEAD~1", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsCompactPrompt(tc.prompt)
			if got != tc.want {
				t.Fatalf("IsCompactPrompt(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}
