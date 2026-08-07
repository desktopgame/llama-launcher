package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
)

func intPtr(n int) *int { return &n }

// testEnv creates a profile dir and a runtime dir containing one fake runtime.
type testEnv struct {
	profMgr  *profile.Manager
	rtMgr    *runtime.Manager
	modelDir string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()

	rtDir := filepath.Join(root, "runtimes", "b1-vulkan")
	if err := os.MkdirAll(rtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"llama-server", "llama-server.exe"} {
		if err := os.WriteFile(filepath.Join(rtDir, name), []byte("fake"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	modelDir := filepath.Join(root, "models")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}

	return &testEnv{
		profMgr:  profile.NewManager(filepath.Join(root, "profiles")),
		rtMgr:    runtime.NewManager(filepath.Join(root, "runtimes")),
		modelDir: modelDir,
	}
}

// addProfile writes a profile whose model file exists unless modelExists is false.
func (e *testEnv) addProfile(t *testing.T, name string, modelExists bool, cost *int) {
	t.Helper()
	modelPath := filepath.Join(e.modelDir, name+".gguf")
	if modelExists {
		if err := os.WriteFile(modelPath, []byte("gguf"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := &profile.Profile{
		Name:           name,
		ModelPath:      modelPath,
		RuntimeDirName: "b1-vulkan",
		ModelType:      profile.ModelTypeGeneration,
		Cost:           cost,
	}
	if err := e.profMgr.Save(p); err != nil {
		t.Fatal(err)
	}
}

func entryNames(ws *Workspace, resident bool) []string {
	var names []string
	for _, e := range ws.Entries {
		if e.Resident == resident {
			names = append(names, e.ProfileName)
		}
	}
	return names
}

func TestBuildResidentIncludesAllProfiles(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "chat", true, nil)
	env.addProfile(t, "judge", true, nil)
	env.addProfile(t, "spare", true, nil)

	ws, warnings, err := BuildResident(env.profMgr, env.rtMgr, []string{"chat", "judge"}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if got := entryNames(ws, true); !slices.Equal(got, []string{"chat", "judge"}) {
		t.Errorf("resident: got %v, want [chat judge]", got)
	}
	if got := entryNames(ws, false); !slices.Equal(got, []string{"spare"}) {
		t.Errorf("on-demand: got %v, want [spare]", got)
	}
	for _, e := range ws.Entries {
		if e.TTL != 900 {
			t.Errorf("%s: TTL = %d, want 900", e.ProfileName, e.TTL)
		}
	}
}

// 明示されたものは厳格に: 解決できなければ起動させない
func TestBuildResidentFailsOnBrokenExplicitProfile(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "chat", false, nil) // model file missing

	if _, _, err := BuildResident(env.profMgr, env.rtMgr, []string{"chat"}, 900); err == nil {
		t.Fatal("expected an error for a resident profile with a missing model")
	}

	if _, _, err := BuildResident(env.profMgr, env.rtMgr, []string{"nope"}, 900); err == nil {
		t.Fatal("expected an error for a resident profile that does not exist")
	}
}

// 暗黙に含まれるものは寛容に: 警告してスキップ
func TestBuildResidentSkipsBrokenImplicitProfile(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "chat", true, nil)
	env.addProfile(t, "deleted", false, nil)

	ws, warnings, err := BuildResident(env.profMgr, env.rtMgr, []string{"chat"}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	if got := entryNames(ws, false); len(got) != 0 {
		t.Errorf("on-demand: got %v, want none", got)
	}
}

func TestBuildResidentSkipsMissingRuntime(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "chat", true, nil)

	p, err := env.profMgr.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "orphan"
	p.RuntimeDirName = "b999-cuda"
	if err := env.profMgr.Save(p); err != nil {
		t.Fatal(err)
	}

	ws, warnings, err := BuildResident(env.profMgr, env.rtMgr, []string{"chat"}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	if len(ws.Entries) != 1 {
		t.Errorf("got %d entries, want 1", len(ws.Entries))
	}
}
