package profile

import (
	"path/filepath"
	"slices"
	"testing"
)

func intPtr(n int) *int    { return &n }
func boolPtr(b bool) *bool { return &b }

func TestBuildArgsReasoningBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget *int
		want   []string // "" means the flag must be absent
	}{
		{"unset omits the flag", nil, nil},
		// 0 は「思考を即終了」という有効な値。落とすと既定の -1(無制限)になり意図と逆になる
		{"zero is emitted", intPtr(0), []string{"--reasoning-budget", "0"}},
		{"explicit unrestricted", intPtr(-1), []string{"--reasoning-budget", "-1"}},
		{"positive budget", intPtr(1024), []string{"--reasoning-budget", "1024"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Profile{Name: "p", ModelPath: "m.gguf", ReasoningBudget: tt.budget}
			args := p.BuildArgs(8081)

			idx := slices.Index(args, "--reasoning-budget")
			if tt.want == nil {
				if idx >= 0 {
					t.Fatalf("expected no --reasoning-budget, got %v", args)
				}
				return
			}
			if idx < 0 || idx+1 >= len(args) {
				t.Fatalf("expected --reasoning-budget in %v", args)
			}
			if got := args[idx : idx+2]; !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildArgsGPULayers(t *testing.T) {
	tests := []struct {
		name   string
		layers *int
		want   []string // nil means the flag must be absent
	}{
		{"unset omits the flag (llama-server defaults to auto)", nil, nil},
		// 0 は「GPUオフロードなし」という有効な値。落とすとデフォルトの auto になり意図と逆になる
		{"zero is emitted", intPtr(0), []string{"-ngl", "0"}},
		{"positive value", intPtr(99), []string{"-ngl", "99"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Profile{Name: "p", ModelPath: "m.gguf", GPULayers: tt.layers}
			args := p.BuildArgs(8081)

			idx := slices.Index(args, "-ngl")
			if tt.want == nil {
				if idx >= 0 {
					t.Fatalf("expected no -ngl, got %v", args)
				}
				return
			}
			if idx < 0 || idx+1 >= len(args) {
				t.Fatalf("expected -ngl in %v", args)
			}
			if got := args[idx : idx+2]; !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildArgsJinja(t *testing.T) {
	tests := []struct {
		name  string
		jinja *bool
		want  []string // nil means neither --jinja nor --no-jinja is passed
	}{
		{"unset omits the flag (llama-server defaults to enabled)", nil, nil},
		{"explicit on", boolPtr(true), []string{"--jinja"}},
		// llama-server は --jinja がデフォルト有効なので、無効化するには明示的に --no-jinja が要る
		{"explicit off", boolPtr(false), []string{"--no-jinja"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Profile{Name: "p", ModelPath: "m.gguf", Jinja: tt.jinja}
			args := p.BuildArgs(8081)

			hasJinja := slices.Contains(args, "--jinja")
			hasNoJinja := slices.Contains(args, "--no-jinja")
			if tt.want == nil {
				if hasJinja || hasNoJinja {
					t.Fatalf("expected neither --jinja nor --no-jinja, got %v", args)
				}
				return
			}
			if !slices.Contains(args, tt.want[0]) {
				t.Errorf("expected %v in %v", tt.want, args)
			}
		})
	}
}

func TestBuildArgsLoadMode(t *testing.T) {
	tests := []struct {
		name string
		mode LoadMode
		want []string // nil means the flag must be absent
	}{
		{"unset omits the flag (llama-server defaults to auto)", "", nil},
		{"none maps to old --no-mmap behavior", LoadModeNone, []string{"--load-mode", "none"}},
		{"mmap+mlock has no bool equivalent", LoadModeMmapMlock, []string{"--load-mode", "mmap+mlock"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Profile{Name: "p", ModelPath: "m.gguf", LoadMode: tt.mode}
			args := p.BuildArgs(8081)

			idx := slices.Index(args, "--load-mode")
			if tt.want == nil {
				if idx >= 0 {
					t.Fatalf("expected no --load-mode, got %v", args)
				}
				return
			}
			if idx < 0 || idx+1 >= len(args) {
				t.Fatalf("expected --load-mode in %v", args)
			}
			if got := args[idx : idx+2]; !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSaveLoadPreservesZeroReasoningBudget(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Save(&Profile{Name: "z", ModelPath: "m.gguf", ReasoningBudget: intPtr(0), Cost: intPtr(0)}); err != nil {
		t.Fatal(err)
	}

	got, err := m.Load("z")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningBudget == nil || *got.ReasoningBudget != 0 {
		t.Errorf("reasoning_budget: got %v, want 0", got.ReasoningBudget)
	}
	if got.Cost == nil || *got.Cost != 0 {
		t.Errorf("cost: got %v, want 0", got.Cost)
	}
}

func TestSetRuntimeForAll(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Save(&Profile{Name: "a", ModelPath: "a.gguf", RuntimeDirName: "old-vulkan"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(&Profile{Name: "b", ModelPath: "b.gguf", RuntimeDirName: "old-vulkan"}); err != nil {
		t.Fatal(err)
	}
	// already on the target runtime; should be left alone and not counted as changed
	if err := m.Save(&Profile{Name: "c", ModelPath: "c.gguf", RuntimeDirName: "new-cuda"}); err != nil {
		t.Fatal(err)
	}

	changed, total, err := m.SetRuntimeForAll("new-cuda")
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2 {
		t.Errorf("changed: got %d, want 2", changed)
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}

	profiles, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if p.RuntimeDirName != "new-cuda" {
			t.Errorf("profile %q: got runtime %q, want new-cuda", p.Name, p.RuntimeDirName)
		}
	}
}

func TestListNamesUsesFilenames(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	for _, name := range []string{"b", "a"} {
		if err := m.Save(&Profile{Name: name, ModelPath: filepath.Join(dir, name)}); err != nil {
			t.Fatal(err)
		}
	}

	names, err := m.ListNames()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Errorf("got %v, want [a b]", names)
	}
}

func TestEffectiveCost(t *testing.T) {
	tests := []struct {
		name     string
		cost     *int
		measured *int
		want     *int
	}{
		{"neither set", nil, nil, nil},
		{"measured only", nil, intPtr(100), intPtr(100)},
		{"manual only", intPtr(200), nil, intPtr(200)},
		// 手で入れた値は実測値より優先される
		{"manual wins", intPtr(200), intPtr(100), intPtr(200)},
		// 0 も有効な手動値であって「未設定」ではない
		{"manual zero wins", intPtr(0), intPtr(100), intPtr(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Profile{Cost: tt.cost, MeasuredCost: tt.measured}
			got := p.EffectiveCost()
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("got %d, want nil", *got)
			case tt.want != nil && got == nil:
				t.Errorf("got nil, want %d", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("got %d, want %d", *got, *tt.want)
			}
		})
	}
}
