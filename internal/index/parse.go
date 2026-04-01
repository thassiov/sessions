package index

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thassiov/sessions/internal/db"
)

// jsonlEntry represents a single line from the session JSONL.
type jsonlEntry struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
	Summary     string          `json:"summary"`
	CustomTitle string          `json:"customTitle"`
}

type messageBody struct {
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type taskInput struct {
	SubagentType string `json:"subagent_type"`
}

// Skip these prefixes when picking an auto-title from user prompts.
var skipTitlePrefixes = []string{
	"#",           // Markdown headers.
	"You are",     // Agent system prompts.
	"Caveat:",     // System caveats injected by hooks.
	"Explore the", // Agent exploration prompts.
}

// ParseSession reads a session JSONL file and extracts all indexable data.
func ParseSession(path, projectName string) (*db.SessionData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening session: %w", err)
	}
	defer f.Close() //nolint:errcheck // file closed on function return

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat session: %w", err)
	}

	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	projectDir := filepath.Base(filepath.Dir(path))

	data := &db.SessionData{
		SessionID:   sessionID,
		Project:     projectDir,
		ProjectName: projectName,
		FilePath:    path,
		FileSize:    info.Size(),
		Tools:       make(map[string]int),
		Agents:      make(map[string]int),
	}

	var userPrompts []string
	var summaries []string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line.

	for scanner.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Skip malformed lines.
		}

		// Track timestamps.
		if entry.Timestamp != "" {
			if data.StartTime == "" {
				data.StartTime = entry.Timestamp
			}
			data.EndTime = entry.Timestamp
		}

		switch entry.Type {
		case "custom-title":
			parseCustomTitle(entry.CustomTitle, data)

		case "summary":
			data.HasCompaction = 1
			if entry.Summary != "" {
				summaries = append(summaries, parseSummary(entry.Summary))
			}

		case "user":
			data.ExchangeCount++
			text := extractUserText(entry.Message)
			if len(text) > 10 {
				userPrompts = append(userPrompts, text)
			}

		case "assistant":
			data.ExchangeCount++
			extractAssistantMeta(entry.Message, data)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning session: %w", err)
	}

	// Auto-generate title if none was set.
	if data.TitleDisplay == "" && len(summaries) > 0 {
		data.TitleDisplay = truncate(summaries[0], 80)
		data.Title = data.TitleDisplay
	} else if data.TitleDisplay == "" && len(userPrompts) > 0 {
		if title := pickTitleFromPrompts(userPrompts); title != "" {
			data.TitleDisplay = title
			data.Title = title
		}
	}

	// Calculate duration.
	data.DurationMinutes = calcDuration(data.StartTime, data.EndTime)

	// Build FTS content from user prompts.
	data.FTSContent = buildFTSContent(userPrompts)

	// File hash.
	hash, _ := FileHash(path)
	data.FileHash = hash

	// Topics from compaction summaries.
	capturedAt := data.EndTime
	if capturedAt == "" {
		capturedAt = time.Now().Format(time.RFC3339)
	}
	for _, s := range summaries {
		data.Topics = append(data.Topics, db.TopicEntry{
			Topic:      truncate(s, 120),
			Source:     "compaction_summary",
			CapturedAt: capturedAt,
		})
	}

	return data, nil
}

func parseCustomTitle(raw string, data *db.SessionData) {
	data.TitleDisplay = raw
	if !strings.HasPrefix(raw, ">>>") {
		return
	}
	parts := strings.Split(raw, "......")
	namePart := strings.TrimSpace(strings.TrimPrefix(parts[0], ">>>"))
	data.Title = namePart
	data.TitleDisplay = raw
	if len(parts) > 1 {
		tagPart := strings.Trim(strings.TrimSpace(parts[len(parts)-1]), "[]<>")
		tagPart = strings.TrimSpace(tagPart)
		if tagPart != "" {
			data.Tags = tagPart
		}
	}
}

func parseSummary(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return raw
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return raw
	}
	if v, ok := parsed["title"].(string); ok && v != "" {
		return v
	}
	if v, ok := parsed["summary"].(string); ok && v != "" {
		return v
	}
	return raw
}

func extractUserText(raw json.RawMessage) string {
	var body messageBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}

	// Try as string first.
	var s string
	if err := json.Unmarshal(body.Content, &s); err == nil {
		return s
	}

	// Try as array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(body.Content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}

	return ""
}

func extractAssistantMeta(raw json.RawMessage, data *db.SessionData) {
	var body messageBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return
	}

	// Extract model from first assistant message.
	if data.Model == "" && body.Model != "" {
		data.Model = body.Model
	}

	// Extract tool usage.
	var blocks []contentBlock
	if err := json.Unmarshal(body.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		if b.Type != "tool_use" || b.Name == "" {
			continue
		}
		data.Tools[b.Name]++

		// Track Task invocations as agents.
		if b.Name == "Task" && len(b.Input) > 0 {
			var input taskInput
			if err := json.Unmarshal(b.Input, &input); err == nil && input.SubagentType != "" {
				data.Agents[input.SubagentType]++
			}
		}
	}
}

func pickTitleFromPrompts(prompts []string) string {
	limit := 5
	if len(prompts) < limit {
		limit = len(prompts)
	}
	for _, prompt := range prompts[:limit] {
		firstLine := strings.SplitN(strings.TrimSpace(prompt), "\n", 2)[0]
		firstLine = strings.TrimSpace(firstLine)
		if len(firstLine) <= 10 {
			continue
		}
		skip := false
		for _, prefix := range skipTitlePrefixes {
			if strings.HasPrefix(firstLine, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		return truncate(firstLine, 80)
	}
	return ""
}

func calcDuration(startTime, endTime string) int {
	if startTime == "" || endTime == "" {
		return 0
	}
	s, err := time.Parse(time.RFC3339, startTime)
	if err != nil {
		s, err = time.Parse("2006-01-02T15:04:05.000Z", startTime)
		if err != nil {
			return 0
		}
	}
	e, err := time.Parse(time.RFC3339, endTime)
	if err != nil {
		e, err = time.Parse("2006-01-02T15:04:05.000Z", endTime)
		if err != nil {
			return 0
		}
	}
	return int(e.Sub(s).Minutes())
}

func buildFTSContent(prompts []string) string {
	limit := 50
	if len(prompts) < limit {
		limit = len(prompts)
	}
	content := strings.Join(prompts[:limit], "\n")
	if len(content) > 100000 {
		content = content[:100000]
	}
	return content
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
