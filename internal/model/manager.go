package model

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manager handles local GGUF model files across multiple directories.
type Manager struct {
	dirs []string
}

// LocalModel represents a GGUF file found on disk.
type LocalModel struct {
	Filename string // e.g. "Llama-2-7B-Q4_K_M.gguf"
	Path     string // full path
	Dir      string // which model_dir it's in
	Size     int64
}

// NewManager creates a Manager that scans the given directories.
func NewManager(dirs []string) *Manager {
	return &Manager{dirs: dirs}
}

// Dirs returns the configured model directories.
func (m *Manager) Dirs() []string {
	return m.dirs
}

// List returns all GGUF files found across all configured directories.
func (m *Manager) List() ([]LocalModel, error) {
	var models []LocalModel
	for _, dir := range m.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			models = append(models, LocalModel{
				Filename: e.Name(),
				Path:     filepath.Join(dir, e.Name()),
				Dir:      dir,
				Size:     info.Size(),
			})
		}
	}
	return models, nil
}

// Download fetches a GGUF file and saves it to destDir.
func (m *Manager) Download(file GGUFFile, destDir string, progress func(downloaded, total int64)) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	destPath := filepath.Join(destDir, file.Filename)
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("file %s already exists in %s", file.Filename, destDir)
	}

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(file.DownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(destDir, ".llama-launcher-dl-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	var reader io.Reader = resp.Body
	if progress != nil {
		reader = &progressReader{
			reader:     resp.Body,
			total:      resp.ContentLength,
			onProgress: progress,
		}
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to save download: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return nil
}

// Remove deletes a local model file.
func (m *Manager) Remove(path string) error {
	return os.Remove(path)
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
