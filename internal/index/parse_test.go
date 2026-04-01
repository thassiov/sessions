package index

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
	path := filepath.Join(dir, "test-session-123.jsonl")
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

func TestParseSessionBasic(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00.000Z","message":{"content":"hello world, this is a test message"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:05.000Z","message":{"model":"claude-opus-4-6","content":[{"type":"text","text":"Hi there!"}]}}`,
		`{"type":"user","timestamp":"2026-03-01T10:01:00.000Z","message":{"content":"can you read a file for me?"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:01:05.000Z","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Read","input":{"path":"/tmp/foo"}},{"type":"text","text":"Here is the file."}]}}`,
	)

	data, err := ParseSession(path, "test project")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	if data.SessionID != "test-session-123" {
		t.Errorf("SessionID = %q, want test-session-123", data.SessionID)
	}
	if data.ProjectName != "test project" {
		t.Errorf("ProjectName = %q, want 'test project'", data.ProjectName)
	}
	if data.ExchangeCount != 4 {
		t.Errorf("ExchangeCount = %d, want 4", data.ExchangeCount)
	}
	if data.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want claude-opus-4-6", data.Model)
	}
	if data.StartTime != "2026-03-01T10:00:00.000Z" {
		t.Errorf("StartTime = %q, want 2026-03-01T10:00:00.000Z", data.StartTime)
	}
	if data.DurationMinutes != 1 {
		t.Errorf("DurationMinutes = %d, want 1", data.DurationMinutes)
	}

	// Tool usage.
	if data.Tools["Read"] != 1 {
		t.Errorf("Tools[Read] = %d, want 1", data.Tools["Read"])
	}

	// FTS content should contain user prompts.
	if data.FTSContent == "" {
		t.Error("FTSContent should not be empty")
	}
}

func TestParseSessionCustomTitle(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"custom-title","customTitle":">>> My Custom Title......[tag1, tag2]","timestamp":"2026-03-01T10:00:00Z"}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:01Z","message":{"content":"some user message here"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:02Z","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}]}}`,
	)

	data, err := ParseSession(path, "test")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	if data.Title != "My Custom Title" {
		t.Errorf("Title = %q, want 'My Custom Title'", data.Title)
	}
	if data.Tags != "tag1, tag2" {
		t.Errorf("Tags = %q, want 'tag1, tag2'", data.Tags)
	}
}

func TestParseSessionCompaction(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"working on something important"}}`,
		`{"type":"summary","summary":"Refactored the auth middleware","timestamp":"2026-03-01T11:00:00Z"}`,
		`{"type":"user","timestamp":"2026-03-01T12:00:00Z","message":{"content":"continuing after compaction"}}`,
	)

	data, err := ParseSession(path, "test")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	if data.HasCompaction != 1 {
		t.Error("HasCompaction should be 1")
	}
	if len(data.Topics) != 1 {
		t.Fatalf("Topics len = %d, want 1", len(data.Topics))
	}
	if data.Topics[0].Source != "compaction_summary" {
		t.Errorf("Topic source = %q, want compaction_summary", data.Topics[0].Source)
	}
}

func TestParseSessionContentBlocks(t *testing.T) {
	t.Parallel()

	// User message as array of content blocks (multimodal format).
	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":[{"type":"text","text":"first part"},{"type":"text","text":"second part"}]}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}]}}`,
	)

	data, err := ParseSession(path, "test")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	if data.FTSContent != "first part second part" {
		t.Errorf("FTSContent = %q, want 'first part second part'", data.FTSContent)
	}
}

func TestParseSessionAgentTracking(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"do some exploration for me"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"model":"claude-opus-4-6","content":[{"type":"tool_use","name":"Task","input":{"subagent_type":"Explore","prompt":"find files"}},{"type":"tool_use","name":"Task","input":{"subagent_type":"Explore","prompt":"search code"}},{"type":"tool_use","name":"Task","input":{"subagent_type":"Plan","prompt":"design approach"}}]}}`,
	)

	data, err := ParseSession(path, "test")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	if data.Tools["Task"] != 3 {
		t.Errorf("Tools[Task] = %d, want 3", data.Tools["Task"])
	}
	if data.Agents["Explore"] != 2 {
		t.Errorf("Agents[Explore] = %d, want 2", data.Agents["Explore"])
	}
	if data.Agents["Plan"] != 1 {
		t.Errorf("Agents[Plan] = %d, want 1", data.Agents["Plan"])
	}
}

func TestParseSessionAutoTitle(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"# Header\nsome content"}}`,
		`{"type":"user","timestamp":"2026-03-01T10:00:01Z","message":{"content":"fix the authentication bug in the login flow"}}`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:02Z","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}]}}`,
	)

	data, err := ParseSession(path, "test")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	// Should skip the markdown header and use the second prompt.
	if data.TitleDisplay != "fix the authentication bug in the login flow" {
		t.Errorf("TitleDisplay = %q, want 'fix the authentication bug in the login flow'", data.TitleDisplay)
	}
}

func TestParseSessionMalformedLines(t *testing.T) {
	t.Parallel()

	path := writeTestSession(t,
		`not json at all`,
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"valid message here"}}`,
		`{"broken json`,
		`{"type":"assistant","timestamp":"2026-03-01T10:00:01Z","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}]}}`,
	)

	data, err := ParseSession(path, "test")
	if err != nil {
		t.Fatalf("ParseSession() error: %v", err)
	}

	// Should still parse the valid lines.
	if data.ExchangeCount != 2 {
		t.Errorf("ExchangeCount = %d, want 2", data.ExchangeCount)
	}
}

func TestPickTitleFromPrompts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prompts []string
		want    string
	}{
		{
			name:    "normal prompt",
			prompts: []string{"fix the authentication bug"},
			want:    "fix the authentication bug",
		},
		{
			name:    "skip markdown header",
			prompts: []string{"# Some Header", "actual question here"},
			want:    "actual question here",
		},
		{
			name:    "skip short prompts",
			prompts: []string{"yes", "no", "a longer prompt that works"},
			want:    "a longer prompt that works",
		},
		{
			name:    "skip system prompts",
			prompts: []string{"You are a helpful assistant", "real user message here"},
			want:    "real user message here",
		},
		{
			name:    "truncate long titles",
			prompts: []string{"this is an extremely long prompt that should be truncated because it exceeds the maximum allowed length for a title display"},
			want:    "this is an extremely long prompt that should be truncated because it exceeds ...",
		},
		{
			name:    "empty prompts",
			prompts: []string{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pickTitleFromPrompts(tt.prompts)
			if got != tt.want {
				t.Errorf("pickTitleFromPrompts() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
