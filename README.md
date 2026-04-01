# sessions

[![CI](https://github.com/thassiov/sessions/actions/workflows/ci.yml/badge.svg)](https://github.com/thassiov/sessions/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/thassiov/sessions)](https://goreportcard.com/report/github.com/thassiov/sessions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Search and index [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions.

A Go CLI that indexes Claude Code session JSONL files into SQLite with FTS5 for full-text search, analytics, and live topic tracking. Includes a file watcher daemon that automatically indexes sessions as they happen — no hook configuration needed.

## How It Works

```
~/.claude/projects/
  ├── -home-user-app1/
  │     ├── session-aaa.jsonl    ← file changes
  │     └── session-bbb.jsonl
  └── -home-user-app2/
        └── session-ccc.jsonl    ← new file appears
                │
                │ fsnotify (CREATE/WRITE)
                │
                ▼
┌──────────────────────────────────────────────────┐
│                sessions watch                     │
│                                                   │
│   1. DETECT     fsnotify event on .jsonl file     │
│         │       debounce 2s (coalesce rapid writes)│
│         ▼                                         │
│   2. PARSE      Read JSONL line by line            │
│         │       Extract: user prompts, tools,      │
│         │       agents, timestamps, model,         │
│         │       compaction summaries, titles        │
│         ▼                                         │
│   3. INDEX      Upsert to SQLite                   │
│         │       sessions, tools, agents, FTS5       │
│         ▼                                         │
│   4. TOPIC      Extract topic from recent messages  │
│                 Noise filter → first sentence       │
│                 → 60 char truncate → session_topics  │
└──────────────────────────────────────────────────┘
                │
                ▼
        ~/.session-index/sessions.db
        (SQLite + FTS5)
                │
                ▼
        sessions search "auth bug"
        sessions recent
        sessions analytics
        sessions context <id>
```

## Features

- **Full-text search** — FTS5 with porter stemmer across all session content
- **Structured filtering** — by client, project, tool, agent, tag, date range
- **Analytics dashboard** — time per client, daily trends, tool usage, week-over-week comparisons
- **Context retrieval** — extract conversation exchanges from any session with tool call summarization
- **Live topic capture** — automatic topic extraction during active sessions
- **File watcher daemon** — fsnotify-based, indexes sessions as they happen
- **Incremental indexing** — hash-based change detection, only processes modified files
- **Auto-index on first use** — backfills all existing sessions automatically
- **Systemd service** — runs as a user service with auto-restart

## Install

```bash
git clone https://github.com/thassiov/sessions.git
cd sessions

# Build
make build

# Install binary to ~/.local/bin
make install-local

# Install + create systemd watcher service + start
make install-service
```

## Quick Start

```bash
# Index all existing sessions (happens automatically on first use)
sessions index --backfill

# Search
sessions "auth bug"

# Recent sessions
sessions recent

# Start the watcher daemon (or use make install-service for systemd)
sessions watch
```

## Usage

### Search

```bash
# Full-text search (bare args route to search)
sessions "webhook debugging"
sessions search "database migration" -n 10

# Output:
#   * 2b817b0b  Fix auth middleware
#     2026-03-30 | myapp | 183 exchanges | 269min
#     "...fixing the authentication bug in the login flow with JWT tokens..."
#     -> claude --resume 2b817b0b-82dd-4e8b-9fae-6b84fb0641ed
```

### Browse

```bash
# Recent sessions
sessions recent 5

# Filter by criteria
sessions find --project myapp --week
sessions find --tool Bash --days 14
sessions find --compacted
```

### Context

```bash
# View conversation exchanges from a session
sessions context 2b817b0b

# Filter to matching exchanges
sessions context 2b817b0b "database"

# Partial session IDs work everywhere
sessions topics 2b81
```

### Analytics

```bash
# Overall stats
sessions stats

# Analytics dashboard
sessions analytics
sessions analytics --week
sessions analytics --month --project myapp
```

### Tools

```bash
# Top tools across all sessions
sessions tools

# Sessions using a specific tool
sessions tools Bash
```

### Indexing

```bash
# Incremental (only new/modified files)
sessions index

# Full reindex
sessions index --backfill

# Single session
sessions index --session <session-id>
```

### Watcher Daemon

```bash
# Run in foreground
sessions watch

# Or install as systemd service
make install-service

# Check status
systemctl --user status sessions-watcher.service

# View logs
journalctl --user -u sessions-watcher.service -f
```

### Hook Integration (Alternative)

If you prefer hook-based capture instead of the watcher daemon, add to your Claude Code `settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "sessions hook UserPromptSubmit" }] }],
    "PreCompact": [{ "hooks": [{ "type": "command", "command": "sessions hook PreCompact" }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "sessions hook SessionEnd" }] }]
  }
}
```

## Configuration

Optional. Create `~/.session-index/config.json`:

```json
{
  "projects_dir": "~/.claude/projects",
  "db_path": "~/.session-index/sessions.db",
  "clients": ["acme", "initech"],
  "project_names": {
    "-home-user-projects-myapp": "MyApp"
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `projects_dir` | `~/.claude/projects` | Where Claude Code stores session files |
| `db_path` | `~/.session-index/sessions.db` | SQLite database path |
| `clients` | `[]` | Client names for auto-detection in session content |
| `project_names` | `{}` | Directory name → friendly name mapping (auto-generated if empty) |

Environment variable overrides: `SESSION_INDEX_PROJECTS`, `SESSION_INDEX_DB`.

## Database Schema

All data lives in a single SQLite database:

```
sessions.db
├── sessions          # Core metadata (18 columns: id, project, title, timestamps, model, ...)
├── session_content   # FTS5 full-text index (porter stemmer + unicode)
├── session_topics    # Topic timeline per session (source: watcher, hook, compaction_summary)
├── session_tools     # Tool usage counts per session
├── session_agents    # Agent/subagent invocation counts
└── hook_state        # Topic capture interval tracking
```

## Development

```bash
# Install dev tools (golangci-lint, gosec, goimports)
make tools

# Build
make build

# Run tests
make test

# Full quality check (fmt, tidy, vet, test, build)
make check

# Full CI pipeline (includes lint + coverage)
make ci

# Rebuild on changes (requires entr)
make watch

# All available targets
make help
```

## Systemd Service

```bash
# Install binary + create + enable + start service
make install-service

# Check status
systemctl --user status sessions-watcher.service

# View logs
journalctl --user -u sessions-watcher.service -f

# Stop
systemctl --user stop sessions-watcher.service

# Remove service completely
make uninstall-service
```

## License

MIT
