package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestSession(t *testing.T, lines ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "-test-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test-session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
	return path
}

func TestExtractExchanges(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"fix the auth bug"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:05Z","message":{"content":[{"type":"text","text":"I'll look into the auth module."}]}}`,
		`{"type":"user","timestamp":"2026-03-01T10:01:00Z","message":{"content":"what about the database?"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:01:05Z","message":{"content":[{"type":"text","text":"The database is fine."}]}}`,
	)

	exchanges, err := ExtractExchanges(path, "", 10, 1000)
	if err != nil {
		t.Fatalf("ExtractExchanges() error: %v", err)
	}

	if len(exchanges) != 2 {
		t.Fatalf("got %d exchanges, want 2", len(exchanges))
	}
	if exchanges[0].User != "fix the auth bug" {
		t.Errorf("first user = %q, want 'fix the auth bug'", exchanges[0].User)
	}
	if exchanges[0].Assistant != "I'll look into the auth module." {
		t.Errorf("first assistant = %q", exchanges[0].Assistant)
	}
}

func TestExtractExchangesWithQuery(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"fix the auth bug"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:05Z","message":{"content":[{"type":"text","text":"Looking at auth."}]}}`,
		`{"type":"user","timestamp":"2026-03-01T10:01:00Z","message":{"content":"what about the database?"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:01:05Z","message":{"content":[{"type":"text","text":"Database looks good."}]}}`,
	)

	exchanges, err := ExtractExchanges(path, "database", 10, 1000)
	if err != nil {
		t.Fatalf("ExtractExchanges() error: %v", err)
	}

	if len(exchanges) != 1 {
		t.Fatalf("got %d exchanges, want 1 matching 'database'", len(exchanges))
	}
	if exchanges[0].User != "what about the database?" {
		t.Errorf("matched user = %q", exchanges[0].User)
	}
}

func TestExtractExchangesToolSummarization(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"read the config file"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:05Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/etc/config.json"}},{"type":"text","text":"Here is the config."}]}}`,
	)

	exchanges, err := ExtractExchanges(path, "", 10, 1000)
	if err != nil {
		t.Fatalf("ExtractExchanges() error: %v", err)
	}

	if len(exchanges) != 1 {
		t.Fatalf("got %d exchanges, want 1", len(exchanges))
	}
	// Should contain tool summary.
	if !contains(exchanges[0].Assistant, "[Read: /etc/config.json]") {
		t.Errorf("assistant should contain tool summary, got: %q", exchanges[0].Assistant)
	}
}

func TestExtractExchangesTruncation(t *testing.T) {
	t.Parallel()

	longMsg := ""
	for i := 0; i < 200; i++ {
		longMsg += "word "
	}

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"`+longMsg+`"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:05Z","message":{"content":[{"type":"text","text":"ok"}]}}`,
	)

	exchanges, err := ExtractExchanges(path, "", 10, 50)
	if err != nil {
		t.Fatalf("ExtractExchanges() error: %v", err)
	}

	if len(exchanges) != 1 {
		t.Fatalf("got %d exchanges, want 1", len(exchanges))
	}
	if len(exchanges[0].User) > 54 { // 50 + "..."
		t.Errorf("user text not truncated, len = %d", len(exchanges[0].User))
	}
}

func TestExtractExchangesLimit(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"message one here"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"content":[{"type":"text","text":"reply one"}]}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:02Z","message":{"content":"message two here"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:03Z","message":{"content":[{"type":"text","text":"reply two"}]}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:04Z","message":{"content":"message three here"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:05Z","message":{"content":[{"type":"text","text":"reply three"}]}}`,
	)

	exchanges, err := ExtractExchanges(path, "", 2, 1000)
	if err != nil {
		t.Fatalf("ExtractExchanges() error: %v", err)
	}

	if len(exchanges) != 2 {
		t.Errorf("got %d exchanges, want 2 (limited)", len(exchanges))
	}
}

func TestFilterExchangesRegex(t *testing.T) {
	t.Parallel()

	exchanges := []Exchange{
		{User: "fix the auth-v2 bug", Assistant: "ok"},
		{User: "what about database?", Assistant: "fine"},
	}

	filtered := filterExchanges(exchanges, "auth-v\\d+")
	if len(filtered) != 1 {
		t.Fatalf("regex filter: got %d, want 1", len(filtered))
	}
}

func TestFilterExchangesWordLevel(t *testing.T) {
	t.Parallel()

	exchanges := []Exchange{
		{User: "working on the frontend", Assistant: "ok"},
		{User: "backend is done", Assistant: "great"},
	}

	// "frontend backend" won't match as substring, but individual words should.
	filtered := filterExchanges(exchanges, "frontend backend")
	if len(filtered) != 2 {
		t.Fatalf("word-level filter: got %d, want 2", len(filtered))
	}
}

func TestSummarizeToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		block    contentBlock
		contains string
	}{
		{"Read", contentBlock{Name: "Read", Input: []byte(`{"file_path":"/foo/bar"}`)}, "[Read: /foo/bar]"},
		{"Bash", contentBlock{Name: "Bash", Input: []byte(`{"command":"ls -la"}`)}, "[Bash: ls -la]"},
		{"Unknown", contentBlock{Name: "CustomTool", Input: []byte(`{}`)}, "[CustomTool]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := summarizeToolCall(tt.block)
			if !contains(got, tt.contains) {
				t.Errorf("summarizeToolCall() = %q, want containing %q", got, tt.contains)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || s != "" && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
