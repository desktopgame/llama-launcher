package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Profile represents a combination of model + runtime + launch parameters.
type Profile struct {
	Name           string `json:"name"`
	ModelPath      string `json:"model_path"`
	RuntimeDirName string `json:"runtime_dir_name"`
	ContextSize    int    `json:"context_size,omitempty"`
	GPULayers      int    `json:"gpu_layers,omitempty"`
	FlashAttention bool   `json:"flash_attention,omitempty"`
	NoMmap         bool   `json:"no_mmap,omitempty"`
	MMProjPath     string `json:"mmproj_path,omitempty"`
	ExtraArgs      string `json:"extra_args,omitempty"`
}

// BuildArgs returns the command-line arguments for llama-server.
func (p *Profile) BuildArgs(port int) []string {
	args := []string{
		"-m", p.ModelPath,
		"--port", fmt.Sprintf("%d", port),
	}
	if p.ContextSize > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", p.ContextSize))
	}
	if p.GPULayers > 0 {
		args = append(args, "-ngl", fmt.Sprintf("%d", p.GPULayers))
	}
	if p.FlashAttention {
		args = append(args, "-fa")
	}
	if p.NoMmap {
		args = append(args, "--no-mmap")
	}
	if p.MMProjPath != "" {
		args = append(args, "--mmproj", p.MMProjPath)
	}
	if p.ExtraArgs != "" {
		for _, arg := range strings.Fields(p.ExtraArgs) {
			args = append(args, arg)
		}
	}
	return args
}

// Manager handles profile storage.
type Manager struct {
	dir string
}

// NewManager creates a profile manager that stores profiles in dir.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// Save writes a profile to disk.
func (m *Manager) Save(p *Profile) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir, p.Name+".json")
	return os.WriteFile(path, data, 0o644)
}

// Load reads a profile by name.
func (m *Manager) Load(name string) (*Profile, error) {
	path := filepath.Join(m.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// List returns all saved profiles.
func (m *Manager) List() ([]*Profile, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var profiles []*Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		p, err := m.Load(name)
		if err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// Remove deletes a profile by name.
func (m *Manager) Remove(name string) error {
	path := filepath.Join(m.dir, name+".json")
	return os.Remove(path)
}
