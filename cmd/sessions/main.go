// Package main provides the sessions CLI for searching and indexing Claude Code sessions.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thassiov/sessions/internal/analyze"
	"github.com/thassiov/sessions/internal/config"
	"github.com/thassiov/sessions/internal/db"
	"github.com/thassiov/sessions/internal/hook"
	"github.com/thassiov/sessions/internal/index"
	"github.com/thassiov/sessions/internal/search"
	"github.com/thassiov/sessions/internal/watch"
)

var (
	// Version is set at build time via ldflags.
	Version = "dev"
	// BuildTime is set at build time via ldflags.
	BuildTime = "unknown"
)

func main() {
	// Smart default: if first arg is not a known subcommand or flag, treat it as a search query.
	if len(os.Args) > 1 {
		first := os.Args[1]
		if !strings.HasPrefix(first, "-") && !isSubcommand(first) {
			// Inject "search" before the bare query.
			os.Args = append([]string{os.Args[0], "search"}, os.Args[1:]...)
		}
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

var subcommands = []string{
	"search", "find", "recent", "tools", "topics",
	"stats", "index", "version", "help", "completion",
	"context", "analytics", "hook", "watch",
}

func isSubcommand(arg string) bool {
	for _, cmd := range subcommands {
		if arg == cmd {
			return true
		}
	}
	return false
}

func run() error {
	var dbPath string

	rootCmd := &cobra.Command{
		Use:           "sessions",
		Short:         "Search and index Claude Code sessions",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVar(&dbPath, "db-path", "", "override database path")

	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newSearchCmd(&dbPath))
	rootCmd.AddCommand(newFindCmd(&dbPath))
	rootCmd.AddCommand(newRecentCmd(&dbPath))
	rootCmd.AddCommand(newToolsCmd(&dbPath))
	rootCmd.AddCommand(newTopicsCmd(&dbPath))
	rootCmd.AddCommand(newStatsCmd(&dbPath))
	rootCmd.AddCommand(newContextCmd(&dbPath))
	rootCmd.AddCommand(newAnalyticsCmd(&dbPath))
	rootCmd.AddCommand(newIndexCmd(&dbPath))
	rootCmd.AddCommand(newHookCmd(&dbPath))
	rootCmd.AddCommand(newWatchCmd(&dbPath))

	return rootCmd.Execute()
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("sessions version %s (built %s)\n", Version, BuildTime)
		},
	}
}

func newSearchCmd(dbPath *string) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Full-text search across all sessions",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			if err := ensureIndexed(database, dbPath); err != nil {
				return err
			}

			results, err := search.Search(database.SQL(), query, limit)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Printf("No results for: %s\n", query)
				return nil
			}
			fmt.Printf("\n%d results for %q\n\n", len(results), query)
			for i := range results {
				fmt.Println(search.FormatResult(results[i]))
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "max results")
	return cmd
}

func newFindCmd(dbPath *string) *cobra.Command {
	var opts search.FilterOpts
	var compacted bool

	cmd := &cobra.Command{
		Use:   "find",
		Short: "Filter sessions by criteria",
		RunE: func(_ *cobra.Command, _ []string) error {
			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			if err := ensureIndexed(database, dbPath); err != nil {
				return err
			}

			if compacted {
				t := true
				opts.HasCompaction = &t
			}

			results, err := search.Find(database.SQL(), opts)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("No sessions match those filters.")
				return nil
			}
			fmt.Printf("\n%d sessions\n\n", len(results))
			for i := range results {
				fmt.Println(search.FormatResult(results[i]))
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Client, "client", "", "filter by client name")
	cmd.Flags().StringVar(&opts.Tag, "tag", "", "filter by tag")
	cmd.Flags().StringVar(&opts.Tool, "tool", "", "filter by tool used")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "filter by agent used")
	cmd.Flags().StringVar(&opts.Date, "date", "", "filter by date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&opts.Week, "week", false, "last 7 days")
	cmd.Flags().IntVar(&opts.Days, "days", 0, "last N days")
	cmd.Flags().StringVar(&opts.Project, "project", "", "filter by project")
	cmd.Flags().StringVar(&opts.ExcludeProject, "exclude-project", "", "exclude project")
	cmd.Flags().BoolVar(&compacted, "compacted", false, "only compacted sessions")
	cmd.Flags().IntVarP(&opts.Limit, "limit", "n", 20, "max results")
	return cmd
}

func newRecentCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "recent [N]",
		Short: "Show recent sessions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			n := 10
			if len(args) > 0 {
				var err error
				n, err = strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid number: %s", args[0])
				}
			}

			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			if err := ensureIndexed(database, dbPath); err != nil {
				return err
			}

			results, err := search.Recent(database.SQL(), n)
			if err != nil {
				return err
			}
			fmt.Printf("\nLast %d sessions\n\n", len(results))
			for i := range results {
				fmt.Println(search.FormatResult(results[i]))
				fmt.Println()
			}
			return nil
		},
	}
}

func newToolsCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "tools [name]",
		Short: "Show tool usage across sessions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			toolName := ""
			if len(args) > 0 {
				toolName = args[0]
			}

			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			results, err := search.ToolsUsage(database.SQL(), toolName, 20)
			if err != nil {
				return err
			}
			fmt.Print(search.FormatToolsUsage(results, toolName))
			return nil
		},
	}
}

func newTopicsCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "topics [session_id]",
		Short: "Show topic timeline for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			sessionID := args[0]
			topics, err := search.Topics(database.SQL(), sessionID)
			if err != nil {
				return err
			}
			if len(topics) == 0 {
				sid := sessionID
				if len(sid) > 8 {
					sid = sid[:8]
				}
				fmt.Printf("No topics recorded for session %s\n", sid)
				return nil
			}

			// Get session info for header.
			var title, startTime, projectName string
			database.SQL().QueryRow(
				"SELECT COALESCE(title_display,''), COALESCE(start_time,''), COALESCE(project_name,'') FROM sessions WHERE session_id LIKE ?",
				sessionID+"%",
			).Scan(&title, &startTime, &projectName) //nolint:errcheck // best-effort header info

			fmt.Print(search.FormatTopicTimeline(sessionID, title, startTime, projectName, topics))
			return nil
		},
	}
}

func newStatsCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show database statistics",
		RunE: func(_ *cobra.Command, _ []string) error {
			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			stats, err := search.GetStats(database.SQL())
			if err != nil {
				return err
			}
			fmt.Print(search.FormatStats(stats))
			return nil
		},
	}
}

func newIndexCmd(dbPath *string) *cobra.Command {
	var backfill bool
	var sessionID string

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index session files",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(dbPath)
			if err != nil {
				return err
			}

			database, err := db.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			if sessionID != "" {
				if err := index.One(database, cfg, sessionID); err != nil {
					return err
				}
				fmt.Printf("Indexed: %s\n", sessionID)
				return nil
			}

			var stats index.Stats
			if backfill {
				stats = index.Backfill(database, cfg, logger)
			} else {
				stats = index.Incremental(database, cfg, logger)
			}

			fmt.Printf("Indexed: %d, Skipped: %d, Errors: %d (of %d total)\n",
				stats.Indexed, stats.Skipped+stats.Unchanged, stats.Errors, stats.Total)
			return nil
		},
	}
	cmd.Flags().BoolVar(&backfill, "backfill", false, "index all sessions (not just new/modified)")
	cmd.Flags().StringVar(&sessionID, "session", "", "index a single session by ID")
	return cmd
}

func newHookCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "hook [event]",
		Short: "Handle Claude Code hook events (UserPromptSubmit, PreCompact, SessionEnd)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			event := args[0]

			// Read session ID from stdin (Claude hooks pass JSON).
			stdinData := hook.ReadStdin(os.Stdin)
			sessionID := stdinData.SessionID
			if sessionID == "" {
				sessionID = os.Getenv("CLAUDE_SESSION_ID")
			}
			if sessionID == "" {
				return nil // No session ID — silently exit.
			}

			cfg, err := loadConfig(dbPath)
			if err != nil {
				return err
			}

			database, err := db.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			hook.Handle(event, sessionID, database, cfg, logger)
			return nil
		},
	}
}

func newWatchCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Watch session files and index on change",
		Long:  "Monitors ~/.claude/projects/ for JSONL file changes and automatically indexes sessions + captures topics.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(dbPath)
			if err != nil {
				return err
			}

			database, err := db.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			// Catch up on anything missed while not running.
			logger.Info("running incremental index on startup")
			stats := index.Incremental(database, cfg, logger)
			if stats.Indexed > 0 {
				logger.Info("catch-up complete", "indexed", stats.Indexed)
			}

			w := watch.New(database, cfg, logger)
			return w.Run(cmd.Context())
		},
	}
}

func newContextCmd(dbPath *string) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "context [session_id] [query]",
		Short: "Show conversation exchanges from a session",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			sessionID := args[0]
			query := ""
			if len(args) > 1 {
				query = args[1]
			}

			result, err := analyze.GetContext(database.SQL(), sessionID, query, limit)
			if err != nil {
				return err
			}
			fmt.Print(analyze.FormatContext(result))
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "max exchanges")
	return cmd
}

func newAnalyticsCmd(dbPath *string) *cobra.Command {
	var opts analyze.AnalyticsOpts

	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Show session analytics and trends",
		RunE: func(_ *cobra.Command, _ []string) error {
			database, err := openDB(dbPath)
			if err != nil {
				return err
			}
			defer database.Close() //nolint:errcheck // closed on command exit

			result, err := analyze.Analytics(database.SQL(), opts)
			if err != nil {
				return err
			}
			fmt.Print(analyze.FormatAnalytics(result))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Client, "client", "", "filter by client")
	cmd.Flags().StringVar(&opts.Project, "project", "", "filter by project")
	cmd.Flags().BoolVar(&opts.Week, "week", false, "this week only")
	cmd.Flags().BoolVar(&opts.Month, "month", false, "this month only")
	return cmd
}

func openDB(dbPath *string) (*db.DB, error) {
	cfg, err := loadConfig(dbPath)
	if err != nil {
		return nil, err
	}
	return db.Open(cfg.DBPath)
}

func loadConfig(dbPath *string) (*config.Config, error) {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		return nil, err
	}
	if dbPath != nil && *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	return cfg, nil
}

func ensureIndexed(database *db.DB, dbPath *string) error {
	empty, err := database.IsEmpty()
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}

	fmt.Println("\n  First run - indexing all your sessions...")
	fmt.Println("  (This only happens once.)")
	fmt.Println()

	cfg, err := loadConfig(dbPath)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	stats := index.Backfill(database, cfg, logger)
	fmt.Printf("  Indexed %d sessions (%d errors)\n\n", stats.Indexed, stats.Errors)
	return nil
}
