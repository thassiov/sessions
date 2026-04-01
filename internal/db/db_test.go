package db

import (
	"path/filepath"
	"testing"
)

func TestOpenAndClose(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer d.Close()

	// Verify tables exist.
	tables := []string{"sessions", "session_topics", "session_tools", "session_agents", "hook_state", "session_content"}
	for _, table := range tables {
		var name string
		err := d.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type IN ('table', 'table') AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestIsEmpty(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer d.Close()

	empty, err := d.IsEmpty()
	if err != nil {
		t.Fatalf("IsEmpty() error: %v", err)
	}
	if !empty {
		t.Error("IsEmpty() = false, want true for fresh db")
	}
}

func TestUpsertSession(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer d.Close()

	data := &SessionData{
		SessionID:       "test-session-001",
		Project:         "-home-user-myapp",
		ProjectName:     "myapp",
		Title:           "Test session",
		TitleDisplay:    "Test session",
		FilePath:        "/tmp/test.jsonl",
		FileSize:        1024,
		ExchangeCount:   10,
		StartTime:       "2026-03-01T10:00:00Z",
		EndTime:         "2026-03-01T11:00:00Z",
		DurationMinutes: 60,
		Model:           "claude-opus-4-6",
		FileHash:        "abc123",
		Tools:           map[string]int{"Bash": 5, "Read": 3},
		Agents:          map[string]int{"Explore": 2},
		FTSContent:      "testing search content here",
		Topics: []TopicEntry{
			{Topic: "initial setup", Source: "compaction_summary", CapturedAt: "2026-03-01T10:30:00Z"},
		},
	}

	if err := d.UpsertSession(data); err != nil {
		t.Fatalf("UpsertSession() error: %v", err)
	}

	// Verify session was inserted.
	empty, _ := d.IsEmpty()
	if empty {
		t.Error("IsEmpty() = true after UpsertSession")
	}

	// Verify tools.
	var toolCount int
	d.db.QueryRow("SELECT COUNT(*) FROM session_tools WHERE session_id=?", data.SessionID).Scan(&toolCount)
	if toolCount != 2 {
		t.Errorf("tool count = %d, want 2", toolCount)
	}

	// Verify agents.
	var agentCount int
	d.db.QueryRow("SELECT COUNT(*) FROM session_agents WHERE session_id=?", data.SessionID).Scan(&agentCount)
	if agentCount != 1 {
		t.Errorf("agent count = %d, want 1", agentCount)
	}

	// Verify FTS content.
	var ftsCount int
	d.db.QueryRow("SELECT COUNT(*) FROM session_content WHERE session_id=?", data.SessionID).Scan(&ftsCount)
	if ftsCount != 1 {
		t.Errorf("FTS content count = %d, want 1", ftsCount)
	}

	// Verify topics.
	var topicCount int
	d.db.QueryRow("SELECT COUNT(*) FROM session_topics WHERE session_id=?", data.SessionID).Scan(&topicCount)
	if topicCount != 1 {
		t.Errorf("topic count = %d, want 1", topicCount)
	}
}

func TestUpsertSessionIdempotent(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer d.Close()

	data := &SessionData{
		SessionID:   "test-session-002",
		Project:     "-home-user",
		ProjectName: "user",
		FilePath:    "/tmp/test.jsonl",
		FileHash:    "hash1",
		Tools:       map[string]int{"Bash": 1},
		Agents:      map[string]int{},
	}

	// Insert twice.
	if err := d.UpsertSession(data); err != nil {
		t.Fatalf("first UpsertSession() error: %v", err)
	}

	data.FileHash = "hash2"
	data.Tools = map[string]int{"Bash": 5, "Edit": 3}
	if err := d.UpsertSession(data); err != nil {
		t.Fatalf("second UpsertSession() error: %v", err)
	}

	// Should still have only one session.
	var count int
	d.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)
	if count != 1 {
		t.Errorf("session count = %d, want 1", count)
	}

	// Tools should be updated.
	var toolCount int
	d.db.QueryRow("SELECT COUNT(*) FROM session_tools WHERE session_id=?", data.SessionID).Scan(&toolCount)
	if toolCount != 2 {
		t.Errorf("tool count = %d, want 2 after upsert", toolCount)
	}

	// Hash should be updated.
	var hash string
	d.db.QueryRow("SELECT file_hash FROM sessions WHERE session_id=?", data.SessionID).Scan(&hash)
	if hash != "hash2" {
		t.Errorf("file_hash = %q, want hash2", hash)
	}
}
