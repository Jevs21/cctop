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

Discovery runs in a background goroutine on a 2-second tick:

1. Discover all running Claude processes (single `ps` call)
2. Resolve working directories (single batched `lsof` call)
3. Match processes to session transcripts on disk
4. Determine state for each session
5. Deliver results to the TUI for rendering

**Refresh interval**: 2 seconds.

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

### Platform Notes

- On Linux, process CWDs are resolved via `/proc/<pid>/cwd` instead of `lsof`.
- `lsof` output format (`-Fn`) is consistent across macOS and Linux.
- `ps -eo pid,etime,tty,command` is POSIX-compatible.
- IDE lock file detection assumes `~/.claude/ide/` exists (created by Claude Code IDE extensions).
- All JSON parsing, file mtime checks, and JSONL reading are handled natively in Go (no `jq`, `stat`, or `tput` needed).

## Implementation Notes

### External Commands vs Native Go

The only external commands used are `ps` (process enumeration) and `lsof` (CWD resolution on macOS). Everything else — JSON parsing, file stat, last-line reading, line counting — is handled with Go stdlib (`encoding/json`, `os.Stat`, `io.SeekEnd` + backward scan, `bufio.Scanner`).

### TUI Modes

| Mode | Purpose | Transitions |
|------|---------|-------------|
| **Normal** | Browse session list, view summary | Default mode |
| **Filter** | Text input to filter sessions by project/topic | `/` from Normal, `esc`/`enter` back |
| **Detail** | View expanded session info (full topic, path, metadata) | `enter` from Normal, `esc` back |

### State Filters and Sort

- **State filter** cycles with `f`: all → active → waiting → idle
- **Sort order** cycles with `s`: state (default) → duration → project

### Keybindings

| Key | Mode | Action |
|-----|------|--------|
| j/k, up/down | Normal | Navigate session list |
| enter | Normal | Open session detail view |
| / | Normal | Open filter input |
| f | Normal | Cycle state filter |
| s | Normal | Cycle sort order |
| q | Normal | Quit |
| esc | Filter/Detail | Return to Normal |
| ctrl+c | Any | Force quit |

### Refresh

Session discovery runs in a background goroutine triggered by a 2-second tick. Each tick fires `refreshSessionsCmd()` which calls `session.DiscoverAll()` and delivers the result as a `sessionsRefreshedMsg`.

## Future Considerations

- **Filesystem watching** — `fsnotify`/`kqueue` instead of polling for JSONL changes
- **Session interaction** — attach to a session, send input, view live output
- **Resource monitoring** — token usage, API cost, token throughput per session
- **Configuration file** — user-customizable refresh rate, colors, column visibility
- **Remote sessions** — monitor sessions on remote machines via SSH
