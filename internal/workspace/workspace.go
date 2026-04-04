package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Entry represents one profile in a workspace with its loading behavior.
type Entry struct {
	ProfileName string `json:"profile_name"`
	Resident    bool   `json:"resident"` // true = won't be swapped out by on-demand models
	TTL         int    `json:"ttl"`      // seconds, 0 = never auto-unload
}

// Workspace represents a set of profiles to run together via llama-swap.
type Workspace struct {
	Name    string  `json:"name"`
	Entries []Entry `json:"entries"`
}

// Manager handles workspace storage.
type Manager struct {
	dir string
}

// NewManager creates a workspace manager that stores workspaces in dir.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// Dir returns the workspace storage directory.
func (m *Manager) Dir() string {
	return m.dir
}

// Save writes a workspace to disk.
func (m *Manager) Save(w *Workspace) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir, w.Name+".json")
	return os.WriteFile(path, data, 0o644)
}

// Load reads a workspace by name.
func (m *Manager) Load(name string) (*Workspace, error) {
	path := filepath.Join(m.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w Workspace
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// List returns all saved workspaces.
func (m *Manager) List() ([]*Workspace, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var workspaces []*Workspace
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		w, err := m.Load(name)
		if err != nil {
			continue
		}
		workspaces = append(workspaces, w)
	}
	return workspaces, nil
}

// Remove deletes a workspace by name.
func (m *Manager) Remove(name string) error {
	path := filepath.Join(m.dir, name+".json")
	return os.Remove(path)
}
