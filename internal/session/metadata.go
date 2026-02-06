package session

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// cachedMetadata stores transcript metadata keyed by cwd + mtime.
type cachedMetadata struct {
	FullPath string
	Topic    string
	Messages int
	Branch   string
}

// metadataCache persists across refresh cycles.
// Key: "cwd:mtime_unix"
var metadataCache = make(map[string]cachedMetadata)

// sessionsIndexEntry represents one entry in sessions-index.json.
type sessionsIndexEntry struct {
	SessionID    string `json:"sessionId"`
	FullPath     string `json:"fullPath"`
	FirstPrompt  string `json:"firstPrompt"`
	MessageCount int    `json:"messageCount"`
	FileMtime    int64  `json:"fileMtime"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"gitBranch"`
}

// sessionsIndex represents the sessions-index.json file.
type sessionsIndex struct {
	Entries []sessionsIndexEntry `json:"entries"`
}

// jsonlLine represents the relevant fields from a JSONL transcript line.
type jsonlLine struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Slug      string `json:"slug"`
	GitBranch string `json:"gitBranch"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
}

// EnrichSessions adds state, topic, branch, and message count to each session
// by reading transcript files from the Claude projects directory.
func EnrichSessions(sessions []Session, claudeDir string) {
	projectsDir := filepath.Join(claudeDir, "projects")
	now := time.Now()

	for i := range sessions {
		cwd := sessions[i].CWD
		enrichSession(&sessions[i], projectsDir, cwd, now)
	}
}

// enrichSession populates a single session's metadata fields.
func enrichSession(session *Session, projectsDir string, cwd string, now time.Time) {
	session.State = StateIdle

	encodedPath := EncodePath(cwd)
	projectDir := filepath.Join(projectsDir, encodedPath)

	// Try sessions-index.json first
	indexPath := filepath.Join(projectDir, "sessions-index.json")
	fullPath, firstPrompt, messageCount, gitBranch, found := findSessionFromIndex(indexPath)

	// Fallback: find newest JSONL file
	if !found {
		fullPath, firstPrompt, messageCount, gitBranch, found = findSessionFallback(projectDir)
	}

	if !found {
		return
	}

	// Check file mtime for caching
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return
	}
	mtime := fileInfo.ModTime()
	cacheKey := cwd + ":" + mtime.Format(time.RFC3339Nano)

	if cached, ok := metadataCache[cacheKey]; ok {
		// Cache hit — reuse topic, messages, branch; always recompute state
		session.Topic = cached.Topic
		session.Messages = cached.Messages
		session.Branch = cached.Branch
		session.State = DetectState(cached.FullPath, mtime, now)
		return
	}

	// Cache miss — compute everything
	topic := CleanTopic(firstPrompt)

	// Fall back to slug or session ID if topic is empty
	if topic == "" {
		lastLine := ReadLastLine(fullPath)
		if lastLine != "" {
			var lastEntry jsonlLine
			if jsonErr := json.Unmarshal([]byte(lastLine), &lastEntry); jsonErr == nil {
				if lastEntry.Slug != "" {
					topic = lastEntry.Slug
				} else if lastEntry.SessionID != "" && len(lastEntry.SessionID) >= 8 {
					topic = lastEntry.SessionID[:8]
				}
			}
		}
	}

	session.Topic = topic
	session.Messages = messageCount
	session.Branch = gitBranch
	session.State = DetectState(fullPath, mtime, now)

	// Store in cache
	metadataCache[cacheKey] = cachedMetadata{
		FullPath: fullPath,
		Topic:    topic,
		Messages: messageCount,
		Branch:   gitBranch,
	}
}

// findSessionFromIndex reads sessions-index.json and returns the most recent session.
func findSessionFromIndex(indexPath string) (fullPath string, firstPrompt string, messageCount int, gitBranch string, found bool) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return "", "", 0, "", false
	}

	var index sessionsIndex
	if jsonErr := json.Unmarshal(data, &index); jsonErr != nil {
		return "", "", 0, "", false
	}

	if len(index.Entries) == 0 {
		return "", "", 0, "", false
	}

	// Sort by fileMtime descending to find the most recent
	sort.Slice(index.Entries, func(i, j int) bool {
		return index.Entries[i].FileMtime > index.Entries[j].FileMtime
	})

	entry := index.Entries[0]
	if entry.FullPath == "" {
		return "", "", 0, "", false
	}

	// Truncate firstPrompt to 200 chars
	prompt := entry.FirstPrompt
	if len(prompt) > 200 {
		prompt = prompt[:200]
	}

	return entry.FullPath, prompt, entry.MessageCount, entry.GitBranch, true
}

// findSessionFallback finds the most recently modified JSONL file in a project directory.
func findSessionFallback(projectDir string) (fullPath string, firstPrompt string, messageCount int, gitBranch string, found bool) {
	matches, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	if err != nil || len(matches) == 0 {
		return "", "", 0, "", false
	}

	// Find the newest JSONL file by mtime
	var newestPath string
	var newestTime time.Time

	for _, matchPath := range matches {
		info, statErr := os.Stat(matchPath)
		if statErr != nil {
			continue
		}
		if newestPath == "" || info.ModTime().After(newestTime) {
			newestPath = matchPath
			newestTime = info.ModTime()
		}
	}

	if newestPath == "" {
		return "", "", 0, "", false
	}

	// Read first 30 lines to find the first user message
	firstPrompt = extractFirstPrompt(newestPath)

	// Count lines for approximate message count
	messageCount = countLines(newestPath)

	// Read last line for gitBranch and slug
	lastLine := ReadLastLine(newestPath)
	if lastLine != "" {
		var lastEntry jsonlLine
		if jsonErr := json.Unmarshal([]byte(lastLine), &lastEntry); jsonErr == nil {
			gitBranch = lastEntry.GitBranch
			if firstPrompt == "" && lastEntry.Slug != "" {
				firstPrompt = lastEntry.Slug
			}
		}
	}

	return newestPath, firstPrompt, messageCount, gitBranch, true
}

// extractFirstPrompt scans the first 30 lines of a JSONL file for the first
// meaningful user message, skipping system-generated messages.
func extractFirstPrompt(jsonlPath string) string {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines
	lineCount := 0

	for scanner.Scan() && lineCount < 30 {
		lineCount++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry jsonlLine
		if jsonErr := json.Unmarshal([]byte(line), &entry); jsonErr != nil {
			continue
		}

		if entry.Type != "user" {
			continue
		}

		text := extractMessageText(entry.Message.Content)

		// Skip system-generated messages
		if strings.HasPrefix(text, "[Request interrupted") || strings.HasPrefix(text, "[Tool use") {
			continue
		}

		if len(text) > 200 {
			text = text[:200]
		}

		return text
	}

	return ""
}

// extractMessageText extracts the text content from a message.content field,
// which can be either a string or an array of content blocks.
func extractMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try as string first
	var textContent string
	if err := json.Unmarshal(raw, &textContent); err == nil {
		return textContent
	}

	// Try as array of content blocks
	var contentBlocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &contentBlocks); err == nil {
		var texts []string
		for _, block := range contentBlocks {
			if block.Type == "text" && block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, " ")
	}

	return ""
}

// countLines returns the number of lines in a file.
func countLines(filePath string) int {
	file, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count
}

// DetectState determines the session state using the 5-rule state machine from the spec.
//  1. If the file was modified within the last 30 seconds → active
//  2. If the last line has type "progress" → active
//  3. If the last line has message.role "assistant" → waiting
//  4. If the last line has message.role "user" and file is < 5 minutes old → active
//  5. Otherwise → idle
func DetectState(jsonlPath string, mtime time.Time, now time.Time) State {
	age := now.Sub(mtime)

	// Rule 1: recently modified → active
	if age < 30*time.Second {
		return StateActive
	}

	// Read last line for type/role checks
	lastLine := ReadLastLine(jsonlPath)
	if lastLine == "" {
		return StateIdle
	}

	var entry jsonlLine
	if err := json.Unmarshal([]byte(lastLine), &entry); err != nil {
		return StateIdle
	}

	// Rule 2: progress type → active
	if entry.Type == "progress" {
		return StateActive
	}

	// Rule 3: assistant role → waiting
	if entry.Message.Role == "assistant" {
		return StateWaiting
	}

	// Rule 4: user role + recent → active
	if entry.Message.Role == "user" && age < 5*time.Minute {
		return StateActive
	}

	// Rule 5: default → idle
	return StateIdle
}

// ReadLastLine reads the last non-empty line of a file by seeking from the end.
// This avoids reading the entire file into memory.
func ReadLastLine(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return ""
	}

	// Read backwards from end of file to find the last newline
	buf := make([]byte, 0, 4096)
	fileSize := info.Size()
	offset := fileSize

	for offset > 0 {
		readSize := int64(4096)
		if readSize > offset {
			readSize = offset
		}
		offset -= readSize

		chunk := make([]byte, readSize)
		_, readErr := file.ReadAt(chunk, offset)
		if readErr != nil && readErr != io.EOF {
			return ""
		}

		buf = append(chunk, buf...)

		// Look for the second-to-last newline (last line boundary)
		content := string(buf)
		content = strings.TrimRight(content, "\n")
		if lastNewline := strings.LastIndexByte(content, '\n'); lastNewline >= 0 {
			return content[lastNewline+1:]
		}
	}

	// No newline found — the entire file is one line
	return strings.TrimRight(string(buf), "\n")
}

// xmlTagRegex matches XML-style tags like <tag>, </tag>, <tag attr="val">, etc.
var xmlTagRegex = regexp.MustCompile(`<[^>]+>`)

// noiseRegex matches common noise prefixes from restored/resumed sessions.
var noiseRegex = regexp.MustCompile(`(?m)^(Caveat:[^.]*\. |Implement the following plan: *|# |DO NOT .*$|IMPORTANT:.*$|The messages below.*$|No prompt$)`)

// CleanTopic strips IDE tags and system noise from a first prompt to produce
// a human-readable topic string.
func CleanTopic(prompt string) string {
	if prompt == "" {
		return ""
	}

	// Strip <ide_selection>...</ide_selection> wrapper
	if strings.HasPrefix(prompt, "<ide_selection>") {
		// Try to get text after the closing tag
		if idx := strings.Index(prompt, "</ide_selection>"); idx != -1 {
			afterTag := strings.TrimSpace(prompt[idx+len("</ide_selection>"):])
			if afterTag != "" {
				prompt = afterTag
			} else {
				// Try to extract inner text
				inner := prompt[len("<ide_selection>"):idx]
				if cutIdx := strings.Index(inner, "This may or may not"); cutIdx != -1 {
					inner = inner[:cutIdx]
				}
				if colonIdx := strings.Index(inner, ": "); colonIdx != -1 {
					inner = inner[colonIdx+2:]
				}
				inner = strings.TrimSpace(inner)
				if inner != "" {
					prompt = inner
				} else {
					return "(IDE selection)"
				}
			}
		}
	}

	// Strip <ide_opened_file>...</ide_opened_file>
	if strings.HasPrefix(prompt, "<ide_opened_file>") {
		if idx := strings.Index(prompt, "opened the file "); idx != -1 {
			rest := prompt[idx+len("opened the file "):]
			if endIdx := strings.Index(rest, " in the IDE"); endIdx != -1 {
				filePath := rest[:endIdx]
				baseName := filepath.Base(filePath)
				return "(opened " + baseName + ")"
			}
		}
		return "(IDE context)"
	}

	// Strip all remaining XML-style tags
	prompt = xmlTagRegex.ReplaceAllString(prompt, "")

	// Strip common noise prefixes
	prompt = noiseRegex.ReplaceAllString(prompt, "")

	// Collapse newlines and multiple spaces into single spaces
	prompt = strings.Join(strings.Fields(prompt), " ")

	return prompt
}
