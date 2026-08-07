package workspace

import (
	"fmt"
	"os"
	"strings"

	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
)

// DefaultTTL is the TTL applied to entries built without a workspace.
const DefaultTTL = 900

// ResidentWorkspaceName is the synthetic name used for workspace-less runs.
const ResidentWorkspaceName = "(--resident)"

// BuildResident assembles an in-memory workspace out of every profile on disk.
// Profiles named in residentNames become resident; all others are included as
// on-demand. The result is never written to disk — a second copy of the truth
// would drift from the profiles it was derived from.
//
// 明示されたものと暗黙に含まれるものは非対称に扱う。ユーザーが名前を書いた
// プロファイルは無いと動かないのでエラーで落とし、名前を書いていない
// プロファイルは「おまけ」なので警告してスキップする。
//
// Returns the workspace along with warnings describing every skipped profile.
func BuildResident(
	profMgr *profile.Manager,
	rtMgr *runtime.Manager,
	residentNames []string,
	ttl int,
) (*Workspace, []string, error) {
	var warnings []string

	residentSet := make(map[string]bool)
	var order []string
	for _, name := range residentNames {
		name = strings.TrimSpace(name)
		if name == "" || residentSet[name] {
			continue
		}
		residentSet[name] = true
		order = append(order, name)
	}

	ws := &Workspace{Name: ResidentWorkspaceName}

	// 明示されたものは厳格に扱う
	for _, name := range order {
		prof, err := profMgr.Load(name)
		if err != nil {
			return nil, warnings, fmt.Errorf("resident profile %q: %w", name, err)
		}
		if err := CheckProfileUsable(prof, rtMgr); err != nil {
			return nil, warnings, fmt.Errorf("resident profile %q: %w", name, err)
		}
		ws.Entries = append(ws.Entries, Entry{ProfileName: name, Resident: true, TTL: ttl})
	}

	// 暗黙に含まれるものは寛容に扱う
	names, err := profMgr.ListNames()
	if err != nil {
		return nil, warnings, fmt.Errorf("failed to list profiles: %w", err)
	}
	for _, name := range names {
		if residentSet[name] {
			continue
		}
		prof, err := profMgr.Load(name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping profile %q: %v", name, err))
			continue
		}
		if err := CheckProfileUsable(prof, rtMgr); err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping profile %q: %v", name, err))
			continue
		}
		ws.Entries = append(ws.Entries, Entry{ProfileName: name, Resident: false, TTL: ttl})
	}

	if len(ws.Entries) == 0 {
		return nil, warnings, fmt.Errorf("no usable profiles found in %s", profMgr.Dir())
	}
	return ws, warnings, nil
}

// CheckProfileUsable reports whether a profile can actually be launched:
// its runtime must be installed and the files it points at must still exist.
func CheckProfileUsable(p *profile.Profile, rtMgr *runtime.Manager) error {
	if _, err := rtMgr.ServerPath(p.RuntimeDirName); err != nil {
		return fmt.Errorf("runtime %q not available: %w", p.RuntimeDirName, err)
	}
	if p.ModelPath == "" {
		return fmt.Errorf("model_path is empty")
	}
	if _, err := os.Stat(p.ModelPath); err != nil {
		return fmt.Errorf("model file missing: %s", p.ModelPath)
	}
	if p.MMProjPath != "" {
		if _, err := os.Stat(p.MMProjPath); err != nil {
			return fmt.Errorf("mmproj file missing: %s", p.MMProjPath)
		}
	}
	return nil
}
