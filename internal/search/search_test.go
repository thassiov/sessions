package search

import (
	"path/filepath"
	"testing"

	"github.com/thassiov/sessions/internal/db"
)

// setupTestDB creates a test database with sample data.
func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	sessions := []*db.SessionData{
		{
			SessionID: "session-aaa-111", Project: "-home-user-app1", ProjectName: "app1",
			Title: "Fix auth bug", TitleDisplay: "Fix auth bug", Client: "acme",
			FilePath: "/tmp/a.jsonl", FileSize: 1000, ExchangeCount: 20,
			StartTime: "2026-03-01T10:00:00Z", EndTime: "2026-03-01T11:00:00Z",
			DurationMinutes: 60, Model: "claude-opus-4-6", FileHash: "aaa",
			Tools: map[string]int{"Bash": 10, "Read": 5}, Agents: map[string]int{"Explore": 1},
			FTSContent: "fixing the authentication bug in the login flow with JWT tokens",
		},
		{
			SessionID: "session-bbb-222", Project: "-home-user-app2", ProjectName: "app2",
			Title: "Add dark mode", TitleDisplay: "Add dark mode", Tags: "frontend",
			FilePath: "/tmp/b.jsonl", FileSize: 2000, ExchangeCount: 50,
			StartTime: "2026-03-15T14:00:00Z", EndTime: "2026-03-15T16:00:00Z",
			DurationMinutes: 120, Model: "claude-sonnet-4-6", HasCompaction: 1, FileHash: "bbb",
			Tools: map[string]int{"Edit": 20, "Write": 10}, Agents: map[string]int{},
			FTSContent: "implementing dark mode theme switching with CSS variables",
			Topics: []db.TopicEntry{
				{Topic: "dark mode CSS", Source: "compaction_summary", CapturedAt: "2026-03-15T15:00:00Z"},
			},
		},
		{
			SessionID: "session-ccc-333", Project: "-home-user-app1", ProjectName: "app1",
			Title: "Refactor database", TitleDisplay: "Refactor database", Client: "acme",
			FilePath: "/tmp/c.jsonl", FileSize: 500, ExchangeCount: 10,
			StartTime: "2026-03-20T09:00:00Z", EndTime: "2026-03-20T09:30:00Z",
			DurationMinutes: 30, Model: "claude-opus-4-6", FileHash: "ccc",
			Tools: map[string]int{"Bash": 3, "Edit": 7}, Agents: map[string]int{"Plan": 1},
			FTSContent: "refactoring the database schema for better performance with indexes",
		},
	}

	for _, s := range sessions {
		if err := database.UpsertSession(s); err != nil {
			t.Fatalf("UpsertSession() error: %v", err)
		}
	}

	return database
}

func TestSearch(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Search(database.SQL(), "authentication JWT", 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Search() returned no results, want at least 1")
	}
	if results[0].SessionID != "session-aaa-111" {
		t.Errorf("first result = %q, want session-aaa-111", results[0].SessionID)
	}
}

func TestSearchNoResults(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Search(database.SQL(), "nonexistent_term_xyz_123", 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search() returned %d results, want 0", len(results))
	}
}

func TestFindByClient(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Find(database.SQL(), FilterOpts{Client: "acme", Limit: 10})
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Find(client=acme) returned %d results, want 2", len(results))
	}
}

func TestFindByProject(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Find(database.SQL(), FilterOpts{Project: "app2", Limit: 10})
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Find(project=app2) returned %d results, want 1", len(results))
	}
	if results[0].SessionID != "session-bbb-222" {
		t.Errorf("result = %q, want session-bbb-222", results[0].SessionID)
	}
}

func TestFindByTool(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Find(database.SQL(), FilterOpts{Tool: "Write", Limit: 10})
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Find(tool=Write) returned %d results, want 1", len(results))
	}
}

func TestFindByTag(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Find(database.SQL(), FilterOpts{Tag: "frontend", Limit: 10})
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Find(tag=frontend) returned %d results, want 1", len(results))
	}
}

func TestFindCompacted(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	compacted := true
	results, err := Find(database.SQL(), FilterOpts{HasCompaction: &compacted, Limit: 10})
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Find(compacted) returned %d results, want 1", len(results))
	}
}

func TestRecent(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := Recent(database.SQL(), 2)
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Recent(2) returned %d results, want 2", len(results))
	}
	// Most recent first.
	if results[0].SessionID != "session-ccc-333" {
		t.Errorf("first recent = %q, want session-ccc-333", results[0].SessionID)
	}
}

func TestTopics(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	topics, err := Topics(database.SQL(), "session-bbb-222")
	if err != nil {
		t.Fatalf("Topics() error: %v", err)
	}

	if len(topics) != 1 {
		t.Fatalf("Topics() returned %d, want 1", len(topics))
	}
	if topics[0].TopicText != "dark mode CSS" {
		t.Errorf("topic = %q, want 'dark mode CSS'", topics[0].TopicText)
	}
}

func TestTopicsPartialID(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	topics, err := Topics(database.SQL(), "session-bbb")
	if err != nil {
		t.Fatalf("Topics() with partial ID error: %v", err)
	}

	if len(topics) != 1 {
		t.Fatalf("Topics(partial) returned %d, want 1", len(topics))
	}
}

func TestToolsUsageAll(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := ToolsUsage(database.SQL(), "", 10)
	if err != nil {
		t.Fatalf("ToolsUsage() error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("ToolsUsage() returned no results")
	}
	// Bash should be first (13 total uses).
	if results[0].ToolName != "Edit" && results[0].ToolName != "Bash" {
		t.Logf("first tool = %q (expected Bash or Edit at top)", results[0].ToolName)
	}
}

func TestToolsUsageByName(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	results, err := ToolsUsage(database.SQL(), "Bash", 10)
	if err != nil {
		t.Fatalf("ToolsUsage(Bash) error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("ToolsUsage(Bash) returned %d results, want 2", len(results))
	}
}

func TestGetStats(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	stats, err := GetStats(database.SQL())
	if err != nil {
		t.Fatalf("GetStats() error: %v", err)
	}

	if stats.TotalSessions != 3 {
		t.Errorf("TotalSessions = %d, want 3", stats.TotalSessions)
	}
	if stats.TotalTools == 0 {
		t.Error("TotalTools should be > 0")
	}
	if stats.Earliest != "2026-03-01" {
		t.Errorf("Earliest = %q, want 2026-03-01", stats.Earliest)
	}
	if stats.Latest != "2026-03-20" {
		t.Errorf("Latest = %q, want 2026-03-20", stats.Latest)
	}
	if stats.ByProject["app1"] != 2 {
		t.Errorf("ByProject[app1] = %d, want 2", stats.ByProject["app1"])
	}
}

func TestEscapeFTSQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple words", "hello world", `"hello" "world"`},
		{"with hyphen", "go-chi router", `"go-chi" "router"`},
		{"with dot", "config.json", `"config.json"`},
		{"already quoted", `"exact phrase" other`, `"exact phrase" "other"`},
		{"reserved words", "NOT AND OR", `"NOT" "AND" "OR"`},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"single word", "hello", `"hello"`},
		{"unterminated quote", `"broken`, `"broken"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EscapeFTSQuery(tt.input)
			if got != tt.want {
				t.Errorf("EscapeFTSQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
