# cctop

A live-updating TUI dashboard for monitoring Claude Code sessions across CLI and IDE.

## Tech Stack

- Go 1.24+
- [Bubbletea](https://github.com/charmbracelet/bubbletea) v1 — TUI framework (Elm architecture)
- [Bubbles](https://github.com/charmbracelet/bubbles) — table, viewport, textinput components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) v1 — terminal styling

## Project Structure

```
cmd/cctop/main.go             # Thin entry point
internal/
  tui/model.go                # Bubbletea model, update, view
  tui/styles.go               # Lipgloss style constants
  session/discover.go          # Process discovery (ps, lsof, lock files)
  session/metadata.go          # JSONL parsing, state detection, topic extraction
  session/types.go             # Session, State, Source types
tests/
  session_test.go             # Discovery & metadata unit tests
  metadata_test.go            # JSONL parsing, state detection tests
```

## Architecture

### Data Sources

Sessions are discovered from two independent sources, then merged:

1. **Process table** (`ps`) — running `claude` processes, distinguished by TTY (`??` = IDE, real TTY = CLI)
2. **IDE lock files** (`~/.claude/ide/*.lock`) — JSON files linking IDE PIDs to workspace folders

Process CWDs are resolved via `lsof`, then matched to transcript files on disk at `~/.claude/projects/<encoded-path>/`.

### Session State Machine

State is derived from the JSONL transcript file's mtime and last line:

```
file modified < 30s ago ─────────────────────────── → active
last line type == "progress" ────────────────────── → active
last line message.role == "assistant" ───────────── → waiting
last line message.role == "user" && mtime < 5min ── → active
otherwise ──────────────────────────────────────── → idle
```

### Refresh Cycle

Each cycle (every 2 seconds):

1. Single `ps` call → cache output
2. Single batched `lsof` call → resolve all CWDs at once
3. Discover CLI sessions (from cached ps, filter by TTY)
4. Discover IDE sessions (from lock files + cached ps)
5. Deduplicate by CWD (IDE takes priority over CLI)
6. Enrich each session: match to JSONL, detect state, extract topic
7. Render table sorted by state priority (active → waiting → idle)

### Caching

Session metadata (topic, message count, branch) is cached by `cwd:mtime`. State is always recomputed since it depends on wall-clock time.

### Topic Extraction

The first user prompt is cleaned through a pipeline:

1. Strip IDE wrapper tags (`<ide_selection>`, `<ide_opened_file>`)
2. Strip remaining XML tags
3. Remove system noise prefixes (`Caveat:`, `Implement the following plan:`, etc.)
4. Fall back to session `slug`, then truncated session ID

Full rules documented in `SPEC.md` under "Topic Extraction".

## Data Model Reference

Filesystem layout and JSON schemas are documented in `SPEC.md` under "Data Model". Key paths:

- `~/.claude/ide/<pid>.lock` — IDE lock files
- `~/.claude/projects/<encoded-path>/sessions-index.json` — session index
- `~/.claude/projects/<encoded-path>/<uuid>.jsonl` — session transcripts

Path encoding: `/` and `.` are replaced with `-`.

## Commands

```bash
make build          # go build -o cctop-go ./cmd/cctop
make test           # go test ./...
make run            # Build and launch
```

## Keybindings

| Key | Mode | Action |
|-----|------|--------|
| j/k, arrows | Normal | Navigate sessions |
| enter | Normal | View session detail |
| / | Normal | Filter sessions |
| f | Normal | Cycle state filter (all → active → waiting → idle) |
| s | Normal | Cycle sort order |
| q | Normal | Quit |
| esc | Filter/Detail | Back to session list |
| ctrl+c | Any | Force quit |
