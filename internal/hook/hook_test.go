package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thassiov/sessions/internal/db"
)

func TestExtractTopic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []string
		want     string
	}{
		{
			name:     "simple message",
			messages: []string{"fix the authentication bug in login"},
			want:     "Fix the authentication bug in login",
		},
		{
			name:     "extracts first sentence",
			messages: []string{"Fix the auth bug. Then refactor the middleware. Also update tests."},
			want:     "Fix the auth bug",
		},
		{
			name:     "truncates long messages",
			messages: []string{"This is an extremely long message that goes on and on and on and exceeds the sixty character limit for topics"},
			want:     "This is an extremely long message that goes on and on and...",
		},
		{
			name:     "strips markdown",
			messages: []string{"**Fix** the `auth` module in [this file](http://example.com)"},
			want:     "Fix the module in this file",
		},
		{
			name:     "strips system reminders",
			messages: []string{"<system-reminder>ignore this</system-reminder>actual topic here for the session"},
			want:     "Actual topic here for the session",
		},
		{
			name:     "falls back to previous message",
			messages: []string{"a good topic message here", "ok"},
			want:     "A good topic message here",
		},
		{
			name:     "empty messages",
			messages: []string{},
			want:     "",
		},
		{
			name:     "capitalizes first letter",
			messages: []string{"fix the bug in the login flow"},
			want:     "Fix the bug in the login flow",
		},
		{
			name:     "strips code blocks",
			messages: []string{"look at this ```go\nfunc main() {}\n``` and fix the thing"},
			want:     "Look at this and fix the thing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractTopic(tt.messages)
			if got != tt.want {
				t.Errorf("ExtractTopic() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadStdin(t *testing.T) {
	t.Parallel()

	input := `{"session_id":"abc-123-def"}`
	r := strings.NewReader(input)
	data := ReadStdin(r)

	if data.SessionID != "abc-123-def" {
		t.Errorf("SessionID = %q, want abc-123-def", data.SessionID)
	}
}

func TestReadStdinEmpty(t *testing.T) {
	t.Parallel()

	r := strings.NewReader("")
	data := ReadStdin(r)

	if data.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", data.SessionID)
	}
}

func TestCountUserMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	lines := []string{
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"content":"hi"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:02Z","message":{"content":"question"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:03Z","message":{"content":"answer"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:04Z","message":{"content":"thanks"}}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	count := CountUserMessages(path)
	if count != 3 {
		t.Errorf("CountUserMessages() = %d, want 3", count)
	}
}

func TestExtractRecentUserMessages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	lines := []string{
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"this is the first user message here"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"content":"reply one"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:02Z","message":{"content":"this is the second user message"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:03Z","message":{"content":"reply two"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:04Z","message":{"content":"this is the third user message"}}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	messages := ExtractRecentUserMessages(path, 2)
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
	// Should be last 2 in chronological order.
	if !strings.Contains(messages[0], "second") {
		t.Errorf("messages[0] = %q, want containing 'second'", messages[0])
	}
	if !strings.Contains(messages[1], "third") {
		t.Errorf("messages[1] = %q, want containing 'third'", messages[1])
	}
}

func TestExtractRecentSkipsNoise(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	lines := []string{
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"this is a real user message here"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:01Z","message":{"content":"yes"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:02Z","message":{"content":"ok"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:03Z","message":{"content":"thanks"}}`,
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	messages := ExtractRecentUserMessages(path, 3)
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1 (noise should be filtered)", len(messages))
	}
	if !strings.Contains(messages[0], "real user message") {
		t.Errorf("message = %q, want containing 'real user message'", messages[0])
	}
}

func TestHookStateDB(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer database.Close()

	// Initially no state.
	last := GetLastCapture(database, "test-session")
	if last != 0 {
		t.Errorf("initial lastCapture = %d, want 0", last)
	}

	// Update state.
	UpdateLastCapture(database, "test-session", 20, nil)
	last = GetLastCapture(database, "test-session")
	if last != 20 {
		t.Errorf("after update lastCapture = %d, want 20", last)
	}

	// Upsert.
	UpdateLastCapture(database, "test-session", 30, nil)
	last = GetLastCapture(database, "test-session")
	if last != 30 {
		t.Errorf("after upsert lastCapture = %d, want 30", last)
	}
}

func TestWriteTopic(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer database.Close()

	// Need a session first for the FK.
	database.UpsertSession(&db.SessionData{
		SessionID: "test-session-001",
		Project:   "test",
		FilePath:  "/tmp/test.jsonl",
		FileHash:  "abc",
		Tools:     map[string]int{},
		Agents:    map[string]int{},
	})

	WriteTopic(database, "test-session-001", "Fix auth bug", "hook_periodic", 15, nil)

	var count int
	database.SQL().QueryRow("SELECT COUNT(*) FROM session_topics WHERE session_id=?", "test-session-001").Scan(&count)
	if count != 1 {
		t.Errorf("topic count = %d, want 1", count)
	}

	var topic, source string
	database.SQL().QueryRow(
		"SELECT topic, source FROM session_topics WHERE session_id=?", "test-session-001",
	).Scan(&topic, &source)
	if topic != "Fix auth bug" {
		t.Errorf("topic = %q, want 'Fix auth bug'", topic)
	}
	if source != "hook_periodic" {
		t.Errorf("source = %q, want 'hook_periodic'", source)
	}
}

// Verify StdinData JSON unmarshaling.
func TestStdinDataJSON(t *testing.T) {
	t.Parallel()

	input := `{"session_id":"abc-123","cwd":"/home/user","prompt":"hello"}`
	var data StdinData
	json.Unmarshal([]byte(input), &data)

	if data.SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want abc-123", data.SessionID)
	}
}
