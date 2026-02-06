# cctop — Claude Session Monitor

## Overview

`cctop` is a read-only terminal dashboard that displays all active Claude Code sessions on the local machine. It operates like `htop` — a live-updating table showing session state, project, and metadata at a glance.

## Core Concepts

### Session

A session is a running Claude Code process. Each session is associated with:

- A **process** (discovered via `ps`)
- A **working directory** (resolved via `lsof`)
- A **transcript file** (JSONL on disk in `~/.claude/projects/`)
- A **source** indicating how it was launched (CLI or IDE)

### Session State

Each session is in exactly one of three states:

| State     | Meaning                                        | Visual      |
|-----------|------------------------------------------------|-------------|
| `active`  | Claude is currently generating or processing   | Yellow `◉`  |
| `waiting` | Claude has responded, awaiting user input       | Green `●`   |
| `idle`    | Session exists but has been inactive            | Dim `○`     |

State is determined from the session's JSONL transcript file:

1. If the file was modified within the last **30 seconds** → `active`
2. If the last line has `"type": "progress"` → `active`
3. If the last line has `"message.role": "assistant"` → `waiting`
4. If the last line has `"message.role": "user"` and the file is less than **5 minutes** old → `active`
5. Otherwise → `idle`

### Session Source

Sessions originate from two sources:

| Source   | How detected                                                                  |
|----------|-------------------------------------------------------------------------------|
| CLI      | `ps` shows a `claude` process with a real TTY (e.g., `ttys001`)              |
| IDE      | Lock file in `~/.claude/ide/*.lock` references a live PID + workspace folder  |

IDE sources are further classified by `ideName` from the lock file (e.g., "VSCode", "Cursor").

When the same working directory appears in both CLI and IDE discovery, the IDE session takes priority and the CLI entry is deduplicated away.

## Data Model

### Filesystem Layout

```
~/.claude/
├── ide/
│   └── <pid>.lock                  # IDE lock files (JSON)
└── projects/
    └── <encoded-path>/             # Path with / and . replaced by -
        ├── sessions-index.json     # Optional session index
        └── <session-id>.jsonl      # Transcript (one JSON object per line)
```

### IDE Lock File (`~/.claude/ide/<pid>.lock`)

```json
{
  "pid": 5578,
  "workspaceFolders": ["/Users/me/projects/myapp"],
  "ideName": "Visual Studio Code - Insiders",
  "transport": "ws"
}
```

### JSONL Transcript Lines

Each line is a self-contained JSON object. Relevant fields:

| Field              | Type   | Description                                |
|--------------------|--------|--------------------------------------------|
| `type`             | string | Line type: `"user"`, `"assistant"`, `"progress"`, `"summary"`, etc. |
| `message.role`     | string | `"user"` or `"assistant"` (present when `type` has a message) |
| `message.content`  | string or array | The message text or structured content array |
| `slug`             | string | Human-readable session name (e.g., `"gleaming-mixing-graham"`) |
| `gitBranch`        | string | Git branch at time of message              |
| `cwd`              | string | Working directory at time of message       |
| `sessionId`        | string | UUID of the session                        |

### Sessions Index (`sessions-index.json`)

Optional. When present, provides a pre-built index:

```json
{
  "entries": [
    {
      "sessionId": "uuid",
      "fullPath": "/absolute/path/to/session.jsonl",
      "firstPrompt": "the user's first message",
      "messageCount": 42,
      "fileMtime": 1738800000,
      "created": "2025-02-06T...",
      "modified": "2025-02-06T...",
      "gitBranch": "main"
    }
  ]
}
```

## Display

### Per-Session Fields

Each row in the dashboard shows:

| Column   | Source                                              | Required |
|----------|-----------------------------------------------------|----------|
| ST       | State indicator icon (see Session State)             | Yes      |
| SRC      | Source type: `CLI`, `VSCode`, `Cursor`, etc.         | Yes      |
| PROJECT  | Last 2 path components of the working directory      | Yes      |
| TOPIC    | First user prompt, cleaned of system/IDE tags        | Yes      |
| BRANCH   | Git branch from the transcript                       | No (shown only if terminal is wide enough) |
| DUR      | Wall-clock duration since process started            | Yes      |

### Header Bar

Top line shows:
- Title: `cctop — Claude Session Monitor`
- Right-aligned: counts by state (e.g., `1 active  2 idle`) and `[q]uit` hint

### Sort Order

