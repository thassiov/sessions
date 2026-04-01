// Package search provides FTS5 search and structured filtering for sessions.
package search

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Result holds a single search result.
type Result struct {
	SessionID      string
	ProjectName    string
	Title          string
	TitleDisplay   string
	Client         string
	Tags           string
	ExchangeCount  int
	StartTime      string
	DurationMinutes int
	HasCompaction  int
	Snippet        string
	Topics         []Topic
}

// Topic is a compact topic entry for display.
type Topic struct {
	TopicText string
	Source    string
}

// TopicDetail is a full topic entry with all fields.
type TopicDetail struct {
	TopicText      string
	CapturedAt     string
	ExchangeNumber *int
	Source         string
}

// ToolResult holds tool usage data.
type ToolResult struct {
	SessionID    string
	Title        string
	TitleDisplay string
	StartTime    string
	ToolName     string
	UseCount     int
	Total        int
	SessionCount int
}

// Stats holds database statistics.
type Stats struct {
	TotalSessions     int
	TotalTopics       int
	TotalTools        int
	TotalAgents       int
	SessionsWithTopics int
	Earliest          string
	Latest            string
	ByProject         map[string]int
	ByClient          map[string]int
	TopTools          map[string]int
}

// FilterOpts holds options for the Find method.
type FilterOpts struct {
	Client         string
	Tag            string
	Tool           string
	Agent          string
	Date           string
	Week           bool
	Days           int
	Project        string
	ExcludeProject string
	HasCompaction  *bool
	Limit          int
}

// Search performs FTS5 full-text search across all sessions.
func Search(db *sql.DB, query string, limit int) ([]Result, error) {
	escaped := EscapeFTSQuery(query)
	if escaped == "" {
		return nil, nil
	}

	rows, err := db.Query(`
		SELECT s.session_id, s.project_name, s.title, s.title_display,
		       s.client, s.tags, s.exchange_count, s.start_time,
		       s.duration_minutes, s.has_compaction,
		       snippet(session_content, 1, '>>>', '<<<', '...', 40) as snippet
		FROM session_content
		JOIN sessions s ON s.session_id = session_content.session_id
		WHERE session_content MATCH ?
		ORDER BY rank
		LIMIT ?
	`, escaped, limit)
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return scanResults(db, rows)
}

