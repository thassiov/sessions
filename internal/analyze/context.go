// Package analyze provides context retrieval and analytics for sessions.
package analyze

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Exchange represents a user-assistant conversation pair.
type Exchange struct {
	User      string
	Assistant string
	Timestamp string
}

// ContextResult holds conversation context for a session.
type ContextResult struct {
	SessionID       string
	Title           string
	ProjectName     string
	StartTime       string
	ExchangeCount   int
	DurationMinutes int
	Query           string
	Exchanges       []Exchange
}

// GetContext retrieves conversation exchanges for a session.
func GetContext(db *sql.DB, sessionID, query string, limit int) (*ContextResult, error) {
	// Resolve partial session ID.
	var filePath string
	var result ContextResult

	row := db.QueryRow(`
		SELECT session_id, file_path, COALESCE(title_display,''), COALESCE(project_name,''),
		       COALESCE(start_time,''), exchange_count, COALESCE(duration_minutes,0)
		FROM sessions WHERE session_id LIKE ?
	`, sessionID+"%")

	err := row.Scan(&result.SessionID, &filePath, &result.Title, &result.ProjectName,
		&result.StartTime, &result.ExchangeCount, &result.DurationMinutes)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	result.Query = query
	exchanges, err := ExtractExchanges(filePath, query, limit, 1000)
	if err != nil {
		return nil, err
	}
	result.Exchanges = exchanges

	return &result, nil
}

// ExtractExchanges reads a session JSONL and extracts user-assistant pairs.
func ExtractExchanges(path, query string, limit, maxChars int) ([]Exchange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening session: %w", err)
	}
	defer f.Close() //nolint:errcheck // file closed on function return

	type entry struct {
		role      string
		text      string
		timestamp string
	}

	var entries []entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var raw jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		if raw.Type != "user" && raw.Type != "assistant" {
			continue
		}

		var body messageBody
		if err := json.Unmarshal(raw.Message, &body); err != nil {
			continue
		}

		var text string
		if raw.Type == "user" {
			text = extractUserText(body.Content)
		} else {
			text = extractAssistantText(body.Content)
		}

		entries = append(entries, entry{role: raw.Type, text: text, timestamp: raw.Timestamp})
	}

	// Pair user -> assistant.
	var exchanges []Exchange
	i := 0
	for i < len(entries) {
		if entries[i].role != "user" {
			i++
			continue
		}

		userText := entries[i].text
		userTS := entries[i].timestamp
		var assistantParts []string

		j := i + 1
		for j < len(entries) && entries[j].role == "assistant" {
			assistantParts = append(assistantParts, entries[j].text)
			j++
		}

		exchanges = append(exchanges, Exchange{
			User:      userText,
			Assistant: strings.Join(assistantParts, "\n"),
			Timestamp: userTS,
		})
		i = j
	}

	// Filter by query if provided.
	if query != "" {
		exchanges = filterExchanges(exchanges, query)
	}

	// Truncate.
	for i := range exchanges {
		if len(exchanges[i].User) > maxChars {
			exchanges[i].User = exchanges[i].User[:maxChars] + "..."
		}
		if len(exchanges[i].Assistant) > maxChars {
			exchanges[i].Assistant = exchanges[i].Assistant[:maxChars] + "..."
		}
	}

	if len(exchanges) > limit {
		exchanges = exchanges[:limit]
	}
	return exchanges, nil
}

func filterExchanges(exchanges []Exchange, query string) []Exchange {
	// Try regex first.
	if re, err := regexp.Compile("(?i)" + query); err == nil {
		var matched []Exchange
		for _, ex := range exchanges {
			if re.MatchString(ex.User) || re.MatchString(ex.Assistant) {
				matched = append(matched, ex)
			}
		}
		if len(matched) > 0 {
			return matched
		}
	}

	// Substring match.
	q := strings.ToLower(query)
	var matched []Exchange
	for _, ex := range exchanges {
		if strings.Contains(strings.ToLower(ex.User), q) || strings.Contains(strings.ToLower(ex.Assistant), q) {
			matched = append(matched, ex)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	// Word-level match.
	words := strings.Fields(query)
	var significantWords []string
	for _, w := range words {
		if len(w) > 2 {
			significantWords = append(significantWords, strings.ToLower(w))
		}
	}
	if len(significantWords) == 0 {
		return nil
	}

	for _, ex := range exchanges {
		lower := strings.ToLower(ex.User + " " + ex.Assistant)
		for _, w := range significantWords {
			if strings.Contains(lower, w) {
				matched = append(matched, ex)
				break
			}
		}
	}
	return matched
}

// JSONL types shared with index package (local copies to avoid import cycle).
type jsonlEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type messageBody struct {
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func extractUserText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
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

func extractAssistantText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			} else if b.Type == "tool_use" {
				parts = append(parts, summarizeToolCall(b))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func summarizeToolCall(b contentBlock) string {
	var inp map[string]interface{}
	if len(b.Input) > 0 {
		json.Unmarshal(b.Input, &inp) //nolint:errcheck // best-effort parse for display
	}

	getStr := func(key string) string {
		if v, ok := inp[key].(string); ok {
			return v
		}
		return "?"
	}

	truncStr := func(s string, n int) string {
		if len(s) > n {
			return s[:n] + "..."
		}
		return s
	}

	switch b.Name {
	case "Read":
		return fmt.Sprintf("[Read: %s]", getStr("file_path"))
	case "Edit":
		return fmt.Sprintf("[Edit: %s]", getStr("file_path"))
	case "Write":
		return fmt.Sprintf("[Write: %s]", getStr("file_path"))
	case "Bash":
		return fmt.Sprintf("[Bash: %s]", truncStr(getStr("command"), 60))
	case "Task":
		return fmt.Sprintf("[Task: %q -> %s]", truncStr(getStr("description"), 40), getStr("subagent_type"))
	case "Grep":
		return fmt.Sprintf("[Grep: %s]", getStr("pattern"))
	case "Glob":
		return fmt.Sprintf("[Glob: %s]", getStr("pattern"))
	case "WebFetch":
		return fmt.Sprintf("[WebFetch: %s]", truncStr(getStr("url"), 60))
	case "WebSearch":
		return fmt.Sprintf("[WebSearch: %s]", getStr("query"))
	default:
		return fmt.Sprintf("[%s]", b.Name)
	}
}
