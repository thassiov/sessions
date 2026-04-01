package analyze

import (
	"fmt"
	"math"
	"strings"
)

// FormatContext formats a context result for CLI output.
func FormatContext(r *ContextResult) string {
	lines := make([]string, 0, 16+len(r.Exchanges)*8)

	title := r.Title
	if title == "" {
		title = "(unnamed)"
	}

	// Header.
	border := 44 - len(title)
	if border < 1 {
		border = 1
	}
	lines = append(lines, "",
		fmt.Sprintf("+--- %s %s", title, strings.Repeat("-", border)))
	var meta []string
	if r.StartTime != "" && len(r.StartTime) >= 10 {
		meta = append(meta, r.StartTime[:10])
	}
	if r.ProjectName != "" {
		meta = append(meta, r.ProjectName)
	}
	if r.ExchangeCount > 0 {
		meta = append(meta, fmt.Sprintf("%d exchanges", r.ExchangeCount))
	}
	if r.DurationMinutes > 0 {
		meta = append(meta, fmt.Sprintf("%dmin", r.DurationMinutes))
	}
	if len(meta) > 0 {
		lines = append(lines, fmt.Sprintf("| %s", strings.Join(meta, " | ")))
	}
	lines = append(lines,
		fmt.Sprintf("| -> claude --resume %s", r.SessionID),
		"+"+strings.Repeat("-", 48))

	if r.Query != "" {
		lines = append(lines, fmt.Sprintf("\nMatching exchanges for %q:", r.Query))
	} else {
		lines = append(lines, fmt.Sprintf("\nAll exchanges (%d shown):", len(r.Exchanges)))
	}
	lines = append(lines, "")

	for _, ex := range r.Exchanges {
		ts := ""
		if len(ex.Timestamp) >= 16 {
			ts = ex.Timestamp[:16]
		}
		border := 40 - len(ts)
		if border < 1 {
			border = 1
		}
		lines = append(lines,
			fmt.Sprintf("  +- %s %s", ts, strings.Repeat("-", border)),
			"  |")

		// User message.
		for i, ul := range strings.Split(ex.User, "\n") {
			if i == 0 {
				lines = append(lines, fmt.Sprintf("  |  [user] %s", ul))
			} else {
				lines = append(lines, fmt.Sprintf("  |         %s", ul))
			}
		}
		lines = append(lines, "  |")

		// Assistant message.
		for i, al := range strings.Split(ex.Assistant, "\n") {
			if i == 0 {
				lines = append(lines, fmt.Sprintf("  |  [asst] %s", al))
			} else {
				lines = append(lines, fmt.Sprintf("  |         %s", al))
			}
		}
		lines = append(lines, "  |",
			"  +"+strings.Repeat("-", 44),
			"")
	}

	return strings.Join(lines, "\n")
}

// FormatAnalytics formats analytics results for CLI output.
func FormatAnalytics(r *AnalyticsResult) string {
	lines := make([]string, 0, 32)

	lines = append(lines, "",
		fmt.Sprintf("Session analytics -- %s", r.Period),
		strings.Repeat("=", 50))

	// Overview.
	ov := r.Overview
	hours := math.Round(float64(ov.TotalMinutes)/60*10) / 10
	lines = append(lines, fmt.Sprintf("\n  %d sessions | %.1fh total | avg %.0fmin/session | avg %.0f exchanges",
		ov.TotalSessions, hours, ov.AvgDuration, ov.AvgExchanges))

	// Time per client.
	if len(r.TimePerClient) > 0 {
		lines = append(lines, "\n  Time per client",
			"  "+strings.Repeat("-", 46))
		for _, c := range r.TimePerClient {
			hrs := math.Round(float64(c.TotalMinutes)/60*10) / 10
			lines = append(lines, fmt.Sprintf("  %-25s  %4d sessions  %6.1fh  avg %.0f exchanges",
				c.Client, c.Sessions, hrs, c.AvgExchanges))
		}
	}

	// By project.
	if len(r.ByProject) > 0 {
		lines = append(lines, "\n  By project",
			"  "+strings.Repeat("-", 46))
		for _, p := range r.ByProject {
			hrs := math.Round(float64(p.TotalMinutes)/60*10) / 10
			lines = append(lines, fmt.Sprintf("  %-25s  %4d sessions  %6.1fh", p.ProjectName, p.Sessions, hrs))
		}
	}

	// Daily trend.
	if len(r.DailyTrend) > 0 {
		lines = append(lines, "\n  Daily trend (last 14 days)",
			"  "+strings.Repeat("-", 46))
		for _, d := range r.DailyTrend {
			hrs := math.Round(float64(d.Minutes)/60*10) / 10
			barLen := d.Minutes / 15
			if barLen > 40 {
				barLen = 40
			}
			bar := strings.Repeat("#", barLen)
			lines = append(lines, fmt.Sprintf("  %s  %3d sessions  %5.1fh  %s", d.Day, d.Sessions, hrs, bar))
		}
	}

	// Top tools.
	if len(r.TopTools) > 0 {
		lines = append(lines, "\n  Top tools",
			"  "+strings.Repeat("-", 46))
		for _, t := range r.TopTools {
			lines = append(lines, fmt.Sprintf("  %-25s  %6d uses  (%d sessions)", t.ToolName, t.Total, t.SessionCount))
		}
	}

	// Tool trends.
	if len(r.ToolTrends) > 0 {
		lines = append(lines, "\n  Tool trends (this week vs last)",
			"  "+strings.Repeat("-", 46))
		for _, t := range r.ToolTrends {
			var change string
			if t.LastWeek > 0 {
				pct := int(math.Round(float64(t.ThisWeek-t.LastWeek) / float64(t.LastWeek) * 100))
				if pct > 0 {
					change = fmt.Sprintf("^ %d%%", pct)
				} else if pct < 0 {
					change = fmt.Sprintf("v %d%%", -pct)
				} else {
					change = "="
				}
			} else if t.ThisWeek > 0 {
				change = "NEW"
			}
			lines = append(lines, fmt.Sprintf("  %-25s  %5d (was %5d)  %s", t.ToolName, t.ThisWeek, t.LastWeek, change))
		}
	}

	// Top topics.
	if len(r.TopTopics) > 0 {
		lines = append(lines, "\n  Most discussed topics",
			"  "+strings.Repeat("-", 46))
		limit := 10
		if len(r.TopTopics) < limit {
			limit = len(r.TopTopics)
		}
		for _, t := range r.TopTopics[:limit] {
			lines = append(lines, fmt.Sprintf("  %3dx  %s", t.Mentions, t.Topic))
		}
	}

	lines = append(lines, "")
	return strings.Join(lines, "\n")
}
