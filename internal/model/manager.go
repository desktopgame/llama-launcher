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

// Source indicates where a local model was found.
type Source string

const (
	SourceUser     Source = "user"      // from model_dirs
	SourceLMStudio Source = "lmstudio"  // from lmstudio_dir
)

// Manager handles local GGUF model files across multiple sources.
type Manager struct {
	dirs        []string // user model directories (recursive scan)
	lmStudioDir string   // LM Studio directory (publisher/model-name layout)
}

// LocalModel represents a GGUF file found on disk.
type LocalModel struct {
	Filename  string // e.g. "Llama-2-7B-Q4_K_M.gguf"
	Path      string // full path
	Dir       string // parent directory
	Size      int64
	Source    Source  // where it was found
	Publisher string // LM Studio only: e.g. "TheBloke"
	ModelName string // LM Studio only: e.g. "Llama-2-7B-GGUF"
}

// DisplayName returns a human-readable name for the model.
func (m LocalModel) DisplayName() string {
	if m.Publisher != "" {
		return fmt.Sprintf("%s/%s/%s", m.Publisher, m.ModelName, m.Filename)
	}
	return m.Filename
}

// NewManager creates a Manager with user directories and optional LM Studio directory.
func NewManager(dirs []string, lmStudioDir string) *Manager {
	return &Manager{dirs: dirs, lmStudioDir: lmStudioDir}
}

// List returns all GGUF files found across all sources.
func (m *Manager) List() ([]LocalModel, error) {
	var models []LocalModel

	// scan user directories (recursive)
	for _, dir := range m.dirs {
		found, err := scanRecursive(dir, SourceUser)
		if err != nil {
			return nil, err
		}
		models = append(models, found...)
	}

	// scan LM Studio directory
	if m.lmStudioDir != "" {
		found, err := scanLMStudio(m.lmStudioDir)
		if err != nil {
			return nil, err
		}
		models = append(models, found...)
	}

	return models, nil
}

// scanRecursive walks a directory tree and returns all GGUF files.
func scanRecursive(root string, source Source) ([]LocalModel, error) {
	var models []LocalModel

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, ".gguf") {
			return nil
		}
		if isAuxiliaryGGUF(lower) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		models = append(models, LocalModel{
			Filename: d.Name(),
			Path:     path,
			Dir:      filepath.Dir(path),
			Size:     info.Size(),
			Source:   source,
		})
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to scan %s: %w", root, err)
	}
	return models, nil
}

// scanLMStudio scans the LM Studio models directory.
// Expected layout: {lmStudioDir}/{publisher}/{model-name}/*.gguf
func scanLMStudio(root string) ([]LocalModel, error) {
	var models []LocalModel

	publishers, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read LM Studio dir: %w", err)
	}

	for _, pub := range publishers {
		if !pub.IsDir() {
			continue
		}
		pubDir := filepath.Join(root, pub.Name())
		modelDirs, err := os.ReadDir(pubDir)
		if err != nil {
			continue
		}
		for _, md := range modelDirs {
			if !md.IsDir() {
				continue
			}
			modelDir := filepath.Join(pubDir, md.Name())
			files, err := os.ReadDir(modelDir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				lower := strings.ToLower(f.Name())
				if !strings.HasSuffix(lower, ".gguf") {
					continue
				}
				if isAuxiliaryGGUF(lower) {
					continue
				}
				info, err := f.Info()
				if err != nil {
					continue
				}
				models = append(models, LocalModel{
					Filename:  f.Name(),
					Path:      filepath.Join(modelDir, f.Name()),
					Dir:       modelDir,
					Size:      info.Size(),
					Source:    SourceLMStudio,
					Publisher: pub.Name(),
					ModelName: md.Name(),
				})
			}
		}
	}

	return models, nil
}

// ListMMProj returns paths of mmproj GGUF files across all sources.
func (m *Manager) ListMMProj() []string {
	var mmprojs []string
	scan := func(root string) {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			lower := strings.ToLower(d.Name())
			if strings.HasSuffix(lower, ".gguf") && strings.Contains(lower, "mmproj") {
				mmprojs = append(mmprojs, path)
			}
			return nil
		})
	}
	for _, dir := range m.dirs {
		scan(dir)
	}
	if m.lmStudioDir != "" {
		scan(m.lmStudioDir)
	}
	return mmprojs
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

// isAuxiliaryGGUF returns true for GGUF files that are not standalone models
// (e.g. multimodal projectors). These are used alongside a main model via
// --mmproj and shouldn't appear in the primary model list.
func isAuxiliaryGGUF(lowerName string) bool {
	return strings.Contains(lowerName, "mmproj")
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
