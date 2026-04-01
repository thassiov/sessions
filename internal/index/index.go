package index

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/thassiov/sessions/internal/config"
	"github.com/thassiov/sessions/internal/db"
)

// Stats holds indexing statistics.
type Stats struct {
	Total     int
	Indexed   int
	Skipped   int
	Errors    int
	Unchanged int
}

// Backfill indexes all existing session files.
func Backfill(database *db.DB, cfg *config.Config, logger *slog.Logger) Stats {
	return indexAll(database, cfg, logger, false)
}

// Incremental indexes only new or modified session files.
func Incremental(database *db.DB, cfg *config.Config, logger *slog.Logger) Stats {
	return indexAll(database, cfg, logger, true)
}

// IndexOne indexes a single session by ID, searching across all project directories.
func IndexOne(database *db.DB, cfg *config.Config, sessionID string) error {
	path, err := findSessionFile(cfg.ProjectsDir, sessionID)
	if err != nil {
		return err
	}

	projectDir := filepath.Base(filepath.Dir(path))
	projectName := cfg.ProjectName(projectDir)

	data, err := ParseSession(path, projectName)
	if err != nil {
		return fmt.Errorf("parsing session %s: %w", sessionID, err)
	}

	return database.UpsertSession(data)
}

func indexAll(database *db.DB, cfg *config.Config, logger *slog.Logger, incrementalOnly bool) Stats {
	var stats Stats

	files := discoverSessionFiles(cfg.ProjectsDir)
	stats.Total = len(files)
	logger.Info("discovered session files", "count", stats.Total)

	for _, path := range files {
		sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

		// Check if file has changed.
		hash, err := FileHash(path)
		if err != nil {
			logger.Warn("hashing failed", "session", sessionID, "error", err)
			stats.Errors++
			continue
		}

		if existingHash, ok := getExistingHash(database, sessionID); ok && existingHash == hash {
			if incrementalOnly {
				stats.Unchanged++
			} else {
				stats.Skipped++
			}
			continue
		}

		projectDir := filepath.Base(filepath.Dir(path))
		projectName := cfg.ProjectName(projectDir)

		data, err := ParseSession(path, projectName)
		if err != nil {
			logger.Warn("parse failed", "session", sessionID, "error", err)
			stats.Errors++
			continue
		}

		// Detect client.
		data.Client = detectClient(cfg, data)

		if err := database.UpsertSession(data); err != nil {
			logger.Warn("upsert failed", "session", sessionID, "error", err)
			stats.Errors++
			continue
		}

		stats.Indexed++
	}

	return stats
}

func discoverSessionFiles(projectsDir string) []string {
	var files []string
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, entry.Name())
		jsonls, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if err != nil {
			continue
		}
		files = append(files, jsonls...)
	}
	return files
}

func findSessionFile(projectsDir, sessionID string) (string, error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", fmt.Errorf("reading projects dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("session file not found: %s", sessionID)
}

func getExistingHash(database *db.DB, sessionID string) (string, bool) {
	var hash string
	err := database.SQL().QueryRow(
		"SELECT file_hash FROM sessions WHERE session_id=?", sessionID,
	).Scan(&hash)
	if err != nil {
		return "", false
	}
	return hash, true
}

func detectClient(cfg *config.Config, data *db.SessionData) string {
	if len(cfg.Clients) == 0 {
		return ""
	}

	// Check user prompts.
	allText := strings.ToLower(data.FTSContent)
	for _, c := range cfg.Clients {
		if strings.Contains(allText, strings.ToLower(c)) {
			return c
		}
	}

	// Check project name.
	for _, c := range cfg.Clients {
		if strings.Contains(strings.ToLower(data.ProjectName), strings.ToLower(c)) {
			return c
		}
	}

	return ""
}
