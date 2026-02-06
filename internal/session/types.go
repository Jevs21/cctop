package session

import (
	"fmt"
	"time"
)

// State represents a session's current activity state.
type State int

const (
	StateActive  State = iota // Claude is generating or processing
	StateWaiting              // Claude has responded, awaiting user input
	StateIdle                 // Session exists but has been inactive
)

// String returns the human-readable name for a State.
func (s State) String() string {
	switch s {
	case StateActive:
		return "active"
	case StateWaiting:
		return "waiting"
	case StateIdle:
		return "idle"
	default:
		return "unknown"
	}
}

// Priority returns the sort priority for a State (lower = higher priority).
func (s State) Priority() int {
	return int(s)
}

// Source represents how a Claude session was launched.
type Source struct {
	Type string // "CLI", "VSCode", "Cursor", or other IDE name
}

// String returns the display name of the source.
func (s Source) String() string {
	return s.Type
}

// Session holds all discoverable metadata for a single Claude Code session.
type Session struct {
	PID      int
	CWD      string
	State    State
	Source   Source
	Project  string        // Last 2 path components of the working directory
	Topic    string        // Cleaned first user prompt
	Branch   string        // Git branch from the transcript
	Duration time.Duration // Wall-clock duration since process started
	Messages int           // Approximate message count
}

// FormatDuration renders a duration as a compact human-readable string.
// Examples: 45s, 12:34, 2h15m, 3d14h
func FormatDuration(duration time.Duration) string {
	totalSeconds := int(duration.Seconds())
	if totalSeconds < 0 {
		return "0s"
	}

	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d:%02d", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
