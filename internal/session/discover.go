package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// psEntry holds raw data parsed from a single ps output line.
type psEntry struct {
	PID     int
	Etime   string
	TTY     string
	Command string
}

// ideLockFile represents the JSON structure of an IDE lock file.
type ideLockFile struct {
	PID              int      `json:"pid"`
	WorkspaceFolders []string `json:"workspaceFolders"`
	IDEName          string   `json:"ideName"`
	Transport        string   `json:"transport"`
}

// DiscoverAll is the main orchestrator that finds all running Claude sessions.
// It performs a single ps call, a single batched lsof call, discovers both CLI
// and IDE sessions, deduplicates by CWD, and enriches with transcript metadata.
func DiscoverAll() []Session {
	claudeDir := filepath.Join(os.Getenv("HOME"), ".claude")

	// Single ps call for all Claude processes
	psOutput := runPS()
	entries := ParsePS(psOutput)

	if len(entries) == 0 {
		return nil
	}

	// Batch-resolve CWDs for all PIDs
	cwdMap := BatchResolveCWDs(entries)

	// Track seen CWDs for deduplication (IDE wins over CLI)
	seenCWDs := make(map[string]bool)
	var sessions []Session

	// Discover IDE sessions first (they have richer metadata from lock files)
	ideSessions := discoverIDESessions(claudeDir, entries, cwdMap)
	for i := range ideSessions {
		seenCWDs[ideSessions[i].CWD] = true
		sessions = append(sessions, ideSessions[i])
	}

	// Discover CLI sessions, skipping CWDs already claimed by IDE
	cliSessions := discoverCLISessions(entries, cwdMap, seenCWDs)
	sessions = append(sessions, cliSessions...)

	// Enrich all sessions with transcript metadata (topic, branch, state, messages)
	EnrichSessions(sessions, claudeDir)

	return sessions
}

// runPS executes ps and returns raw stdout.
func runPS() string {
	out, err := exec.Command("ps", "-eo", "pid,etime,tty,command").CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// ParsePS parses ps -eo pid,etime,tty,command output, filtering for "claude" processes.
func ParsePS(output string) []psEntry {
	var entries []psEntry
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "PID") {
			continue
		}

		// Only process lines containing "claude"
		if !strings.Contains(line, "claude") {
			continue
		}
		// Skip grep processes
		if strings.Contains(line, "grep") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		entries = append(entries, psEntry{
			PID:     pid,
			Etime:   fields[1],
			TTY:     fields[2],
			Command: strings.Join(fields[3:], " "),
		})
	}

	return entries
}

// BatchResolveCWDs resolves working directories for all PIDs in a single system call.
// On macOS, uses lsof. On Linux, reads /proc/<pid>/cwd.
func BatchResolveCWDs(entries []psEntry) map[int]string {
	cwdMap := make(map[int]string)

	if runtime.GOOS == "linux" {
		for _, entry := range entries {
			link, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", entry.PID))
			if err == nil {
				cwdMap[entry.PID] = link
			}
		}
		return cwdMap
	}

	// macOS: use lsof with batched PIDs
	if len(entries) == 0 {
		return cwdMap
	}

	pidStrs := make([]string, len(entries))
	for i, entry := range entries {
		pidStrs[i] = strconv.Itoa(entry.PID)
	}
	pidList := strings.Join(pidStrs, ",")

	out, err := exec.Command("lsof", "-a", "-p", pidList, "-d", "cwd", "-Fn").CombinedOutput()
	if err != nil {
		return cwdMap
	}

	// Parse lsof -Fn output: lines starting with 'p' are PIDs, 'n' are paths
	var currentPID int
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, parseErr := strconv.Atoi(line[1:])
			if parseErr == nil {
				currentPID = pid
			}
		case 'n':
			if currentPID != 0 {
				cwdMap[currentPID] = line[1:]
			}
		}
	}

	return cwdMap
}

