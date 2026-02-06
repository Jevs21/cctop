package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jevs21/cctop/internal/session"
)

func TestCleanTopic_IDESelection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "text after closing tag",
			input:    "<ide_selection>some code here</ide_selection> Fix the bug in this function",
			expected: "Fix the bug in this function",
		},
		{
			name:     "only wrapper with no useful text after",
			input:    "<ide_selection>file.ts: const x = 1; This may or may not be relevant</ide_selection>",
			expected: "const x = 1;",
		},
		{
			name:     "empty selection returns fallback",
			input:    "<ide_selection></ide_selection>",
			expected: "(IDE selection)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.CleanTopic(tt.input)
			if result != tt.expected {
				t.Errorf("CleanTopic(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanTopic_IDEOpenedFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "opened file with path",
			input:    "<ide_opened_file>User opened the file /Users/me/project/main.go in the IDE</ide_opened_file>",
			expected: "(opened main.go)",
		},
		{
			name:     "opened file without path match",
			input:    "<ide_opened_file>something else</ide_opened_file>",
			expected: "(IDE context)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.CleanTopic(tt.input)
			if result != tt.expected {
				t.Errorf("CleanTopic(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanTopic_XMLTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strip simple tags",
			input:    "<tag>Hello</tag> World",
			expected: "Hello World",
		},
		{
			name:     "strip nested tags",
			input:    "<outer><inner>text</inner></outer> remaining",
			expected: "text remaining",
		},
		{
			name:     "no tags",
			input:    "Just a plain prompt",
			expected: "Just a plain prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.CleanTopic(tt.input)
			if result != tt.expected {
				t.Errorf("CleanTopic(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanTopic_NoisePrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "caveat prefix",
			input:    "Caveat: some warning. Actual prompt here",
			expected: "Actual prompt here",
		},
		{
			name:     "implement plan prefix",
			input:    "Implement the following plan: Do something",
			expected: "Do something",
		},
		{
			name:     "heading prefix",
			input:    "# Fix the login page",
			expected: "Fix the login page",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no prompt",
			input:    "No prompt",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.CleanTopic(tt.input)
			if result != tt.expected {
				t.Errorf("CleanTopic(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetectState(t *testing.T) {
	// Create a temp JSONL file for testing
	tmpDir := t.TempDir()

	t.Run("recently modified file is active", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "recent.jsonl")
		writeTestFile(t, filePath, `{"type":"user","message":{"role":"user","content":"hello"}}`)

		now := time.Now()
		mtime := now.Add(-10 * time.Second) // 10 seconds ago
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateActive {
			t.Errorf("expected StateActive, got %v", state)
		}
	})

	t.Run("progress type is active", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "progress.jsonl")
		writeTestFile(t, filePath, `{"type":"progress"}`)

		now := time.Now()
		mtime := now.Add(-60 * time.Second) // 60 seconds ago
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateActive {
			t.Errorf("expected StateActive for progress type, got %v", state)
		}
	})

	t.Run("assistant role is waiting", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "assistant.jsonl")
		writeTestFile(t, filePath, `{"type":"assistant","message":{"role":"assistant","content":"done"}}`)

		now := time.Now()
		mtime := now.Add(-60 * time.Second) // 60 seconds ago
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateWaiting {
			t.Errorf("expected StateWaiting for assistant role, got %v", state)
		}
	})

	t.Run("recent user message is active", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "user_recent.jsonl")
		writeTestFile(t, filePath, `{"type":"user","message":{"role":"user","content":"do this"}}`)

		now := time.Now()
		mtime := now.Add(-120 * time.Second) // 2 minutes ago (within 5 min)
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateActive {
			t.Errorf("expected StateActive for recent user message, got %v", state)
		}
	})

	t.Run("old user message is idle", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "user_old.jsonl")
		writeTestFile(t, filePath, `{"type":"user","message":{"role":"user","content":"do this"}}`)

		now := time.Now()
		mtime := now.Add(-10 * time.Minute) // 10 minutes ago
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateIdle {
			t.Errorf("expected StateIdle for old user message, got %v", state)
		}
	})

	t.Run("nonexistent file is idle", func(t *testing.T) {
		now := time.Now()
		state := session.DetectState("/nonexistent/path.jsonl", time.Time{}, now)
		if state != session.StateIdle {
			t.Errorf("expected StateIdle for nonexistent file, got %v", state)
		}
	})

	t.Run("assistant with AskUserQuestion is input state", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "ask_user.jsonl")
		content := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I need to ask you something."},{"type":"tool_use","id":"toolu_123","name":"AskUserQuestion","input":{}}]}}`
		writeTestFile(t, filePath, content)

		now := time.Now()
		mtime := now.Add(-60 * time.Second)
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateInput {
			t.Errorf("expected StateInput for AskUserQuestion tool use, got %v", state)
		}
	})

	t.Run("assistant with other tool use is waiting state", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "other_tool.jsonl")
		content := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me read a file."},{"type":"tool_use","id":"toolu_456","name":"Read","input":{}}]}}`
		writeTestFile(t, filePath, content)

		now := time.Now()
		mtime := now.Add(-60 * time.Second)
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateWaiting {
			t.Errorf("expected StateWaiting for non-AskUserQuestion tool use, got %v", state)
		}
	})

	t.Run("assistant with text only content blocks is waiting state", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "text_only.jsonl")
		content := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Here is my response."}]}}`
		writeTestFile(t, filePath, content)

		now := time.Now()
		mtime := now.Add(-60 * time.Second)
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateWaiting {
			t.Errorf("expected StateWaiting for text-only assistant message, got %v", state)
		}
	})

	t.Run("assistant with string content is waiting state", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "string_content.jsonl")
		content := `{"type":"assistant","message":{"role":"assistant","content":"Just a string response"}}`
		writeTestFile(t, filePath, content)

		now := time.Now()
		mtime := now.Add(-60 * time.Second)
		os.Chtimes(filePath, mtime, mtime)

		state := session.DetectState(filePath, mtime, now)
		if state != session.StateWaiting {
			t.Errorf("expected StateWaiting for string content, got %v", state)
		}
	})
}

func TestReadLastLine(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("single line file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "single.jsonl")
		writeTestFile(t, filePath, `{"type":"user"}`)

		result := session.ReadLastLine(filePath)
		if result != `{"type":"user"}` {
			t.Errorf("expected single line content, got %q", result)
		}
	})

	t.Run("multi line file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "multi.jsonl")
		writeTestFile(t, filePath, `{"type":"user","message":{"role":"user"}}
{"type":"assistant","message":{"role":"assistant"}}
{"type":"progress"}`)

		result := session.ReadLastLine(filePath)
		if result != `{"type":"progress"}` {
			t.Errorf("expected last line, got %q", result)
		}
	})

	t.Run("file with trailing newline", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "trailing.jsonl")
		writeTestFile(t, filePath, "{\"type\":\"assistant\"}\n")

		result := session.ReadLastLine(filePath)
		if result != `{"type":"assistant"}` {
			t.Errorf("expected content without trailing newline, got %q", result)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "empty.jsonl")
		writeTestFile(t, filePath, "")

		result := session.ReadLastLine(filePath)
		if result != "" {
			t.Errorf("expected empty string for empty file, got %q", result)
		}
	})
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
