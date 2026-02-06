package tests

import (
	"testing"
	"time"

	"github.com/Jevs21/cctop/internal/session"
)

func TestParseEtime(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"30", 30 * time.Second},
		{"1:30", 90 * time.Second},
		{"01:30", 90 * time.Second},
		{"1:00:00", 1 * time.Hour},
		{"2:30:15", 2*time.Hour + 30*time.Minute + 15*time.Second},
		{"1-00:00:00", 24 * time.Hour},
		{"2-10:30:15", 2*24*time.Hour + 10*time.Hour + 30*time.Minute + 15*time.Second},
		{"  1:30", 90 * time.Second}, // leading whitespace
		{"0:05", 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := session.ParseEtime(tt.input)
			if result != tt.expected {
				t.Errorf("ParseEtime(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEncodePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/Users/me/project", "-Users-me-project"},
		{"/Users/me/my.project", "-Users-me-my-project"},
		{"/Users/me/a/b/c", "-Users-me-a-b-c"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := session.EncodePath(tt.input)
			if result != tt.expected {
				t.Errorf("EncodePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShortProjectName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/a/b/c", "b/c"},
		{"/Users/me/project", "me/project"},
		{"/project", "project"},
		{"/a/b/c/d", "c/d"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := session.ShortProjectName(tt.input)
			if result != tt.expected {
				t.Errorf("ShortProjectName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStatePriority(t *testing.T) {
	if session.StateActive.Priority() >= session.StateWaiting.Priority() {
		t.Error("expected active priority < waiting priority")
	}
	if session.StateWaiting.Priority() >= session.StateIdle.Priority() {
		t.Error("expected waiting priority < idle priority")
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state    session.State
		expected string
	}{
		{session.StateActive, "active"},
		{session.StateWaiting, "waiting"},
		{session.StateIdle, "idle"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if tt.state.String() != tt.expected {
				t.Errorf("State.String() = %q, want %q", tt.state.String(), tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds  int
		expected string
	}{
		{0, "0s"},
		{30, "30s"},
		{59, "59s"},
		{60, "1:00"},
		{90, "1:30"},
		{754, "12:34"},
		{3600, "1h00m"},
		{3661, "1h01m"},
		{7200, "2h00m"},
		{8100, "2h15m"},
		{86400, "1d0h"},
		{93600, "1d2h"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			duration := time.Duration(tt.seconds) * time.Second
			result := session.FormatDuration(duration)
			if result != tt.expected {
				t.Errorf("FormatDuration(%ds) = %q, want %q", tt.seconds, result, tt.expected)
			}
		})
	}
}

func TestParsePS(t *testing.T) {
	sampleOutput := `  PID   ELAPSED TTY      COMMAND
 1234     10:30 ttys001  /usr/local/bin/claude --help
 5678      5:00 ??       /usr/local/bin/claude
 9999     01:00 ttys002  /usr/bin/grep claude
`
	entries := session.ParsePS(sampleOutput)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (excluding grep), got %d", len(entries))
	}

	if entries[0].PID != 1234 {
		t.Errorf("expected PID 1234, got %d", entries[0].PID)
	}
	if entries[0].TTY != "ttys001" {
		t.Errorf("expected TTY ttys001, got %q", entries[0].TTY)
	}

	if entries[1].PID != 5678 {
		t.Errorf("expected PID 5678, got %d", entries[1].PID)
	}
	if entries[1].TTY != "??" {
		t.Errorf("expected TTY ??, got %q", entries[1].TTY)
	}
}