// discoverCLISessions finds CLI-launched Claude sessions from ps entries.
// CLI sessions have a real TTY (not "??").
func discoverCLISessions(entries []psEntry, cwdMap map[int]string, seenCWDs map[string]bool) []Session {
	var sessions []Session

	for _, entry := range entries {
		// CLI sessions have a TTY (ttysNNN), IDE sessions have ??
		if entry.TTY == "??" {
			continue
		}

		// Ensure this is a top-level claude command
		commandParts := strings.Fields(entry.Command)
		if len(commandParts) == 0 {
			continue
		}
		baseName := filepath.Base(commandParts[0])
		if baseName != "claude" {
			continue
		}

		cwd, hasCWD := cwdMap[entry.PID]
		if !hasCWD || cwd == "" {
			continue
		}

		// Skip if this CWD was already claimed by an IDE session
		if seenCWDs[cwd] {
			continue
		}
		seenCWDs[cwd] = true

		duration := ParseEtime(entry.Etime)

		sessions = append(sessions, Session{
			PID:      entry.PID,
			CWD:      cwd,
			Source:   Source{Type: "CLI"},
			Project:  ShortProjectName(cwd),
			Duration: duration,
		})
	}

	return sessions
}

// discoverIDESessions finds IDE-launched Claude sessions from lock files.
func discoverIDESessions(claudeDir string, entries []psEntry, cwdMap map[int]string) []Session {
	var sessions []Session

	ideDir := filepath.Join(claudeDir, "ide")
	lockFiles, err := filepath.Glob(filepath.Join(ideDir, "*.lock"))
	if err != nil || len(lockFiles) == 0 {
		return sessions
	}

	for _, lockFilePath := range lockFiles {
		data, readErr := os.ReadFile(lockFilePath)
		if readErr != nil {
			continue
		}

		var lockFile ideLockFile
		if jsonErr := json.Unmarshal(data, &lockFile); jsonErr != nil {
			continue
		}

		if lockFile.PID == 0 || len(lockFile.WorkspaceFolders) == 0 {
			continue
		}

		// Verify the IDE process is still alive
		if !isProcessAlive(lockFile.PID) {
			continue
		}

		workspace := lockFile.WorkspaceFolders[0]
		ideName := shortenIDEName(lockFile.IDEName)

		// Find a claude process (TTY=??) whose CWD matches this workspace
		for _, entry := range entries {
			if entry.TTY != "??" {
				continue
			}

			cwd, hasCWD := cwdMap[entry.PID]
			if !hasCWD {
				continue
			}

			if strings.HasPrefix(cwd, workspace) {
				duration := ParseEtime(entry.Etime)
				sessions = append(sessions, Session{
					PID:      entry.PID,
					CWD:      workspace,
					Source:   Source{Type: ideName},
					Project:  ShortProjectName(workspace),
					Duration: duration,
				})
				break // One Claude process per workspace
			}
		}
	}

	return sessions
}

// isProcessAlive checks if a PID is running by sending signal 0.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// shortenIDEName converts verbose IDE names to short display names.
func shortenIDEName(fullName string) string {
	if strings.Contains(fullName, "Visual Studio Code") {
		return "VSCode"
	}
	if strings.Contains(fullName, "Cursor") {
		return "Cursor"
	}
	if fullName == "" {
		return "IDE"
	}
	return fullName
}

// ParseEtime parses ps etime format (DD-HH:MM:SS, HH:MM:SS, MM:SS, or SS) to a Duration.
func ParseEtime(etime string) time.Duration {
	etime = strings.TrimSpace(etime)

	var days int
	if idx := strings.IndexByte(etime, '-'); idx != -1 {
		d, err := strconv.Atoi(etime[:idx])
		if err == nil {
			days = d
		}
		etime = etime[idx+1:]
	}

	parts := strings.Split(etime, ":")
	var hours, minutes, seconds int

	switch len(parts) {
	case 3:
		hours, _ = strconv.Atoi(parts[0])
		minutes, _ = strconv.Atoi(parts[1])
		seconds, _ = strconv.Atoi(parts[2])
	case 2:
		minutes, _ = strconv.Atoi(parts[0])
		seconds, _ = strconv.Atoi(parts[1])
	case 1:
		seconds, _ = strconv.Atoi(parts[0])
	}

	totalSeconds := days*86400 + hours*3600 + minutes*60 + seconds
	return time.Duration(totalSeconds) * time.Second
}

// ShortProjectName returns the last 2 path components (e.g., "personal/myapp").
func ShortProjectName(fullPath string) string {
	fullPath = strings.TrimRight(fullPath, "/")
	name := filepath.Base(fullPath)
	parent := filepath.Base(filepath.Dir(fullPath))

	if parent == "/" || parent == "." || parent == "" {
		return name
	}
	return parent + "/" + name
}

// EncodePath converts a filesystem path to Claude's project directory format.
// Replaces / and . with -
func EncodePath(path string) string {
	result := strings.ReplaceAll(path, "/", "-")
	result = strings.ReplaceAll(result, ".", "-")
	return result
}