Rows are sorted by state priority: `active` first, then `waiting`, then `idle`.

### Layout Rules

- Minimum terminal width: 60 columns
- BRANCH column appears only when terminal width exceeds ~80 usable columns
- PROJECT and TOPIC share remaining width at roughly 35/65 split
- Strings exceeding their column width are truncated with `…`
- When more rows exist than fit the terminal, overflow shows `… N more sessions`

### Empty State

When no sessions are found:
```
  No active Claude sessions

  Start a session with claude in a terminal or VSCode
```

## Behavior

### Refresh Loop

1. Discover all running Claude processes (single `ps` call)
2. Resolve working directories (single batched `lsof` call)
3. Match processes to session transcripts on disk
4. Determine state for each session
5. Render the table
6. Wait for the refresh interval, accepting `q` to quit

**Refresh interval**: 2 seconds.

### Terminal Management

- Runs in the alternate screen buffer (`tput smcup`/`rmcup`)
- Cursor is hidden during operation
- Screen is redrawn via cursor-home + overwrite (no full clear, avoids flicker)
- Clean exit on `SIGINT`, `SIGTERM`, or `q` keypress — restores terminal state

### Topic Extraction

The "topic" is derived from the first user prompt in the session, with cleanup:

1. Strip `<ide_selection>` wrappers → extract user text after the closing tag
2. Strip `<ide_opened_file>` → display as `(opened filename.ext)`
3. Strip all remaining XML-style tags
4. Remove noise prefixes: `Caveat:...`, `Implement the following plan:`, `# `, `DO NOT...`, `IMPORTANT:...`, `The messages below...`, `No prompt`
5. Trim whitespace
6. If empty, fall back to the session's `slug` field, then to truncated session ID

### Session Matching

For a given working directory:

1. Encode the path (`/` and `.` become `-`)
2. Look for `~/.claude/projects/<encoded>/sessions-index.json`
   - If found, select the entry with the highest `fileMtime`
3. If no index, find the most recently modified `.jsonl` file in the directory
   - Parse first 30 lines for the first user message
   - Count lines for approximate message count
   - Read last line for `gitBranch` and `slug`

### Deduplication

If the same working directory appears from both IDE lock files and CLI `ps` output, the IDE session wins. CLI sessions matching an already-seen CWD are skipped.

## CLI Interface

```
cctop [OPTIONS]

Options:
  --once, -1    Print the table once and exit (no live refresh)
  --debug       Print timing diagnostics to stderr
  -h, --help    Show usage information
```

### Exit Codes

| Code | Meaning       |
|------|---------------|
| 0    | Normal exit   |

## Platform Dependencies

| Dependency | Purpose                              | Availability       |
|------------|--------------------------------------|---------------------|
| `ps`       | Enumerate running processes          | POSIX / macOS       |
| `lsof`     | Resolve process working directories  | macOS default       |
| `jq`       | Parse JSON transcript and lock files | Must be installed   |
| `stat -f`  | Get file modification time           | macOS (`-f '%m'`)   |
| `tput`     | Terminal capabilities (size, alt screen) | POSIX / macOS   |

### Platform Notes

- `stat -f '%m'` is macOS-specific. Linux uses `stat -c '%Y'`.
- `lsof` output format (`-Fn`) is consistent across macOS and Linux.
- `ps -eo pid,etime,tty,command` is POSIX-compatible.
- IDE lock file detection assumes `~/.claude/ide/` exists (created by Claude Code IDE extensions).

## Go Rewrite Architecture

The bash prototype is being replaced with a Go TUI app using Bubbletea (Elm architecture). This section documents patterns and decisions for the rewrite.

### Project Layout

Follow jeb-todo-md conventions:

```
cmd/cctop/main.go              # Thin entry point: parse flags, call tui.Run()
internal/
  tui/
    model.go                   # Bubbletea model, Update(), View()
    styles.go                  # Lipgloss style constants
  session/
    discover.go                # Process enumeration, CWD resolution, lock file parsing
    metadata.go                # JSONL parsing, state detection, topic extraction
    types.go                   # Session, State, Source types
tests/
  session_test.go              # Discovery logic unit tests
  metadata_test.go             # State detection & topic extraction tests
```

`internal/` enforces package privacy. `session` package has zero TUI dependency — it produces `[]Session` from the filesystem and process table. `tui` package consumes `[]Session` for rendering.

### TUI Modes

