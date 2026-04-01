package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	if got := DefaultDBPath(); got != filepath.Join(home, ".session-index", "sessions.db") {
		t.Errorf("DefaultDBPath() = %q, want suffix .session-index/sessions.db", got)
	}

	if got := DefaultProjectsDir(); got != filepath.Join(home, ".claude", "projects") {
		t.Errorf("DefaultProjectsDir() = %q, want suffix .claude/projects", got)
	}

	if got := DefaultConfigPath(); got != filepath.Join(home, ".session-index", "config.json") {
		t.Errorf("DefaultConfigPath() = %q, want suffix .session-index/config.json", got)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load("/nonexistent/config.json")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ProjectsDir == "" {
		t.Error("ProjectsDir should not be empty")
	}
	if cfg.DBPath == "" {
		t.Error("DBPath should not be empty")
	}
	if cfg.Clients == nil {
		t.Error("Clients should not be nil")
	}
	if cfg.ProjectNames == nil {
		t.Error("ProjectNames should not be nil")
	}
}

func TestLoadFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	err := os.WriteFile(cfgFile, []byte(`{
		"projects_dir": "/custom/projects",
		"db_path": "/custom/db.sqlite",
		"clients": ["acme", "initech"]
	}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ProjectsDir != "/custom/projects" {
		t.Errorf("ProjectsDir = %q, want /custom/projects", cfg.ProjectsDir)
	}
	if cfg.DBPath != "/custom/db.sqlite" {
		t.Errorf("DBPath = %q, want /custom/db.sqlite", cfg.DBPath)
	}
	if len(cfg.Clients) != 2 || cfg.Clients[0] != "acme" {
		t.Errorf("Clients = %v, want [acme initech]", cfg.Clients)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("SESSION_INDEX_PROJECTS", "/env/projects")
	t.Setenv("SESSION_INDEX_DB", "/env/db.sqlite")

	cfg, err := Load("/nonexistent/config.json")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ProjectsDir != "/env/projects" {
		t.Errorf("ProjectsDir = %q, want /env/projects", cfg.ProjectsDir)
	}
	if cfg.DBPath != "/env/db.sqlite" {
		t.Errorf("DBPath = %q, want /env/db.sqlite", cfg.DBPath)
	}
}

func TestProjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      *Config
		dirName  string
		expected string
	}{
		{
			name:     "configured name",
			cfg:      &Config{ProjectNames: map[string]string{"-home-user-myapp": "MyApp"}},
			dirName:  "-home-user-myapp",
			expected: "MyApp",
		},
		{
			name:     "auto-generated two segments",
			cfg:      &Config{ProjectNames: map[string]string{}},
			dirName:  "-home-user-projects-myapp",
			expected: "projects myapp",
		},
		{
			name:     "auto-generated single segment",
			cfg:      &Config{ProjectNames: map[string]string{}},
			dirName:  "-myapp",
			expected: "myapp",
		},
		{
			name:     "empty dir name",
			cfg:      &Config{ProjectNames: map[string]string{}},
			dirName:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.ProjectName(tt.dirName)
			if got != tt.expected {
				t.Errorf("ProjectName(%q) = %q, want %q", tt.dirName, got, tt.expected)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	t.Parallel()

	home, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.expected {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
