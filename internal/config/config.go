// Package config provides configuration resolution for sessions.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config holds the resolved configuration.
type Config struct {
	ProjectsDir  string            `json:"projects_dir"`
	DBPath       string            `json:"db_path"`
	Clients      []string          `json:"clients"`
	ProjectNames map[string]string `json:"project_names"`
}

// DefaultDBPath returns the default database path.
func DefaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".session-index", "sessions.db")
}

// DefaultProjectsDir returns the default Claude projects directory.
func DefaultProjectsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".session-index", "config.json")
}

// Load resolves configuration from file, environment, and defaults.
func Load(configPath string) (*Config, error) {
	cfg := &Config{
		ProjectsDir:  DefaultProjectsDir(),
		DBPath:       DefaultDBPath(),
		Clients:      []string{},
		ProjectNames: map[string]string{},
	}

	// Layer config file.
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	// Layer environment variables.
	if v := os.Getenv("SESSION_INDEX_PROJECTS"); v != "" {
		cfg.ProjectsDir = expandHome(v)
	}
	if v := os.Getenv("SESSION_INDEX_DB"); v != "" {
		cfg.DBPath = expandHome(v)
	}

	// Expand home in paths.
	cfg.ProjectsDir = expandHome(cfg.ProjectsDir)
	cfg.DBPath = expandHome(cfg.DBPath)

	return cfg, nil
}

// ProjectName returns the friendly name for a project directory.
func (c *Config) ProjectName(dirName string) string {
	if name, ok := c.ProjectNames[dirName]; ok {
		return name
	}
	return autoProjectName(dirName)
}

// autoProjectName generates a friendly name from a project directory name.
// Example: "-home-thassiov-projects-myapp" -> "projects myapp".
func autoProjectName(dirName string) string {
	parts := strings.Split(dirName, "-")
	// Filter empty parts.
	var clean []string
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) == 0 {
		return dirName
	}
	if len(clean) >= 2 {
		return strings.Join(clean[len(clean)-2:], " ")
	}
	return clean[0]
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