The rewrite expands from a read-only monitor to an interactive TUI with modes:

| Mode | Purpose | Transitions |
|------|---------|-------------|
| **ModeNormal** | Browse session list, view summary | Default mode |
| **ModeFilter** | Text input to filter sessions by project/topic | `/` from Normal, `esc` back |
| **ModeDetail** | View expanded session info (full topic, path, metadata) | `enter` from Normal, `esc` back |

### Keybindings

| Key | Mode | Action |
|-----|------|--------|
| j/k, up/down | Normal | Navigate session list |
| enter | Normal | Open session detail view |
| / | Normal | Open filter input |
| f | Normal | Cycle state filter: all → active → waiting → idle |
| s | Normal | Cycle sort: state → duration → project |
| q | Normal | Quit |
| esc | Filter/Detail | Return to Normal |
| ctrl+c | Any | Force quit |

### Bubbletea Patterns

Follow the same patterns as jeb-todo-md:

- **Mode-specific update handlers** — `updateNormal(msg)`, `updateFilter(msg)`, `updateDetail(msg)` keep Update() clean
- **Immediate data refresh** — each tick re-discovers sessions (no dirty flag needed, it's read-only)
- **Embedded bubbles components** — `textinput.Model` for filter, `table.Model` or custom list for session rows
- **Window size tracking** — `tea.WindowSizeMsg` stored on model, used for responsive column layout

### Session Discovery (Go equivalents)

| Current (bash + subprocesses) | Go replacement |
|-------------------------------|----------------|
| `ps -eo pid,etime,tty,command \| grep` | `exec.Command("ps", ...)` → parse stdout (still needed, no pure-Go alternative on macOS) |
| `lsof -a -p PIDs -d cwd -Fn` | `exec.Command("lsof", ...)` → parse `p`/`n` lines. On Linux, read `/proc/<pid>/cwd` symlink instead |
| `jq` (3-6 calls per cycle) | `encoding/json` — native, zero subprocess overhead |
| `stat -f '%m'` | `os.Stat(path).ModTime()` — native |
| `tail -1` on JSONL | `io.SeekEnd` + backward byte scan — native, no subprocess |
| `wc -l` for message count | `bufio.Scanner` line count or estimate from `file.Stat().Size()` |
| `head -30 \| jq -s` | `bufio.Scanner` first 30 lines + `json.Unmarshal` |

**Net effect:** The only external commands are `ps` and `lsof` (macOS). Everything else becomes native Go, eliminating ~95% of subprocess forks.

### Tick-Based Refresh

```go
type tickMsg time.Time

func tickCmd() tea.Cmd {
    return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tickMsg:
        m.sessions = session.Discover()
        return m, tickCmd()
    }
}
```

### Caching Strategy

The Go rewrite should cache session metadata the same way:

- Key: `cwd` + JSONL file `ModTime()`
- Cached fields: topic, message count, branch, full path
- Always recompute: state (depends on wall-clock time vs mtime)
- Use `sync.Map` or a plain `map[string]cachedSession` (single-goroutine access)

### Testing Strategy

Follow jeb-todo-md: test the data layer, not the TUI.

- **session package tests** — feed fixture JSONL files and mock `ps`/`lsof` output, assert `[]Session` contents
- **State detection tests** — fixture files with known mtimes, assert correct state enum
- **Topic extraction tests** — fixture first-prompt strings with IDE tags, assert cleaned output
- **Path encoding tests** — assert `encode("/Users/me/project")` == `-Users-me-project`

### Cross-Platform

| Concern | macOS | Linux |
|---------|-------|-------|
| Process CWD | `lsof -a -p PID -d cwd -Fn` | `os.Readlink("/proc/<pid>/cwd")` |
| File mtime | `os.Stat()` (same) | `os.Stat()` (same) |
| Terminal | Bubbletea handles both | Bubbletea handles both |
| `ps` output | Same flags work | Same flags work |

Build-tag or runtime detection in `discover.go` to pick the right CWD resolution strategy.

## Future Considerations

These are out of scope for the initial Go rewrite but noted for later:

- **Filesystem watching** — `fsnotify`/`kqueue` instead of polling for JSONL changes
- **Session interaction** — attach to a session, send input, view live output
- **Resource monitoring** — token usage, API cost, token throughput per session
- **Configuration file** — user-customizable refresh rate, colors, column visibility
- **Remote sessions** — monitor sessions on remote machines via SSH
