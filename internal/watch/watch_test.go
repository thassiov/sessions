package watch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thassiov/sessions/internal/config"
	"github.com/thassiov/sessions/internal/db"
)

func setupWatchTest(t *testing.T) (*db.DB, *config.Config, string) {
	t.Helper()

	dir := t.TempDir()
	projectsDir := filepath.Join(dir, "projects")
	projectDir := filepath.Join(projectsDir, "-test-project")
	os.MkdirAll(projectDir, 0o755)

	dbPath := filepath.Join(dir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	cfg := &config.Config{
		ProjectsDir:  projectsDir,
		DBPath:       dbPath,
		Clients:      []string{},
		ProjectNames: map[string]string{},
	}

	return database, cfg, projectDir
}

func TestWatcherIndexesNewFile(t *testing.T) {
	t.Parallel()

	database, cfg, projectDir := setupWatchTest(t)
	defer database.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	w := New(database, cfg, logger)
	w.debounce = 200 * time.Millisecond // Fast debounce for tests.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watcher in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	// Give watcher time to start.
	time.Sleep(100 * time.Millisecond)

	// Write a session file.
	sessionPath := filepath.Join(projectDir, "test-session-watch-001.jsonl")
	lines := `{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"this is a test message for the watcher"}}
{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"model":"claude-opus-4-6","content":[{"type":"text","text":"got it"}]}}
`
	os.WriteFile(sessionPath, []byte(lines), 0o644)

	// Wait for debounce + processing.
	time.Sleep(500 * time.Millisecond)

	// Check that session was indexed.
	var count int
	database.SQL().QueryRow("SELECT COUNT(*) FROM sessions WHERE session_id='test-session-watch-001'").Scan(&count)
	if count != 1 {
		t.Errorf("session count = %d, want 1 (watcher should have indexed it)", count)
	}

	cancel()
	<-errCh
}

func TestWatcherDetectsFileUpdate(t *testing.T) {
	t.Parallel()

	database, cfg, projectDir := setupWatchTest(t)
	defer database.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	w := New(database, cfg, logger)
	w.debounce = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessionPath := filepath.Join(projectDir, "test-session-watch-002.jsonl")

	// Start watcher first.
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Write initial session file after watcher is running.
	lines := `{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"first message in the session"}}
{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"model":"claude-opus-4-6","content":[{"type":"text","text":"ok"}]}}
`
	os.WriteFile(sessionPath, []byte(lines), 0o644)

	// Wait for debounce + index.
	time.Sleep(500 * time.Millisecond)

	// Verify initial state.
	var exchanges int
	database.SQL().QueryRow("SELECT exchange_count FROM sessions WHERE session_id='test-session-watch-002'").Scan(&exchanges)
	if exchanges != 2 {
		t.Errorf("initial exchanges = %d, want 2", exchanges)
	}

	// Append more messages.
	f, _ := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"user","timestamp":"2026-03-01T10:02:00Z","message":{"content":"another message appended"}}
{"type":"assistant","timestamp":"2026-03-01T10:02:01Z","message":{"model":"claude-opus-4-6","content":[{"type":"text","text":"noted"}]}}
`)
	f.Close()

	// Wait for debounce + reindex.
	time.Sleep(500 * time.Millisecond)

	database.SQL().QueryRow("SELECT exchange_count FROM sessions WHERE session_id='test-session-watch-002'").Scan(&exchanges)
	if exchanges != 4 {
		t.Errorf("updated exchanges = %d, want 4", exchanges)
	}

	cancel()
	<-errCh
}

func TestWatcherSkipsSubagents(t *testing.T) {
	t.Parallel()

	database, cfg, projectDir := setupWatchTest(t)
	defer database.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	w := New(database, cfg, logger)
	w.debounce = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Write a subagent file (should be skipped).
	subagentDir := filepath.Join(projectDir, "some-session-id", "subagents")
	os.MkdirAll(subagentDir, 0o755)
	subagentPath := filepath.Join(subagentDir, "agent-abc123.jsonl")
	os.WriteFile(subagentPath, []byte(`{"type":"user","message":{"content":"subagent msg"}}`+"\n"), 0o644)

	time.Sleep(500 * time.Millisecond)

	// Should not be indexed.
	var count int
	database.SQL().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)
	if count != 0 {
		t.Errorf("session count = %d, want 0 (subagent files should be skipped)", count)
	}

	cancel()
	<-errCh
}

func TestWatcherDebounce(t *testing.T) {
	t.Parallel()

	database, cfg, projectDir := setupWatchTest(t)
	defer database.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	w := New(database, cfg, logger)
	w.debounce = 300 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- w.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	sessionPath := filepath.Join(projectDir, "test-session-watch-003.jsonl")

	// Write rapidly — should only index once after debounce settles.
	for i := 0; i < 5; i++ {
		f, _ := os.OpenFile(sessionPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		f.WriteString(`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"rapid write message number whatever"}}` + "\n")
		f.Close()
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for debounce.
	time.Sleep(500 * time.Millisecond)

	var count int
	database.SQL().QueryRow("SELECT COUNT(*) FROM sessions WHERE session_id='test-session-watch-003'").Scan(&count)
	if count != 1 {
		t.Errorf("session count = %d, want 1 (debounce should coalesce writes)", count)
	}

	cancel()
	<-errCh
}