// Find filters sessions by various criteria.
func Find(db *sql.DB, opts FilterOpts) ([]Result, error) {
	var conditions []string
	var params []interface{}

	if opts.Client != "" {
		conditions = append(conditions, "s.client LIKE ?")
		params = append(params, "%"+opts.Client+"%")
	}
	if opts.Tag != "" {
		conditions = append(conditions, "s.tags LIKE ?")
		params = append(params, "%"+opts.Tag+"%")
	}
	if opts.Tool != "" {
		conditions = append(conditions, "s.session_id IN (SELECT session_id FROM session_tools WHERE tool_name LIKE ?)")
		params = append(params, "%"+opts.Tool+"%")
	}
	if opts.Agent != "" {
		conditions = append(conditions, "s.session_id IN (SELECT session_id FROM session_agents WHERE agent_name LIKE ?)")
		params = append(params, "%"+opts.Agent+"%")
	}
	if opts.Project != "" {
		conditions = append(conditions, "(s.project_name LIKE ? OR s.project LIKE ?)")
		params = append(params, "%"+opts.Project+"%", "%"+opts.Project+"%")
	}
	if opts.Date != "" {
		conditions = append(conditions, "s.start_time LIKE ?")
		params = append(params, opts.Date+"%")
	}
	if opts.Week {
		weekAgo := time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
		conditions = append(conditions, "s.start_time >= ?")
		params = append(params, weekAgo)
	}
	if opts.Days > 0 {
		daysAgo := time.Now().AddDate(0, 0, -opts.Days).Format(time.RFC3339)
		conditions = append(conditions, "s.start_time >= ?")
		params = append(params, daysAgo)
	}
	if opts.ExcludeProject != "" {
		conditions = append(conditions, "NOT (s.project_name LIKE ? OR s.project LIKE ?)")
		params = append(params, "%"+opts.ExcludeProject+"%", "%"+opts.ExcludeProject+"%")
	}
	if opts.HasCompaction != nil {
		val := 0
		if *opts.HasCompaction {
			val = 1
		}
		conditions = append(conditions, "s.has_compaction = ?")
		params = append(params, val)
	}

	where := "1=1"
	if len(conditions) > 0 {
		where = strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	params = append(params, limit)

	query := fmt.Sprintf(`
		SELECT s.session_id, s.project_name, s.title, s.title_display,
		       s.client, s.tags, s.exchange_count, s.start_time,
		       s.duration_minutes, s.has_compaction
		FROM sessions s
		WHERE %s
		ORDER BY s.start_time DESC
		LIMIT ?
	`, where)

	rows, err := db.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("finding sessions: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return scanFindResults(db, rows)
}

// Recent returns the N most recent sessions.
func Recent(db *sql.DB, n int) ([]Result, error) {
	return Find(db, FilterOpts{Limit: n})
}

// Topics returns the full topic timeline for a session.
func Topics(db *sql.DB, sessionID string) ([]TopicDetail, error) {
	// Resolve partial session ID.
	sessionID, err := resolveSessionID(db, sessionID)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT topic, captured_at, exchange_number, source
		FROM session_topics
		WHERE session_id = ?
		ORDER BY captured_at
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying topics: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var topics []TopicDetail
	for rows.Next() {
		var t TopicDetail
		if err := rows.Scan(&t.TopicText, &t.CapturedAt, &t.ExchangeNumber, &t.Source); err != nil {
			return nil, fmt.Errorf("scanning topic: %w", err)
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// ToolsUsage returns tool usage data, optionally filtered by tool name.
func ToolsUsage(db *sql.DB, toolName string, limit int) ([]ToolResult, error) {
	if limit <= 0 {
		limit = 20
	}

	if toolName != "" {
		return toolsByName(db, toolName, limit)
	}
	return topTools(db, limit)
}

// GetStats returns database statistics.
func GetStats(db *sql.DB) (*Stats, error) {
	s := &Stats{
		ByProject: make(map[string]int),
		ByClient:  make(map[string]int),
		TopTools:  make(map[string]int),
	}

	db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&s.TotalSessions)                           //nolint:errcheck
	db.QueryRow("SELECT COUNT(*) FROM session_topics").Scan(&s.TotalTopics)                        //nolint:errcheck
	db.QueryRow("SELECT COUNT(DISTINCT tool_name) FROM session_tools").Scan(&s.TotalTools)         //nolint:errcheck
	db.QueryRow("SELECT COUNT(DISTINCT agent_name) FROM session_agents").Scan(&s.TotalAgents)      //nolint:errcheck
	db.QueryRow("SELECT COUNT(DISTINCT session_id) FROM session_topics").Scan(&s.SessionsWithTopics) //nolint:errcheck

	var earliest, latest sql.NullString
	db.QueryRow("SELECT MIN(start_time), MAX(start_time) FROM sessions WHERE start_time IS NOT NULL").Scan(&earliest, &latest) //nolint:errcheck
	if earliest.Valid && len(earliest.String) >= 10 {
		s.Earliest = earliest.String[:10]
	}
	if latest.Valid && len(latest.String) >= 10 {
		s.Latest = latest.String[:10]
	}

	// By project.
	rows, _ := db.Query("SELECT project_name, COUNT(*) as cnt FROM sessions GROUP BY project_name ORDER BY cnt DESC")
	if rows != nil {
		defer rows.Close() //nolint:errcheck
		for rows.Next() {
			var name string
			var cnt int
			if rows.Scan(&name, &cnt) == nil {
				s.ByProject[name] = cnt
			}
		}
	}

	// By client.
	rows2, _ := db.Query("SELECT client, COUNT(*) as cnt FROM sessions WHERE client IS NOT NULL AND client != '' GROUP BY client ORDER BY cnt DESC")
	if rows2 != nil {
		defer rows2.Close() //nolint:errcheck
		for rows2.Next() {
			var name string
			var cnt int
			if rows2.Scan(&name, &cnt) == nil {
				s.ByClient[name] = cnt
			}
		}
	}

	// Top tools.
	rows3, _ := db.Query("SELECT tool_name, SUM(use_count) as total FROM session_tools GROUP BY tool_name ORDER BY total DESC LIMIT 10")
	if rows3 != nil {
		defer rows3.Close() //nolint:errcheck
		for rows3.Next() {
			var name string
			var cnt int
			if rows3.Scan(&name, &cnt) == nil {
				s.TopTools[name] = cnt
			}
		}
	}

	return s, nil
}

// EscapeFTSQuery wraps each token in double quotes for FTS5 safety.
func EscapeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	var tokens []string
	i := 0
	for i < len(query) {
		// Skip whitespace.
		if query[i] == ' ' || query[i] == '\t' {
			i++
			continue
		}

		// Already-quoted phrase — preserve verbatim.
		if query[i] == '"' {
			end := strings.IndexByte(query[i+1:], '"')
			if end == -1 {
				tokens = append(tokens, fmt.Sprintf(`"%s"`, query[i+1:]))
				break
			}
			tokens = append(tokens, query[i:i+1+end+1])
			i = i + 1 + end + 1
			continue
		}

		// Unquoted token — collect until whitespace or quote.
		start := i
		for i < len(query) && query[i] != ' ' && query[i] != '\t' && query[i] != '"' {
			i++
		}
		tokens = append(tokens, fmt.Sprintf(`"%s"`, query[start:i]))
	}

	return strings.Join(tokens, " ")
}

func resolveSessionID(db *sql.DB, sessionID string) (string, error) {
	if len(sessionID) >= 36 {
		return sessionID, nil
	}
	var resolved string
	err := db.QueryRow(
		"SELECT session_id FROM sessions WHERE session_id LIKE ?", sessionID+"%",
	).Scan(&resolved)
	if err != nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	return resolved, nil
}

func scanResults(db *sql.DB, rows *sql.Rows) ([]Result, error) {
	var results []Result
	for rows.Next() {
		var r Result
		var title, titleDisplay, client, tags, snippet sql.NullString
		err := rows.Scan(
			&r.SessionID, &r.ProjectName, &title, &titleDisplay,
			&client, &tags, &r.ExchangeCount, &r.StartTime,
			&r.DurationMinutes, &r.HasCompaction, &snippet,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning result: %w", err)
		}
		r.Title = title.String
		r.TitleDisplay = titleDisplay.String
		r.Client = client.String
		r.Tags = tags.String
		r.Snippet = snippet.String
		r.Topics = getTopics(db, r.SessionID)
		results = append(results, r)
	}
	return results, rows.Err()
}

func scanFindResults(db *sql.DB, rows *sql.Rows) ([]Result, error) {
	var results []Result
	for rows.Next() {
		var r Result
		var title, titleDisplay, client, tags sql.NullString
		err := rows.Scan(
			&r.SessionID, &r.ProjectName, &title, &titleDisplay,
			&client, &tags, &r.ExchangeCount, &r.StartTime,
			&r.DurationMinutes, &r.HasCompaction,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning result: %w", err)
		}
		r.Title = title.String
		r.TitleDisplay = titleDisplay.String
		r.Client = client.String
		r.Tags = tags.String
		r.Topics = getTopics(db, r.SessionID)
		results = append(results, r)
	}
	return results, rows.Err()
}

func getTopics(db *sql.DB, sessionID string) []Topic {
	rows, err := db.Query(`
		SELECT topic, source FROM session_topics
		WHERE session_id = ? ORDER BY captured_at LIMIT 10
	`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck

	var topics []Topic
	for rows.Next() {
		var t Topic
		if rows.Scan(&t.TopicText, &t.Source) == nil {
			topics = append(topics, t)
		}
	}
	return topics
}

func toolsByName(db *sql.DB, toolName string, limit int) ([]ToolResult, error) {
	rows, err := db.Query(`
		SELECT s.session_id, s.title, s.title_display, s.start_time,
		       st.tool_name, st.use_count
		FROM session_tools st
		JOIN sessions s ON s.session_id = st.session_id
		WHERE st.tool_name LIKE ?
		ORDER BY st.use_count DESC
		LIMIT ?
	`, "%"+toolName+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("querying tool usage: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var results []ToolResult
	for rows.Next() {
		var r ToolResult
		var title, titleDisplay sql.NullString
		if err := rows.Scan(&r.SessionID, &title, &titleDisplay, &r.StartTime, &r.ToolName, &r.UseCount); err != nil {
			return nil, fmt.Errorf("scanning tool result: %w", err)
		}
		r.Title = title.String
		r.TitleDisplay = titleDisplay.String
		results = append(results, r)
	}
	return results, rows.Err()
}

func topTools(db *sql.DB, limit int) ([]ToolResult, error) {
	rows, err := db.Query(`
		SELECT tool_name, SUM(use_count) as total,
		       COUNT(DISTINCT session_id) as session_count
		FROM session_tools
		GROUP BY tool_name
		ORDER BY total DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying top tools: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var results []ToolResult
	for rows.Next() {
		var r ToolResult
		if err := rows.Scan(&r.ToolName, &r.Total, &r.SessionCount); err != nil {
			return nil, fmt.Errorf("scanning tool result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
