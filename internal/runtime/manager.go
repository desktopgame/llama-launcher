package runtime

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Manager handles local llama.cpp runtime versions.
type Manager struct {
	baseDir string // directory where runtimes are stored
}

// InstalledRuntime represents a locally installed llama.cpp version.
type InstalledRuntime struct {
	Tag       string // release tag (e.g. "b5000")
	Backend   string // backend name (e.g. "vulkan", "cuda")
	DirName   string // directory name (e.g. "b5000-vulkan")
	Path      string // full path to the extracted directory
	Installed time.Time
}

// DirName returns the directory name for a tag+backend combination.
func RuntimeDirName(tag, backend string) string {
	return tag + "-" + backend
}

// NewManager creates a Manager that stores runtimes under baseDir.
func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

// BaseDir returns the base directory for runtimes.
func (m *Manager) BaseDir() string {
	return m.baseDir
}

// Download fetches a release asset and extracts it into baseDir/<tag>-<backend>/.
func (m *Manager) Download(tag, backend string, asset Asset, progress func(downloaded, total int64)) error {
	dirName := RuntimeDirName(tag, backend)
	destDir := filepath.Join(m.baseDir, dirName)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("runtime %s already exists", dirName)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "llama-launcher-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	var reader io.Reader = resp.Body
	if progress != nil {
		reader = &progressReader{reader: resp.Body, total: asset.Size, onProgress: progress}
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	if err := extractZip(tmpPath, destDir); err != nil {
		os.RemoveAll(destDir)
		return fmt.Errorf("failed to extract: %w", err)
	}

	return nil
}

// List returns all installed runtimes sorted by tag descending.
func (m *Manager) List() ([]InstalledRuntime, error) {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var runtimes []InstalledRuntime
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		tag, backend := parseDirName(name)
		runtimes = append(runtimes, InstalledRuntime{
			Tag:       tag,
			Backend:   backend,
			DirName:   name,
			Path:      filepath.Join(m.baseDir, name),
			Installed: info.ModTime(),
		})
	}

	sort.Slice(runtimes, func(i, j int) bool {
		return runtimes[i].DirName > runtimes[j].DirName
	})

	return runtimes, nil
}

// Remove deletes an installed runtime by its directory name (e.g. "b5000-vulkan").
func (m *Manager) Remove(dirName string) error {
	dir := filepath.Join(m.baseDir, dirName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("runtime %s not found", dirName)
	}
	return os.RemoveAll(dir)
}

// ServerPath returns the path to llama-server executable for a given directory name.
func (m *Manager) ServerPath(dirName string) (string, error) {
	candidates := []string{
		filepath.Join(m.baseDir, dirName, "llama-server.exe"),
		filepath.Join(m.baseDir, dirName, "llama-server"),
		filepath.Join(m.baseDir, dirName, "bin", "llama-server.exe"),
		filepath.Join(m.baseDir, dirName, "bin", "llama-server"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("llama-server not found for runtime %s", dirName)
}

// parseDirName splits "b5000-vulkan" into ("b5000", "vulkan").
func parseDirName(name string) (tag, backend string) {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '-' {
			return name[:i], name[i+1:]
		}
	}
	return name, ""
}

type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	onProgress func(downloaded, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.downloaded += int64(n)
	if pr.onProgress != nil {
		pr.onProgress(pr.downloaded, pr.total)
	}
	return n, err
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		path := filepath.Join(dest, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0o755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
