package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const hfAPIBase = "https://huggingface.co/api"

// HFModel represents a model repository on HuggingFace.
type HFModel struct {
	ID        string     `json:"id"`        // e.g. "TheBloke/Llama-2-7B-GGUF"
	Downloads int        `json:"downloads"`
	Likes     int        `json:"likes"`
	Tags      []string   `json:"tags"`
	Siblings  []HFFile   `json:"siblings"`
}

// Author returns the author portion of the model ID.
func (m HFModel) Author() string {
	if i := strings.Index(m.ID, "/"); i >= 0 {
		return m.ID[:i]
	}
	return ""
}

// Name returns the model name portion of the ID.
func (m HFModel) Name() string {
	if i := strings.Index(m.ID, "/"); i >= 0 {
		return m.ID[i+1:]
	}
	return m.ID
}

// HFFile represents a file in a HuggingFace model repository.
type HFFile struct {
	RFilename string `json:"rfilename"`
}

// GGUFFile holds info about a single GGUF file available for download.
type GGUFFile struct {
	Filename    string // just the filename, e.g. "model-Q4_K_M.gguf"
	RepoPath    string // relative path in repo, e.g. "Q4_K_M/model.gguf"
	DownloadURL string // direct download URL
	RepoID      string // parent model ID, e.g. "TheBloke/Llama-2-7B-GGUF"
}

// SearchGGUF searches HuggingFace for GGUF model repositories.
func SearchGGUF(query string, limit int) ([]HFModel, error) {
	u := fmt.Sprintf("%s/models?search=%s&filter=gguf&sort=downloads&direction=-1&limit=%d",
		hfAPIBase, url.QueryEscape(query), limit)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("failed to search models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HuggingFace API returned status %d", resp.StatusCode)
	}

	var models []HFModel
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to decode models: %w", err)
	}
	return models, nil
}

// FetchGGUFFiles retrieves the list of GGUF files in a model repository.
func FetchGGUFFiles(repoID string) ([]GGUFFile, error) {
	u := fmt.Sprintf("%s/models/%s", hfAPIBase, repoID)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch model info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HuggingFace API returned status %d", resp.StatusCode)
	}

	var model HFModel
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, fmt.Errorf("failed to decode model: %w", err)
	}

	var files []GGUFFile
	for _, s := range model.Siblings {
		if !strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
			continue
		}
		files = append(files, GGUFFile{
			Filename:    path.Base(s.RFilename),
			RepoPath:    s.RFilename,
			DownloadURL: fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repoID, s.RFilename),
			RepoID:      repoID,
		})
	}
	return files, nil
}
