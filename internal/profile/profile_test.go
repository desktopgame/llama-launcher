package profile

import (
	"path/filepath"
	"slices"
	"testing"
)

func intPtr(n int) *int { return &n }

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
