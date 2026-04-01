// Package hook handles Claude Code hook events for live topic capture.
package hook

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/thassiov/sessions/internal/config"
	"github.com/thassiov/sessions/internal/db"
	"github.com/thassiov/sessions/internal/index"
)

// TopicInterval is the number of user exchanges between periodic captures.
const TopicInterval = 10

// Noise patterns to skip when extracting topics.
var skipPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(yes|no|ok|sure|thanks|thank you|yep|nope|cool|great|good|fine|hmm|ah|oh)\b`),
	regexp.MustCompile(`^/`),
	regexp.MustCompile(`^<system-reminder>`),
	regexp.MustCompile(`^<task-notification>`),
	regexp.MustCompile(`(?i)^This session has ended`),
	regexp.MustCompile(`^\[Request interrupted`),
	regexp.MustCompile(`(?i)^Please curate the memories`),
	regexp.MustCompile(`(?i)^implement the following plan`),
}

// Cleanup patterns for topic extraction.
var (
	systemReminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	codeBlockRE      = regexp.MustCompile("(?s)```[\\s\\S]*?```")
	inlineCodeRE     = regexp.MustCompile("`[^`]+`")
	markdownLinkRE   = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	formattingRE     = regexp.MustCompile(`[#*_~]+`)
	sentenceSplitRE  = regexp.MustCompile(`[.!?]\s+`)
	clauseSplitRE    = regexp.MustCompile(`[,;:\x{2014}\x{2013}\-]\s+`)
)

// StdinData holds the JSON data passed to hooks via stdin.
type StdinData struct {
	SessionID string `json:"session_id"`
}

// ReadStdin reads hook input from stdin.
func ReadStdin(r io.Reader) StdinData {
	var data StdinData
	bytes, err := io.ReadAll(r)
	if err != nil {
		return data
	}
	json.Unmarshal(bytes, &data) //nolint:errcheck
	return data
}

// Handle dispatches hook events.
func Handle(event string, sessionID string, database *db.DB, cfg *config.Config, logger *slog.Logger) {
	switch event {
	case "UserPromptSubmit":
		handleUserPromptSubmit(sessionID, database, cfg, logger)
	case "PreCompact":
		handlePreCompact(sessionID, database, cfg, logger)
	case "SessionEnd":
		handleSessionEnd(sessionID, database, cfg, logger)
	default:
		logger.Debug("unknown hook event", "event", event)
	}
}

func handleUserPromptSubmit(sessionID string, database *db.DB, cfg *config.Config, logger *slog.Logger) {
	sessionPath := FindSessionFile(cfg.ProjectsDir, sessionID)
	if sessionPath == "" {
		return
	}

	exchangeCount := CountUserMessages(sessionPath)
	if exchangeCount < TopicInterval {
		return
	}

	// Check hook_state in DB.
	lastCapture := GetLastCapture(database, sessionID)
	if exchangeCount-lastCapture < TopicInterval {
		return
	}

	messages := ExtractRecentUserMessages(sessionPath, 3)
	topic := ExtractTopic(messages)
	if topic == "" {
		return
	}

	WriteTopic(database, sessionID, topic, "hook_periodic", exchangeCount, logger)
	UpdateLastCapture(database, sessionID, exchangeCount, logger)
}

func handlePreCompact(sessionID string, database *db.DB, cfg *config.Config, logger *slog.Logger) {
	sessionPath := FindSessionFile(cfg.ProjectsDir, sessionID)
	if sessionPath == "" {
		return
	}

	messages := ExtractRecentUserMessages(sessionPath, 5)
	topic := ExtractTopic(messages)
	if topic == "" {
		return
	}

	exchangeCount := CountUserMessages(sessionPath)
	WriteTopic(database, sessionID, topic, "hook_precompact", exchangeCount, logger)
}

func handleSessionEnd(sessionID string, database *db.DB, cfg *config.Config, logger *slog.Logger) {
	sessionPath := FindSessionFile(cfg.ProjectsDir, sessionID)
	if sessionPath == "" {
		return
	}

	// Capture final topic.
	messages := ExtractRecentUserMessages(sessionPath, 5)
	topic := ExtractTopic(messages)
	if topic != "" {
		exchangeCount := CountUserMessages(sessionPath)
		WriteTopic(database, sessionID, topic, "hook_session_end", exchangeCount, logger)
	}

	// Trigger full session reindex.
	if err := index.IndexOne(database, cfg, sessionID); err != nil {
		logger.Warn("session reindex failed", "session", sessionID, "error", err)
	} else {
		logger.Info("session reindexed", "session", sessionID)
	}

	// Clean up hook state.
	database.SQL().Exec("DELETE FROM hook_state WHERE session_id=?", sessionID) //nolint:errcheck
}

// FindSessionFile locates a session JSONL file across project directories.
func FindSessionFile(projectsDir, sessionID string) string {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// CountUserMessages counts user messages in a session JSONL file.
func CountUserMessages(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Fast check without full JSON parse.
		if strings.Contains(string(line), `"type":"user"`) || strings.Contains(string(line), `"type": "user"`) {
			count++
		}
	}
	return count
}

// ExtractRecentUserMessages reads the last N substantive user messages from a session JSONL.
func ExtractRecentUserMessages(path string, count int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck

	// Read last 200KB for efficiency on large files.
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	readSize := int64(200_000)
	if info.Size() < readSize {
		readSize = info.Size()
	}
	if _, err := f.Seek(info.Size()-readSize, io.SeekStart); err != nil {
		return nil
	}

	// Collect all user messages from the tail, then take last N.
	var allMessages []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "user" {
			continue
		}

		text := extractUserText(entry.Message)
		if len(text) < 15 {
			continue
		}

		// Skip noise.
		skip := false
		for _, re := range skipPatterns {
			if re.MatchString(text) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		allMessages = append(allMessages, text)
	}

	// Take last N messages (chronological order).
	if len(allMessages) > count {
		allMessages = allMessages[len(allMessages)-count:]
	}
	return allMessages
}

func extractUserText(raw json.RawMessage) string {
	var body struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}

	var s string
	if err := json.Unmarshal(body.Content, &s); err == nil {
		return s
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
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

// ExtractTopic extracts a concise topic from recent user messages.
func ExtractTopic(messages []string) string {
	if len(messages) == 0 {
		return ""
	}

	// Use most recent message.
	msg := messages[len(messages)-1]

	// Strip system reminders.
	msg = systemReminderRE.ReplaceAllString(msg, "")

	// Strip markdown.
	msg = codeBlockRE.ReplaceAllString(msg, "")
	msg = inlineCodeRE.ReplaceAllString(msg, "")
	msg = markdownLinkRE.ReplaceAllString(msg, "$1")
	msg = formattingRE.ReplaceAllString(msg, "")

	// Clean whitespace.
	msg = strings.Join(strings.Fields(msg), " ")
	msg = strings.TrimSpace(msg)

	if len(msg) < 10 {
		// Fall back to second most recent.
		if len(messages) >= 2 {
			return ExtractTopic(messages[:len(messages)-1])
		}
		return ""
	}

	// Extract first sentence.
	sentences := sentenceSplitRE.Split(msg, -1)
	topic := strings.TrimSpace(sentences[0])

	// If too long, take first clause.
	if len(topic) > 60 {
		clauses := clauseSplitRE.Split(topic, -1)
		topic = strings.TrimSpace(clauses[0])
	}

	// Final truncation.
	if len(topic) > 60 {
		topic = topic[:57] + "..."
	}

	// Capitalize first letter.
	if len(topic) > 0 {
		topic = strings.ToUpper(topic[:1]) + topic[1:]
	}

	return topic
}

// GetLastCapture returns the exchange count at the last topic capture for a session.
func GetLastCapture(database *db.DB, sessionID string) int {
	var lastCapture int
	err := database.SQL().QueryRow(
		"SELECT last_capture_exchange FROM hook_state WHERE session_id=?", sessionID,
	).Scan(&lastCapture)
	if err != nil {
		return 0
	}
	return lastCapture
}

// UpdateLastCapture updates the hook state for a session's last capture point.
func UpdateLastCapture(database *db.DB, sessionID string, exchangeCount int, logger *slog.Logger) {
	now := time.Now().Format(time.RFC3339)
	_, err := database.SQL().Exec(`
		INSERT INTO hook_state (session_id, last_capture_exchange, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			last_capture_exchange=excluded.last_capture_exchange,
			updated_at=excluded.updated_at
	`, sessionID, exchangeCount, now)
	if err != nil && logger != nil {
		logger.Warn("updating hook state", "error", err)
	}
}

// WriteTopic writes a topic entry to the session_topics table.
func WriteTopic(database *db.DB, sessionID, topic, source string, exchangeNumber int, logger *slog.Logger) {
	now := time.Now().Format(time.RFC3339)
	_, err := database.SQL().Exec(
		"INSERT INTO session_topics (session_id, topic, captured_at, exchange_number, source) VALUES (?, ?, ?, ?, ?)",
		sessionID, topic, now, exchangeNumber, source,
	)
	if logger == nil {
		return
	}
	if err != nil {
		logger.Warn("writing topic", "error", err)
	} else {
		sid := sessionID
		if len(sid) > 8 {
			sid = sid[:8]
		}
		logger.Info("topic captured", "session", sid, "source", source, "topic", topic)
	}
}

// Ensure sql import is used (referenced in handleSessionEnd for DELETE).
var _ = sql.ErrNoRows
