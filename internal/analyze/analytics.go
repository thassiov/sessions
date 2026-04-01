package analyze

import (
	"database/sql"
	"fmt"
	"time"
)

// AnalyticsOpts holds options for analytics queries.
type AnalyticsOpts struct {
	Client  string
	Project string
	Week    bool
	Month   bool
}

// AnalyticsResult holds the full analytics output.
type AnalyticsResult struct {
	Period        string
	Overview      Overview
	TimePerClient []ClientTime
	DailyTrend    []DailyEntry
	TopTools      []ToolEntry
	ToolTrends    []ToolTrend
	TopTopics     []TopicEntry
	ByProject     []ProjectEntry
}

// Overview holds summary statistics.
type Overview struct {
	TotalSessions     int
	TotalMinutes      int
	AvgDuration       float64
	AvgExchanges      float64
	CompactedSessions int
}

// ClientTime holds time-per-client data.
type ClientTime struct {
	Client       string
	Sessions     int
	TotalMinutes int
	AvgExchanges float64
}

// DailyEntry holds one day's activity.
type DailyEntry struct {
	Day      string
	Sessions int
	Minutes  int
}

// ToolEntry holds aggregated tool usage.
type ToolEntry struct {
	ToolName     string
	Total        int
	SessionCount int
}

// ToolTrend holds week-over-week tool comparison.
type ToolTrend struct {
	ToolName string
	ThisWeek int
	LastWeek int
}

// TopicEntry holds topic mention count.
type TopicEntry struct {
	Topic    string
	Mentions int
}

// ProjectEntry holds per-project stats.
type ProjectEntry struct {
	ProjectName  string
	Sessions     int
	TotalMinutes int
}

// Analytics runs aggregation queries against the session index.
func Analytics(db *sql.DB, opts AnalyticsOpts) (*AnalyticsResult, error) {
	result := &AnalyticsResult{}

	// Period filter.
	var periodClause string
	var periodParams []interface{}

	if opts.Week {
		result.Period = "this week"
		periodClause = "AND s.start_time >= ?"
		periodParams = []interface{}{time.Now().AddDate(0, 0, -7).Format(time.RFC3339)}
	} else if opts.Month {
		result.Period = "this month"
		periodClause = "AND s.start_time >= ?"
		periodParams = []interface{}{time.Now().AddDate(0, 0, -30).Format(time.RFC3339)}
	} else {
		result.Period = "all time"
	}

	// Client/project filters.
	var clientClause string
	var clientParams []interface{}
	if opts.Client != "" {
		clientClause = "AND s.client LIKE ?"
		clientParams = []interface{}{"%" + opts.Client + "%"}
	}

	var projectClause string
	var projectParams []interface{}
	if opts.Project != "" {
		projectClause = "AND (s.project_name LIKE ? OR s.project LIKE ?)"
		projectParams = []interface{}{"%" + opts.Project + "%", "%" + opts.Project + "%"}
	}

	baseWhere := fmt.Sprintf("WHERE 1=1 %s %s %s", periodClause, clientClause, projectClause)
	baseParams := append(append(periodParams, clientParams...), projectParams...)

	// Overview.
	result.Overview = queryOverview(db, baseWhere, baseParams)

	// Time per client.
	result.TimePerClient = queryTimePerClient(db, baseWhere, baseParams)

	// Daily trend (last 14 days, ignores period filter).
	result.DailyTrend = queryDailyTrend(db)

	// Top tools.
	result.TopTools = queryTopTools(db, baseWhere, baseParams)

	// Tool trends.
	result.ToolTrends = queryToolTrends(db)

	// Top topics.
	result.TopTopics = queryTopTopics(db, baseWhere, baseParams)

	// By project.
	result.ByProject = queryByProject(db, baseWhere, baseParams)

	return result, nil
}

