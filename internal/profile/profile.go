package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModelType indicates the model's purpose.
type ModelType string

const (
	ModelTypeGeneration ModelType = "generation"
	ModelTypeEmbedding  ModelType = "embedding"
)

// LoadMode is llama-server's model loading strategy, passed via --load-mode.
// This replaces the now-deprecated --mmap/--no-mmap/--mlock/--direct-io flags,
// which a single bool can no longer represent (mmap+mlock has no bool equivalent).
type LoadMode string

const (
	LoadModeNone      LoadMode = "none"  // 旧 --no-mmap 相当
	LoadModeMmap      LoadMode = "mmap"  // 旧 --mmap 相当
	LoadModeMlock     LoadMode = "mlock" // 旧 --mlock 相当
	LoadModeMmapMlock LoadMode = "mmap+mlock"
	LoadModeDio       LoadMode = "dio" // 旧 --direct-io/--dio 相当
)

// Profile represents a combination of model + runtime + launch parameters.
type Profile struct {
	Name           string    `json:"name"`
	ModelPath      string    `json:"model_path"`
	RuntimeDirName string    `json:"runtime_dir_name"`
	ModelType      ModelType `json:"model_type"`
	ContextSize    int       `json:"context_size,omitempty"`
	// GPULayers は *int で保持する。llama-server の -ngl は現在デフォルトが
	// "auto"(GPUオフロードを試みる) であり、意図的な 0(CPU限定)と未設定を
	// 区別できないと、CPU限定のつもりが黙ってGPUを使ってしまう。
	GPULayers      *int `json:"gpu_layers,omitempty"`
	FlashAttention bool `json:"flash_attention,omitempty"`
	// LoadMode は未設定(空文字)なら --load-mode を渡さず、llama-server の
	// デフォルト(auto)に委ねる。
	LoadMode LoadMode `json:"load_mode,omitempty"`
	// Jinja は *bool で保持する。llama-server の --jinja は現在デフォルトで
	// 有効なので、「オフにしたい」という意図を表すには明示的に --no-jinja を
	// 渡す必要がある。nil = 未設定(デフォルトの有効に従う)。
	Jinja                  *bool             `json:"jinja,omitempty"`
	ReasoningBudget        *int              `json:"reasoning_budget,omitempty"` // nil = 未設定, 0 = 思考を即終了, -1 = 無制限
	ReasoningBudgetMessage string            `json:"reasoning_budget_message,omitempty"`
	MMProjPath             string            `json:"mmproj_path,omitempty"`
	ExtraArgs              string            `json:"extra_args,omitempty"`
	Env                    map[string]string `json:"env,omitempty"`
	// Cost はこのプロファイルを常駐させたときのメモリ消費の抽象量。
	// VRAM/RAM の境界が曖昧な環境でも使えるよう、単位を持たない整数にしている。
	// nil = 未設定。
	Cost *int `json:"cost,omitempty"`
	// MeasuredCost は --measure が書き戻す実測値。手で入れた Cost は決して上書きしない。
	MeasuredCost *int   `json:"measured_cost,omitempty"`
	MeasuredAt   string `json:"measured_at,omitempty"` // RFC3339
}

// EffectiveCost returns the cost the budget check should use: the hand-set
// value if there is one, otherwise the measured one. Returns nil when neither
// has been set.
func (p *Profile) EffectiveCost() *int {
	if p.Cost != nil {
		return p.Cost
	}
	return p.MeasuredCost
}

// EnvPairs returns the profile's environment variables as "KEY=VALUE" entries,
// sorted by key for deterministic output.
func (p *Profile) EnvPairs() []string {
	if len(p.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(p.Env))
	for k := range p.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+p.Env[k])
	}
	return pairs
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
	// nil = 未設定。0 は「GPUオフロードなし」という有効な値であり、
	// 省略すると llama-server のデフォルト(auto = GPUオフロードを試みる)になってしまう。
	if p.GPULayers != nil {
		args = append(args, "-ngl", fmt.Sprintf("%d", *p.GPULayers))
	}
	if p.FlashAttention && p.ModelType != ModelTypeEmbedding {
		args = append(args, "-fa", "on")
	}
	if p.ModelType == ModelTypeEmbedding {
		args = append(args, "--embedding")
	}
	if p.LoadMode != "" {
		args = append(args, "--load-mode", string(p.LoadMode))
	}
	// nil = 未設定(デフォルトの有効に従う)。false は明示的な無効化であり、
	// 省略するとデフォルト有効のままになって意図と逆転するため --no-jinja を渡す。
	if p.Jinja != nil {
		if *p.Jinja {
			args = append(args, "--jinja")
		} else {
			args = append(args, "--no-jinja")
		}
	}
	// 0 は「思考を即終了」という有効な値なので、未設定(nil)と区別する
	if p.ReasoningBudget != nil {
		args = append(args, "--reasoning-budget", fmt.Sprintf("%d", *p.ReasoningBudget))
	}
	if p.ReasoningBudgetMessage != "" {
		args = append(args, "--reasoning-budget-message", p.ReasoningBudgetMessage)
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

// Dir returns the profile storage directory.
func (m *Manager) Dir() string {
	return m.dir
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

// ListNames returns the names of all saved profiles, sorted, derived from the
// filenames rather than the JSON body so they always round-trip through Load.
func (m *Manager) ListNames() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names, nil
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

// SetRuntimeForAll overwrites every saved profile's RuntimeDirName with
// dirName and persists the change. It returns how many profiles were
// actually changed and how many profiles exist in total (profiles already
// pointing at dirName are left untouched but still counted in total).
func (m *Manager) SetRuntimeForAll(dirName string) (changed, total int, err error) {
	profiles, err := m.List()
	if err != nil {
		return 0, 0, err
	}
	total = len(profiles)
	for _, p := range profiles {
		if p.RuntimeDirName == dirName {
			continue
		}
		p.RuntimeDirName = dirName
		if err := m.Save(p); err != nil {
			return changed, total, err
		}
		changed++
	}
	return changed, total, nil
}
