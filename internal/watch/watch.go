// Package watch provides a file watcher daemon that monitors Claude Code
// session JSONL files and automatically indexes + captures topics.
package watch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/thassiov/sessions/internal/config"
	"github.com/thassiov/sessions/internal/db"
	"github.com/thassiov/sessions/internal/hook"
	"github.com/thassiov/sessions/internal/index"
)

// DefaultDebounce is the default debounce duration for file change events.
const DefaultDebounce = 2 * time.Second

// Watcher monitors session JSONL files and indexes them on change.
type Watcher struct {
	database *db.DB
	cfg      *config.Config
	logger   *slog.Logger
	debounce time.Duration
	pending  map[string]*time.Timer
	mu       sync.Mutex
}

// New creates a new file watcher.
func New(database *db.DB, cfg *config.Config, logger *slog.Logger) *Watcher {
	return &Watcher{
		database: database,
		cfg:      cfg,
		logger:   logger,
		debounce: DefaultDebounce,
		pending:  make(map[string]*time.Timer),
	}
}

// Run starts the file watcher and blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	// Watch projects dir and all existing subdirs.
	if err := w.addWatchDirs(fsw); err != nil {
		return err
	}

	w.logger.Info("watcher started", "dir", w.cfg.ProjectsDir)

	for {
		select {
		case <-ctx.Done():
			w.drainPending()
			w.logger.Info("watcher stopped")
			return nil

		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(fsw, event)

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("watcher error", "error", err)
		}
	}
}

func (w *Watcher) addWatchDirs(fsw *fsnotify.Watcher) error {
	projectsDir := w.cfg.ProjectsDir
	if err := fsw.Add(projectsDir); err != nil {
		return err
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dir := filepath.Join(projectsDir, entry.Name())
			if err := fsw.Add(dir); err != nil {
				w.logger.Warn("cannot watch dir", "dir", dir, "error", err)
			}
		}
	}
	return nil
}

func (w *Watcher) handleEvent(fsw *fsnotify.Watcher, event fsnotify.Event) {
	path := event.Name

	// New directory created → add to watch list.
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := fsw.Add(path); err == nil {
				w.logger.Info("watching new project dir", "dir", filepath.Base(path))
			}
			return
		}
	}

	// Only care about .jsonl files.
	if !strings.HasSuffix(path, ".jsonl") {
		return
	}

	// Skip subagent files.
	if strings.Contains(path, "subagents") {
		return
	}

	// Debounce: reset timer for this file.
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		w.scheduleIndex(path)
	}
}

func (w *Watcher) scheduleIndex(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if timer, exists := w.pending[path]; exists {
		timer.Reset(w.debounce)
		return
	}

	w.pending[path] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		w.indexSession(path)
	})
}

func (w *Watcher) indexSession(path string) {
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	projectDir := filepath.Base(filepath.Dir(path))
	projectName := w.cfg.ProjectName(projectDir)

	// Check if file has actually changed.
	newHash, err := index.FileHash(path)
	if err != nil {
		return
	}

	var existingHash string
	w.database.SQL().QueryRow(
		"SELECT file_hash FROM sessions WHERE session_id=?", sessionID,
	).Scan(&existingHash) //nolint:errcheck

	if existingHash == newHash {
		return // No change.
	}

	// Parse and index.
	data, err := index.ParseSession(path, projectName)
	if err != nil {
		w.logger.Warn("parse failed", "session", sessionID[:8], "error", err)
		return
	}

	if err := w.database.UpsertSession(data); err != nil {
		w.logger.Warn("index failed", "session", sessionID[:8], "error", err)
		return
	}

	w.logger.Info("indexed", "session", sessionID[:8], "exchanges", data.ExchangeCount)

	// Live topic capture.
	w.captureTopic(path, sessionID)
}

func (w *Watcher) captureTopic(path, sessionID string) {
	exchangeCount := hook.CountUserMessages(path)
	if exchangeCount < hook.TopicInterval {
		return
	}

	lastCapture := hook.GetLastCapture(w.database, sessionID)
	if exchangeCount-lastCapture < hook.TopicInterval {
		return
	}

	messages := hook.ExtractRecentUserMessages(path, 3)
	topic := hook.ExtractTopic(messages)
	if topic == "" {
		return
	}

	hook.WriteTopic(w.database, sessionID, topic, "watcher", exchangeCount, w.logger)
	hook.UpdateLastCapture(w.database, sessionID, exchangeCount, w.logger)
}

func (w *Watcher) drainPending() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for path, timer := range w.pending {
		timer.Stop()
		delete(w.pending, path)
		// Fire remaining pending indexes synchronously.
		w.indexSession(path)
	}
}