func queryOverview(db *sql.DB, where string, params []interface{}) Overview {
	var ov Overview
	var totalMin, avgDur, avgEx sql.NullFloat64
	db.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(duration_minutes),0),
		       ROUND(AVG(duration_minutes),1), ROUND(AVG(exchange_count),1),
		       SUM(CASE WHEN has_compaction=1 THEN 1 ELSE 0 END)
		FROM sessions s %s
	`, where), params...).Scan(&ov.TotalSessions, &totalMin, &avgDur, &avgEx, &ov.CompactedSessions) //nolint:errcheck // best-effort stats
	ov.TotalMinutes = int(totalMin.Float64)
	ov.AvgDuration = avgDur.Float64
	ov.AvgExchanges = avgEx.Float64
	return ov
}

func queryTimePerClient(db *sql.DB, where string, params []interface{}) []ClientTime {
	rows, err := db.Query(fmt.Sprintf(`
		SELECT s.client, COUNT(*), COALESCE(SUM(s.duration_minutes),0),
		       ROUND(AVG(s.exchange_count),1)
		FROM sessions s %s AND s.client IS NOT NULL AND s.client != ''
		GROUP BY s.client ORDER BY SUM(s.duration_minutes) DESC
	`, where), params...)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck // rows closed on function return

	var result []ClientTime
	for rows.Next() {
		var c ClientTime
		var avgEx sql.NullFloat64
		if rows.Scan(&c.Client, &c.Sessions, &c.TotalMinutes, &avgEx) == nil {
			c.AvgExchanges = avgEx.Float64
			result = append(result, c)
		}
	}
	_ = rows.Err() // best-effort stats query
	return result
}

func queryDailyTrend(db *sql.DB) []DailyEntry {
	rows, err := db.Query(`
		SELECT date(start_time) as day, COUNT(*), COALESCE(SUM(duration_minutes),0)
		FROM sessions WHERE start_time >= date('now', '-14 days')
		GROUP BY day ORDER BY day
	`)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck // rows closed on function return

	var result []DailyEntry
	for rows.Next() {
		var d DailyEntry
		if rows.Scan(&d.Day, &d.Sessions, &d.Minutes) == nil {
			result = append(result, d)
		}
	}
	_ = rows.Err() // best-effort stats query
	return result
}

func queryTopTools(db *sql.DB, where string, params []interface{}) []ToolEntry {
	rows, err := db.Query(fmt.Sprintf(`
		SELECT st.tool_name, SUM(st.use_count), COUNT(DISTINCT st.session_id)
		FROM session_tools st
		JOIN sessions s ON s.session_id = st.session_id
		%s
		GROUP BY st.tool_name ORDER BY SUM(st.use_count) DESC LIMIT 15
	`, where), params...)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck // rows closed on function return

	var result []ToolEntry
	for rows.Next() {
		var t ToolEntry
		if rows.Scan(&t.ToolName, &t.Total, &t.SessionCount) == nil {
			result = append(result, t)
		}
	}
	_ = rows.Err() // best-effort stats query
	return result
}

func queryToolTrends(db *sql.DB) []ToolTrend {
	thisWeek := time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
	lastWeek := time.Now().AddDate(0, 0, -14).Format(time.RFC3339)

	rows, err := db.Query(`
		SELECT tool_name,
		       SUM(CASE WHEN s.start_time >= ? THEN use_count ELSE 0 END),
		       SUM(CASE WHEN s.start_time >= ? AND s.start_time < ? THEN use_count ELSE 0 END)
		FROM session_tools st
		JOIN sessions s ON s.session_id = st.session_id
		WHERE s.start_time >= ?
		GROUP BY tool_name
		HAVING SUM(CASE WHEN s.start_time >= ? THEN use_count ELSE 0 END) > 0
		    OR SUM(CASE WHEN s.start_time >= ? AND s.start_time < ? THEN use_count ELSE 0 END) > 0
		ORDER BY SUM(CASE WHEN s.start_time >= ? THEN use_count ELSE 0 END) DESC
		LIMIT 15
	`, thisWeek, lastWeek, thisWeek, lastWeek, thisWeek, lastWeek, thisWeek, thisWeek)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck // rows closed on function return

	var result []ToolTrend
	for rows.Next() {
		var t ToolTrend
		if rows.Scan(&t.ToolName, &t.ThisWeek, &t.LastWeek) == nil {
			result = append(result, t)
		}
	}
	_ = rows.Err() // best-effort stats query
	return result
}

func queryTopTopics(db *sql.DB, where string, params []interface{}) []TopicEntry {
	rows, err := db.Query(fmt.Sprintf(`
		SELECT st.topic, COUNT(*)
		FROM session_topics st
		JOIN sessions s ON s.session_id = st.session_id
		%s
		GROUP BY st.topic ORDER BY COUNT(*) DESC LIMIT 20
	`, where), params...)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck // rows closed on function return

	var result []TopicEntry
	for rows.Next() {
		var t TopicEntry
		if rows.Scan(&t.Topic, &t.Mentions) == nil {
			result = append(result, t)
		}
	}
	_ = rows.Err() // best-effort stats query
	return result
}

func queryByProject(db *sql.DB, where string, params []interface{}) []ProjectEntry {
	rows, err := db.Query(fmt.Sprintf(`
		SELECT s.project_name, COUNT(*), COALESCE(SUM(s.duration_minutes),0)
		FROM sessions s %s
		GROUP BY s.project_name ORDER BY COUNT(*) DESC
	`, where), params...)
	if err != nil {
		return nil
	}
	defer rows.Close() //nolint:errcheck // rows closed on function return

	var result []ProjectEntry
	for rows.Next() {
		var p ProjectEntry
		if rows.Scan(&p.ProjectName, &p.Sessions, &p.TotalMinutes) == nil {
			result = append(result, p)
		}
	}
	_ = rows.Err() // best-effort stats query
	return result
}
