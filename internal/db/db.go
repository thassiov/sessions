// Package db manages the SQLite database for session indexing.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the session index database.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the session index database.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}
	if _, err := conn.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("setting synchronous: %w", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if err := migrate(conn); err != nil {
		conn.Close() //nolint:errcheck
		return nil, fmt.Errorf("migrating db: %w", err)
	}

	return &DB{db: conn}, nil
}

// Close closes the database.
func (d *DB) Close() error {
	return d.db.Close()
}

// SQL returns the underlying sql.DB for direct queries.
func (d *DB) SQL() *sql.DB {
	return d.db
}

// IsEmpty returns true if no sessions have been indexed.
func (d *DB) IsEmpty() (bool, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)
	if err != nil {
		return true, fmt.Errorf("checking session count: %w", err)
	}
	return count == 0, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			session_id       TEXT PRIMARY KEY,
			project          TEXT NOT NULL DEFAULT '',
			project_name     TEXT NOT NULL DEFAULT '',
			title            TEXT,
			title_display    TEXT,
			tags             TEXT,
			client           TEXT,
			file_path        TEXT NOT NULL,
			file_size        INTEGER DEFAULT 0,
			exchange_count   INTEGER DEFAULT 0,
			start_time       TEXT,
			end_time         TEXT,
			duration_minutes INTEGER,
			model            TEXT,
			has_compaction   INTEGER DEFAULT 0,
			indexed_at       TEXT NOT NULL,
			last_modified    TEXT,
			file_hash        TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS session_topics (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT NOT NULL,
			topic           TEXT NOT NULL,
			captured_at     TEXT NOT NULL,
			exchange_number INTEGER,
			source          TEXT NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS session_tools (
			session_id TEXT NOT NULL,
			tool_name  TEXT NOT NULL,
			use_count  INTEGER DEFAULT 0,
			PRIMARY KEY (session_id, tool_name),
			FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS session_agents (
			session_id       TEXT NOT NULL,
			agent_name       TEXT NOT NULL,
			invocation_count INTEGER DEFAULT 0,
			PRIMARY KEY (session_id, agent_name),
			FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS hook_state (
			session_id            TEXT PRIMARY KEY,
			last_capture_exchange INTEGER NOT NULL DEFAULT 0,
			updated_at            TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
		CREATE INDEX IF NOT EXISTS idx_sessions_client ON sessions(client);
		CREATE INDEX IF NOT EXISTS idx_sessions_start ON sessions(start_time);
		CREATE INDEX IF NOT EXISTS idx_topics_session ON session_topics(session_id);
		CREATE INDEX IF NOT EXISTS idx_topics_source ON session_topics(source);
	`)
	if err != nil {
		return fmt.Errorf("creating tables: %w", err)
	}

	// FTS5 table — check if exists first (virtual tables don't support IF NOT EXISTS).
	var name sql.NullString
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='session_content'",
	).Scan(&name)
	if err == sql.ErrNoRows || !name.Valid {
		_, err = db.Exec(`
			CREATE VIRTUAL TABLE session_content USING fts5(
				session_id,
				content,
				tokenize='porter unicode61'
			)
		`)
		if err != nil {
			return fmt.Errorf("creating FTS5 table: %w", err)
		}
	}

	return nil
}

// SessionData holds parsed session data ready for upserting.
type SessionData struct {
	SessionID      string
	Project        string
	ProjectName    string
	Title          string
	TitleDisplay   string
	Tags           string
	Client         string
	FilePath       string
	FileSize       int64
	ExchangeCount  int
	StartTime      string
	EndTime        string
	DurationMinutes int
	Model          string
	HasCompaction  int
	FileHash       string
	Tools          map[string]int
	Agents         map[string]int
	FTSContent     string
	Topics         []TopicEntry
}

// TopicEntry represents a topic captured during a session.
type TopicEntry struct {
	Topic          string
	Source         string
	CapturedAt     string
	ExchangeNumber *int
}

// UpsertSession inserts or updates a session and its relations.
func (d *DB) UpsertSession(data *SessionData) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Format(time.RFC3339)

	_, err = tx.Exec(`
		INSERT INTO sessions (
			session_id, project, project_name, title, title_display, tags,
			client, file_path, file_size, exchange_count, start_time, end_time,
			duration_minutes, model, has_compaction, indexed_at, last_modified, file_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			title=excluded.title, title_display=excluded.title_display,
			tags=excluded.tags, client=excluded.client,
			file_size=excluded.file_size, exchange_count=excluded.exchange_count,
			end_time=excluded.end_time, duration_minutes=excluded.duration_minutes,
			model=excluded.model, has_compaction=excluded.has_compaction,
			indexed_at=excluded.indexed_at, last_modified=excluded.last_modified,
			file_hash=excluded.file_hash
	`, data.SessionID, data.Project, data.ProjectName,
		data.Title, data.TitleDisplay, data.Tags,
		data.Client, data.FilePath, data.FileSize,
		data.ExchangeCount, data.StartTime, data.EndTime,
		data.DurationMinutes, data.Model, data.HasCompaction,
		now, now, data.FileHash,
	)
	if err != nil {
		return fmt.Errorf("upserting session: %w", err)
	}

	// Replace tools.
	if _, err := tx.Exec("DELETE FROM session_tools WHERE session_id=?", data.SessionID); err != nil {
		return fmt.Errorf("clearing tools: %w", err)
	}
	for tool, count := range data.Tools {
		if _, err := tx.Exec(
			"INSERT INTO session_tools (session_id, tool_name, use_count) VALUES (?, ?, ?)",
			data.SessionID, tool, count,
		); err != nil {
			return fmt.Errorf("inserting tool %s: %w", tool, err)
		}
	}

	// Replace agents.
	if _, err := tx.Exec("DELETE FROM session_agents WHERE session_id=?", data.SessionID); err != nil {
		return fmt.Errorf("clearing agents: %w", err)
	}
	for agent, count := range data.Agents {
		if _, err := tx.Exec(
			"INSERT INTO session_agents (session_id, agent_name, invocation_count) VALUES (?, ?, ?)",
			data.SessionID, agent, count,
		); err != nil {
			return fmt.Errorf("inserting agent %s: %w", agent, err)
		}
	}

	// Replace FTS content.
	if _, err := tx.Exec("DELETE FROM session_content WHERE session_id=?", data.SessionID); err != nil {
		return fmt.Errorf("clearing FTS content: %w", err)
	}
	if data.FTSContent != "" {
		if _, err := tx.Exec(
			"INSERT INTO session_content (session_id, content) VALUES (?, ?)",
			data.SessionID, data.FTSContent,
		); err != nil {
			return fmt.Errorf("inserting FTS content: %w", err)
		}
	}

	// Add topics (don't delete hook-captured topics, only add compaction summaries).
	for _, topic := range data.Topics {
		var exists int
		err := tx.QueryRow(
			"SELECT COUNT(*) FROM session_topics WHERE session_id=? AND topic=? AND source=?",
			data.SessionID, topic.Topic, topic.Source,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking topic existence: %w", err)
		}
		if exists == 0 {
			if _, err := tx.Exec(
				"INSERT INTO session_topics (session_id, topic, captured_at, exchange_number, source) VALUES (?, ?, ?, ?, ?)",
				data.SessionID, topic.Topic, topic.CapturedAt, topic.ExchangeNumber, topic.Source,
			); err != nil {
				return fmt.Errorf("inserting topic: %w", err)
			}
		}
	}

	return tx.Commit()
}
