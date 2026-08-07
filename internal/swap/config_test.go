package swap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
	"github.com/desktopgame/llama-launcher/internal/workspace"
)

// newConfigEnv sets up a profile dir with one profile and a fake runtime.
func newConfigEnv(t *testing.T) (*profile.Manager, *runtime.Manager) {
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

	profMgr := profile.NewManager(filepath.Join(root, "profiles"))
	if err := profMgr.Save(&profile.Profile{
		Name:           "chat",
		ModelPath:      filepath.Join(root, "chat.gguf"),
		RuntimeDirName: "b1-vulkan",
		ModelType:      profile.ModelTypeGeneration,
	}); err != nil {
		t.Fatal(err)
	}
	return profMgr, runtime.NewManager(filepath.Join(root, "runtimes"))
}

func generate(t *testing.T, opts ...ConfigOption) string {
	t.Helper()
	profMgr, rtMgr := newConfigEnv(t)
	ws := &workspace.Workspace{Name: "w", Entries: []workspace.Entry{
		{ProfileName: "chat", Resident: true, TTL: 300},
	}}

	path, err := GenerateConfig(ws, profMgr, rtMgr, 8080, opts...)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// 既定では performance ブロックを書かない（llama-swap の既定に任せる）
func TestGenerateConfigOmitsPerformanceByDefault(t *testing.T) {
	if got := generate(t); strings.Contains(got, "performance:") {
		t.Errorf("did not expect a performance block:\n%s", got)
	}
}

// 計測時は安定判定の前提になるので明示的に書き出す
func TestGenerateConfigWritesPerfInterval(t *testing.T) {
	got := generate(t, WithPerfInterval(5*time.Second))
	if !strings.Contains(got, "performance:\n  every: 5s\n") {
		t.Errorf("expected a performance block with every: 5s:\n%s", got)
	}
}

func TestGenerateConfigGroupsAndPaths(t *testing.T) {
	got := generate(t)
	if !strings.Contains(got, "  resident:\n    swap: false\n") {
		t.Errorf("expected a resident group with swap: false:\n%s", got)
	}
	// Windowsパスはスラッシュに正規化される
	if strings.Contains(got, "\\") {
		t.Errorf("expected no backslashes in the generated config:\n%s", got)
	}
}
