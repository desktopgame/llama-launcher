package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	repoOwner = "ggml-org"
	repoName  = "llama.cpp"
	apiBase   = "https://api.github.com"
)

// Release represents a llama.cpp GitHub release.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Asset represents a downloadable file in a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// AssetInfo holds parsed metadata from an asset filename.
// Filename format: llama-{tag}-bin-{os}-{backend}-{arch}.{ext}
// Examples:
//
//	llama-b8660-bin-win-vulkan-x64.zip
//	llama-b8660-bin-win-cuda-12.4-x64.zip
//	llama-b8660-bin-win-cpu-x64.zip
//	llama-b8660-bin-win-hip-radeon-x64.zip
//	llama-b8660-bin-ubuntu-rocm-7.2-x64.tar.gz
//	llama-b8660-bin-macos-arm64.tar.gz
type AssetInfo struct {
	Tag     string // e.g. "b8660"
	OS      string // e.g. "win", "ubuntu", "macos"
	Backend string // e.g. "cpu", "vulkan", "cuda-12.4", "hip-radeon"
	Arch    string // e.g. "x64", "arm64"
	Asset   Asset  // original asset
}

// knownArches are the architecture suffixes that appear at the end of the name.
var knownArches = []string{"x64", "x86", "arm64", "aarch64", "s390x"}

// knownOSes are the OS segments that appear after "bin-".
var knownOSes = []string{"win", "ubuntu", "macos", "openEuler"}

// ParseAssetName extracts structured info from a llama.cpp release asset filename.
// Returns nil if the filename doesn't match the expected pattern.
func ParseAssetName(a Asset) *AssetInfo {
	name := a.Name

	// strip extension
	base := name
	if strings.HasSuffix(base, ".tar.gz") {
		base = strings.TrimSuffix(base, ".tar.gz")
	} else {
		ext := path.Ext(base)
		if ext == "" {
			return nil
		}
		base = strings.TrimSuffix(base, ext)
	}

	// must start with "llama-" and contain "-bin-"
	if !strings.HasPrefix(base, "llama-") {
		return nil
	}
	binIdx := strings.Index(base, "-bin-")
	if binIdx < 0 {
		return nil
	}

	// tag is between "llama-" and "-bin-"
	tag := base[len("llama-"):binIdx]
	rest := base[binIdx+len("-bin-"):]

	// split remaining parts
	parts := strings.Split(rest, "-")
	if len(parts) < 1 {
		return nil
	}

	// first part is OS
	osName := ""
	osLen := 0
	for _, known := range knownOSes {
		if parts[0] == known {
			osName = known
			osLen = 1
			break
		}
	}
	if osName == "" {
		return nil
	}

	remaining := parts[osLen:]
	if len(remaining) == 0 {
		return nil
	}

	// last part is arch
	arch := ""
	for _, known := range knownArches {
		if remaining[len(remaining)-1] == known {
			arch = known
			remaining = remaining[:len(remaining)-1]
			break
		}
	}
	if arch == "" {
		return nil
	}

	// everything in between is the backend (may be empty for base/cpu builds)
	backend := "cpu"
	if len(remaining) > 0 {
		backend = strings.Join(remaining, "-")
	}

	return &AssetInfo{
		Tag:     tag,
		OS:      osName,
		Backend: backend,
		Arch:    arch,
		Asset:   a,
	}
}

// ClassifyAssets parses all assets and returns those matching the given OS and arch,
// grouped by backend name.
func ClassifyAssets(assets []Asset, osFilter, archFilter string) map[string]AssetInfo {
	result := make(map[string]AssetInfo)
	for _, a := range assets {
		info := ParseAssetName(a)
		if info == nil {
			continue
		}
		if info.OS != osFilter || info.Arch != archFilter {
			continue
		}
		// keep the first match per backend (assets are usually ordered)
		if _, exists := result[info.Backend]; !exists {
			result[info.Backend] = *info
		}
	}
	return result
}

// FetchReleases retrieves recent releases from the llama.cpp GitHub repository.
// It returns up to perPage releases.
func FetchReleases(perPage int) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", apiBase, repoOwner, repoName, perPage)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode releases: %w", err)
	}

	return releases, nil
}
