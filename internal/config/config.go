package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds the application settings.
type Config struct {
	ModelDirs       []string `json:"model_dirs"`        // user-defined model storage directories (recursive scan)
	LMStudioDir     string   `json:"lmstudio_dir"`      // LM Studio models dir (publisher/model-name layout)
	RuntimeDir      string   `json:"runtime_dir"`       // where llama.cpp runtimes are stored
	ProfileDir      string   `json:"profile_dir"`       // where profiles are stored
	WorkspaceDir    string   `json:"workspace_dir"`     // where workspaces are stored
	DefaultBackend  string   `json:"default_backend"`   // preferred backend: vulkan, cuda, rocm, cpu
	Port            int      `json:"port"`              // llama-server port (shared across all profiles)
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "llama-launcher", "config.json")
}

// Load reads config from path. Returns defaults if file doesn't exist.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	fillDefaults(&cfg)
	return &cfg, nil
}

// Save writes config to path.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// EnsureExists writes the config to path if the file doesn't already exist.
func EnsureExists(path string, cfg *Config) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return Save(path, cfg)
}

func fillDefaults(cfg *Config) {
	d := defaults()
	if cfg.RuntimeDir == "" {
		cfg.RuntimeDir = d.RuntimeDir
	}
	if cfg.ProfileDir == "" {
		cfg.ProfileDir = d.ProfileDir
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = d.WorkspaceDir
	}
	if cfg.DefaultBackend == "" {
		cfg.DefaultBackend = d.DefaultBackend
	}
	if cfg.Port == 0 {
		cfg.Port = d.Port
	}
	if len(cfg.ModelDirs) == 0 {
		cfg.ModelDirs = d.ModelDirs
	}
}

func defaults() *Config {
	dir, _ := os.UserConfigDir()
	return &Config{
		ModelDirs:      []string{filepath.Join(dir, "llama-launcher", "models")},
		RuntimeDir:     filepath.Join(dir, "llama-launcher", "runtimes"),
		ProfileDir:     filepath.Join(dir, "llama-launcher", "profiles"),
		WorkspaceDir:   filepath.Join(dir, "llama-launcher", "workspaces"),
		DefaultBackend: "vulkan",
		Port:           8080,
	}
}
