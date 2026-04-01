package search

import (
	"fmt"
	"strings"
)

// FormatResult formats a single search result for CLI output.
func FormatResult(r Result) string {
	lines := make([]string, 0, 8)

	// Title line.
	sid := r.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	title := r.TitleDisplay
	if title == "" {
		title = r.Title
	}
	if title == "" {
		title = "(unnamed)"
	}
	if len(title) > 70 {
		title = title[:67] + "..."
	}
	lines = append(lines, fmt.Sprintf("  * %s  %s", sid, title))

	// Metadata line.
	var meta []string
	if r.StartTime != "" && len(r.StartTime) >= 10 {
		meta = append(meta, r.StartTime[:10])
	}
	if r.ProjectName != "" {
		meta = append(meta, r.ProjectName)
	}
	if r.Client != "" {
		meta = append(meta, r.Client)
	}
	if r.ExchangeCount > 0 {
		meta = append(meta, fmt.Sprintf("%d exchanges", r.ExchangeCount))
	}
	if r.DurationMinutes > 0 {
		meta = append(meta, fmt.Sprintf("%dmin", r.DurationMinutes))
	}
	if r.HasCompaction == 1 {
		meta = append(meta, "compacted")
	}
	if len(meta) > 0 {
		lines = append(lines, fmt.Sprintf("    %s", strings.Join(meta, " | ")))
	}

	// Snippet.
	if r.Snippet != "" {
		snippet := strings.ReplaceAll(r.Snippet, ">>>", "")
		snippet = strings.ReplaceAll(snippet, "<<<", "")
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		if len(snippet) > 120 {
			snippet = snippet[:120]
		}
		lines = append(lines, fmt.Sprintf("    %q", strings.TrimSpace(snippet)))
	}

	// Topics.
	if len(r.Topics) > 0 {
		limit := 5
		if len(r.Topics) < limit {
			limit = len(r.Topics)
		}
		var topicStrs []string
		for _, t := range r.Topics[:limit] {
			topicStrs = append(topicStrs, t.TopicText)
		}
		lines = append(lines, fmt.Sprintf("    topics: %s", strings.Join(topicStrs, " -> ")))
	}

	// Tags.
	if r.Tags != "" {
		lines = append(lines, fmt.Sprintf("    [%s]", r.Tags))
	}

	// Resume command.
	lines = append(lines, fmt.Sprintf("    -> claude --resume %s", r.SessionID))

	return strings.Join(lines, "\n")
}

// FormatStats formats database statistics for CLI output.
func FormatStats(s *Stats) string {
	lines := make([]string, 0, 16)

	lines = append(lines, "",
		"Database overview",
		strings.Repeat("=", 40),
		fmt.Sprintf("  Sessions:  %d", s.TotalSessions),
		fmt.Sprintf("  Topics:    %d", s.TotalTopics),
		fmt.Sprintf("  Tools:     %d distinct", s.TotalTools),
		fmt.Sprintf("  Agents:    %d distinct", s.TotalAgents))
	if s.Earliest != "" {
		lines = append(lines, fmt.Sprintf("  Range:     %s -> %s", s.Earliest, s.Latest))
	}

	if len(s.ByProject) > 0 {
		lines = append(lines, "",
			"  By project",
			"  "+strings.Repeat("-", 36))
		for name, cnt := range s.ByProject {
			lines = append(lines, fmt.Sprintf("  %-25s  %5d", name, cnt))
		}
	}

	if len(s.TopTools) > 0 {
		lines = append(lines, "",
			"  Top tools",
			"  "+strings.Repeat("-", 36))
		for name, cnt := range s.TopTools {
			lines = append(lines, fmt.Sprintf("  %-25s  %5d", name, cnt))
		}
	}

	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

// FormatTopicTimeline formats a topic timeline for CLI output.
func FormatTopicTimeline(_, title, startTime, projectName string, topics []TopicDetail) string {
	lines := make([]string, 0, 8+len(topics)*3)

	// Header.
	if title == "" {
		title = "(unnamed)"
	}
	border := 44 - len(title)
	if border < 1 {
		border = 1
	}
	lines = append(lines, "",
		fmt.Sprintf("+--- %s %s", title, strings.Repeat("-", border)))
	var meta []string
	if startTime != "" && len(startTime) >= 16 {
		meta = append(meta, startTime[:16])
	}
	if projectName != "" {
		meta = append(meta, projectName)
	}
	if len(meta) > 0 {
		lines = append(lines, fmt.Sprintf("| %s", strings.Join(meta, " | ")))
	}
	lines = append(lines, "+"+strings.Repeat("-", 48),
		"",
		fmt.Sprintf("Topic timeline (%d entries)", len(topics)),
		"")

	for _, t := range topics {
		ts := ""
		if len(t.CapturedAt) >= 16 {
			ts = t.CapturedAt[:16]
		}
		ex := ""
		if t.ExchangeNumber != nil {
			ex = fmt.Sprintf(" (exchange %d)", *t.ExchangeNumber)
		}
		lines = append(lines,
			fmt.Sprintf("  [%-20s] %s%s", t.Source, ts, ex),
			fmt.Sprintf("                       %s", t.TopicText),
			"")
	}

	return strings.Join(lines, "\n")
}

// FormatToolsUsage formats tool usage results for CLI output.
func FormatToolsUsage(results []ToolResult, toolName string) string {
	lines := make([]string, 0, 4+len(results))

	if toolName != "" {
		lines = append(lines, "",
			fmt.Sprintf("Sessions using '%s'", toolName),
			"")
		for _, r := range results {
			lines = append(lines, formatToolByName(r))
		}
	} else {
		lines = append(lines, "",
			"Top tools across all sessions",
			"")
		for _, r := range results {
			lines = append(lines, fmt.Sprintf("  %-25s  %6d uses  (%d sessions)", r.ToolName, r.Total, r.SessionCount))
		}
	}

	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func formatToolByName(r ToolResult) string {
	sid := r.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	title := r.TitleDisplay
	if title == "" {
		title = r.Title
	}
	if title == "" {
		title = "(unnamed)"
	}
	return fmt.Sprintf("  * %s  %s x%d  %s", sid, r.ToolName, r.UseCount, title)
}
