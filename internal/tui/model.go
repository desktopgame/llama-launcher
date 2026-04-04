package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/desktopgame/llama-launcher/internal/config"
	"github.com/desktopgame/llama-launcher/internal/model"
	"github.com/desktopgame/llama-launcher/internal/runtime"
)

type view int

const (
	viewMenu view = iota
	// runtime views
	viewRemoteReleases
	viewBackendSelect
	viewInstalledRuntimes
	// model views
	viewModelSearch
	viewModelResults
	viewModelFiles
	viewLocalModels
	// settings
	viewSettings
	// shared
	viewLoading
)

type menuItem struct {
	title string
	desc  string
}

func (i menuItem) Title() string       { return i.title }
func (i menuItem) Description() string { return i.desc }
func (i menuItem) FilterValue() string { return i.title }

type localTab struct {
	label string
	list  list.Model
}

type Model struct {
	cfg          *config.Config
	cfgPath      string
	rtManager    *runtime.Manager
	modelManager *model.Manager
	current      view
	prevView     view // for back navigation from nested views
	menu         list.Model
	// runtime: release selection
	releases        list.Model
	fetchedReleases []runtime.Release
	// runtime: backend selection
	backends        list.Model
	selectedRelease *runtime.Release
	classifiedMap   map[string]runtime.AssetInfo
	// runtime: installed
	installed list.Model
	// model: search
	searchInput textinput.Model
	// model: search results
	modelResults    list.Model
	fetchedModels   []model.HFModel
	// model: gguf file selection
	modelFiles      list.Model
	fetchedFiles    []model.GGUFFile
	// model: local models (tabbed by source)
	localTabs       []localTab // one per source
	localTabIdx     int        // active tab index
	// ui
	spinner spinner.Model
	status  string
	width   int
	height  int
}

func NewModel() Model {
	cfgPath := config.DefaultPath()
	cfg, _ := config.Load(cfgPath)
	rtMgr := runtime.NewManager(cfg.RuntimeDir)
	modelMgr := model.NewManager(cfg.ModelDirs, cfg.LMStudioDir)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ti := textinput.New()
	ti.Placeholder = "Search GGUF models on HuggingFace..."
	ti.CharLimit = 100
	ti.Width = 60

	items := []list.Item{
		menuItem{title: "Download Runtime", desc: "Download a new llama.cpp version from GitHub"},
		menuItem{title: "Installed Runtimes", desc: "View and manage installed llama.cpp versions"},
		menuItem{title: "Search Models", desc: "Search and download GGUF models from HuggingFace"},
		menuItem{title: "Local Models", desc: "View GGUF models on disk"},
		menuItem{title: "Settings", desc: "View current settings and open config file"},
	}

	menuList := list.New(items, list.NewDefaultDelegate(), 0, 0)
	menuList.Title = "llama-launcher"
	menuList.SetShowStatusBar(false)

	return Model{
		cfg:          cfg,
		cfgPath:      cfgPath,
		rtManager:    rtMgr,
		modelManager: modelMgr,
		current:      viewMenu,
		menu:         menuList,
		searchInput:  ti,
		spinner:      s,
	}
}

// --- messages ---

type releasesMsg struct {
	releases []runtime.Release
	err      error
}
type runtimeDownloadMsg struct {
	dirName string
	err     error
}
type installedRuntimesMsg struct {
	runtimes []runtime.InstalledRuntime
	err      error
}
type modelSearchMsg struct {
	models []model.HFModel
	err    error
}
type modelFilesMsg struct {
	files []model.GGUFFile
	err   error
}
type modelDownloadMsg struct {
	filename string
	err      error
}
type localModelsMsg struct {
	models []model.LocalModel
	err    error
}

// --- Init ---

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

// --- Update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// handle text input when searching
	if m.current == viewModelSearch {
		return m.updateModelSearch(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			return m.handleBack()
		case "enter":
			return m.handleEnter()
		case "left":
			if m.current == viewLocalModels && len(m.localTabs) > 0 {
				m.localTabIdx = (m.localTabIdx - 1 + len(m.localTabs)) % len(m.localTabs)
				return m, nil
			}
		case "right":
			if m.current == viewLocalModels && len(m.localTabs) > 0 {
				m.localTabIdx = (m.localTabIdx + 1) % len(m.localTabs)
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.menu.SetSize(msg.Width, msg.Height-2)
		if len(m.fetchedReleases) > 0 {
			m.releases.SetSize(msg.Width, msg.Height-2)
		}
		return m, nil

	// runtime messages
	case releasesMsg:
		return m.handleReleasesMsg(msg)
	case runtimeDownloadMsg:
		return m.handleRuntimeDownloadMsg(msg)
	case installedRuntimesMsg:
		return m.handleInstalledRuntimesMsg(msg)

	// settings messages
	case openFolderMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Failed to open folder: %v", msg.err)
		} else {
			m.status = "Opened config folder in explorer"
		}
		return m, nil

	// model messages
	case modelSearchMsg:
		return m.handleModelSearchMsg(msg)
	case modelFilesMsg:
		return m.handleModelFilesMsg(msg)
	case modelDownloadMsg:
		return m.handleModelDownloadMsg(msg)
	case localModelsMsg:
		return m.handleLocalModelsMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// delegate to active list
	var cmd tea.Cmd
	switch m.current {
	case viewMenu:
		m.menu, cmd = m.menu.Update(msg)
	case viewRemoteReleases:
		m.releases, cmd = m.releases.Update(msg)
	case viewBackendSelect:
		m.backends, cmd = m.backends.Update(msg)
	case viewInstalledRuntimes:
		m.installed, cmd = m.installed.Update(msg)
	case viewModelResults:
		m.modelResults, cmd = m.modelResults.Update(msg)
	case viewModelFiles:
		m.modelFiles, cmd = m.modelFiles.Update(msg)
	case viewLocalModels:
		if len(m.localTabs) > 0 {
			m.localTabs[m.localTabIdx].list, cmd = m.localTabs[m.localTabIdx].list.Update(msg)
		}
	}
	return m, cmd
}

func (m Model) handleBack() (tea.Model, tea.Cmd) {
	switch m.current {
	case viewMenu:
		return m, tea.Quit
	case viewBackendSelect:
		m.current = viewRemoteReleases
	case viewModelFiles:
		m.current = viewModelResults
	default:
		m.current = viewMenu
		m.status = ""
	}
	return m, nil
}

// --- Enter handlers ---

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.current {
	case viewMenu:
		return m.handleMenuEnter()
	case viewRemoteReleases:
		return m.handleReleaseEnter()
	case viewBackendSelect:
		return m.handleBackendEnter()
	case viewInstalledRuntimes:
		return m.handleInstalledEnter()
	case viewModelResults:
		return m.handleModelResultEnter()
	case viewModelFiles:
		return m.handleModelFileEnter()
	case viewLocalModels:
		return m.handleLocalModelEnter()
	case viewSettings:
		return m.handleSettingsEnter()
	}
	return m, nil
}

func (m Model) handleMenuEnter() (tea.Model, tea.Cmd) {
	i, ok := m.menu.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}
	switch i.title {
	case "Download Runtime":
		m.current = viewLoading
		m.status = "Fetching releases..."
		return m, tea.Batch(m.spinner.Tick, fetchReleasesCmd())
	case "Installed Runtimes":
		m.current = viewLoading
		m.status = "Loading..."
		return m, tea.Batch(m.spinner.Tick, listInstalledCmd(m.rtManager))
	case "Search Models":
		m.searchInput.Focus()
		m.current = viewModelSearch
		return m, textinput.Blink
	case "Local Models":
		m.current = viewLoading
		m.status = "Scanning model directories..."
		return m, tea.Batch(m.spinner.Tick, listLocalModelsCmd(m.modelManager))
	case "Settings":
		m.current = viewSettings
		return m, nil
	}
	return m, nil
}

func (m Model) handleReleaseEnter() (tea.Model, tea.Cmd) {
	idx := m.releases.Index()
	if idx < 0 || idx >= len(m.fetchedReleases) {
		return m, nil
	}
	release := m.fetchedReleases[idx]
	m.selectedRelease = &release
	classified := runtime.ClassifyAssets(release.Assets, "win", "x64")
	if len(classified) == 0 {
		m.status = "No compatible assets found for this release"
		m.current = viewMenu
		return m, nil
	}
	m.classifiedMap = classified

	backendNames := make([]string, 0, len(classified))
	for name := range classified {
		backendNames = append(backendNames, name)
	}
	sort.Strings(backendNames)

	// move default backend to top
	for i, name := range backendNames {
		if name == m.cfg.DefaultBackend {
			backendNames = append(backendNames[:i], backendNames[i+1:]...)
			backendNames = append([]string{name}, backendNames...)
			break
		}
	}

	var items []list.Item
	for _, name := range backendNames {
		info := classified[name]
		suffix := ""
		if name == m.cfg.DefaultBackend {
			suffix = " (default)"
		}
		items = append(items, menuItem{
			title: name + suffix,
			desc:  fmt.Sprintf("%s (%.1f MB)", info.Asset.Name, float64(info.Asset.Size)/(1024*1024)),
		})
	}

	m.backends = list.New(items, list.NewDefaultDelegate(), m.width, m.height-2)
	m.backends.Title = fmt.Sprintf("Select backend for %s", release.TagName)
	m.current = viewBackendSelect
	return m, nil
}

func (m Model) handleBackendEnter() (tea.Model, tea.Cmd) {
	i, ok := m.backends.SelectedItem().(menuItem)
	if !ok || m.selectedRelease == nil {
		return m, nil
	}
	backendName := strings.TrimSuffix(i.title, " (default)")
	info, ok := m.classifiedMap[backendName]
	if !ok {
		return m, nil
	}
	tag := m.selectedRelease.TagName
	dirName := runtime.RuntimeDirName(tag, backendName)
	m.current = viewLoading
	m.status = fmt.Sprintf("Downloading %s...", dirName)
	return m, tea.Batch(m.spinner.Tick, downloadRuntimeCmd(m.rtManager, tag, backendName, info.Asset))
}

func (m Model) handleInstalledEnter() (tea.Model, tea.Cmd) {
	i, ok := m.installed.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}
	if err := m.rtManager.Remove(i.title); err != nil {
		m.status = fmt.Sprintf("Failed to remove: %v", err)
	} else {
		m.status = fmt.Sprintf("Removed %s", i.title)
	}
	m.current = viewMenu
	return m, nil
}

// --- Model search ---

func (m Model) updateModelSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.current = viewMenu
			return m, nil
		case "enter":
			query := m.searchInput.Value()
			if query == "" {
				return m, nil
			}
			m.current = viewLoading
			m.status = fmt.Sprintf("Searching \"%s\"...", query)
			return m, tea.Batch(m.spinner.Tick, searchModelsCmd(query))
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

func (m Model) handleModelResultEnter() (tea.Model, tea.Cmd) {
	idx := m.modelResults.Index()
	if idx < 0 || idx >= len(m.fetchedModels) {
		return m, nil
	}
	hfModel := m.fetchedModels[idx]
	m.current = viewLoading
	m.status = fmt.Sprintf("Fetching files for %s...", hfModel.ID)
	return m, tea.Batch(m.spinner.Tick, fetchModelFilesCmd(hfModel.ID))
}

func (m Model) handleModelFileEnter() (tea.Model, tea.Cmd) {
	idx := m.modelFiles.Index()
	if idx < 0 || idx >= len(m.fetchedFiles) {
		return m, nil
	}
	file := m.fetchedFiles[idx]
	destDir := m.cfg.ModelDirs[0] // download to first configured directory
	m.current = viewLoading
	m.status = fmt.Sprintf("Downloading %s...", file.Filename)
	return m, tea.Batch(m.spinner.Tick, downloadModelCmd(m.modelManager, file, destDir))
}

func (m Model) handleLocalModelEnter() (tea.Model, tea.Cmd) {
	// TODO: could add delete confirmation
	m.current = viewMenu
	return m, nil
}


type openFolderMsg struct{ err error }

func (m Model) handleSettingsEnter() (tea.Model, tea.Cmd) {
	cfgPath := m.cfgPath
	cfg := m.cfg
	return m, func() tea.Msg {
		// create default config if it doesn't exist
		if err := config.EnsureExists(cfgPath, cfg); err != nil {
			return openFolderMsg{err: err}
		}
		err := exec.Command("explorer", filepath.Dir(cfgPath)).Start()
		return openFolderMsg{err: err}
	}
}

// --- Message handlers ---

func (m Model) handleReleasesMsg(msg releasesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	m.fetchedReleases = msg.releases
	items := make([]list.Item, len(msg.releases))
	for i, r := range msg.releases {
		items[i] = menuItem{
			title: r.TagName,
			desc:  fmt.Sprintf("%s — %d assets", r.PublishedAt.Format("2006-01-02"), len(r.Assets)),
		}
	}
	m.releases = list.New(items, list.NewDefaultDelegate(), m.width, m.height-2)
	m.releases.Title = "Select a release"
	m.current = viewRemoteReleases
	return m, nil
}

func (m Model) handleRuntimeDownloadMsg(msg runtimeDownloadMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Download failed: %v", msg.err)
	} else {
		m.status = fmt.Sprintf("Downloaded %s successfully", msg.dirName)
	}
	m.current = viewMenu
	return m, nil
}

func (m Model) handleInstalledRuntimesMsg(msg installedRuntimesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	items := make([]list.Item, len(msg.runtimes))
	for i, r := range msg.runtimes {
		items[i] = menuItem{
			title: r.DirName,
			desc:  fmt.Sprintf("%s [%s] — Installed: %s", r.Tag, r.Backend, r.Installed.Format("2006-01-02 15:04")),
		}
	}
	m.installed = list.New(items, list.NewDefaultDelegate(), m.width, m.height-2)
	m.installed.Title = "Installed Runtimes (enter to delete, q to back)"
	m.current = viewInstalledRuntimes
	return m, nil
}

func (m Model) handleModelSearchMsg(msg modelSearchMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	m.fetchedModels = msg.models
	items := make([]list.Item, len(msg.models))
	for i, hm := range msg.models {
		items[i] = menuItem{
			title: hm.ID,
			desc:  fmt.Sprintf("Downloads: %d  Likes: %d", hm.Downloads, hm.Likes),
		}
	}
	m.modelResults = list.New(items, list.NewDefaultDelegate(), m.width, m.height-2)
	m.modelResults.Title = "Search Results (enter to view files, q to back)"
	m.current = viewModelResults
	return m, nil
}

func (m Model) handleModelFilesMsg(msg modelFilesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewModelResults
		return m, nil
	}
	if len(msg.files) == 0 {
		m.status = "No GGUF files found in this repository"
		m.current = viewModelResults
		return m, nil
	}
	m.fetchedFiles = msg.files
	items := make([]list.Item, len(msg.files))
	for i, f := range msg.files {
		items[i] = menuItem{
			title: f.Filename,
			desc:  f.RepoPath,
		}
	}
	m.modelFiles = list.New(items, list.NewDefaultDelegate(), m.width, m.height-2)
	m.modelFiles.Title = "Select a GGUF file to download (q to back)"
	m.current = viewModelFiles
	return m, nil
}

func (m Model) handleModelDownloadMsg(msg modelDownloadMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Download failed: %v", msg.err)
	} else {
		m.status = fmt.Sprintf("Downloaded %s successfully", msg.filename)
	}
	m.current = viewMenu
	return m, nil
}

func (m Model) handleLocalModelsMsg(msg localModelsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	if len(msg.models) == 0 {
		m.status = "No GGUF models found in configured directories"
		m.current = viewMenu
		return m, nil
	}

	// group models by source directory
	groups := make(map[string][]model.LocalModel)
	var order []string
	for _, lm := range msg.models {
		var key string
		if lm.Source == model.SourceLMStudio {
			key = "LM Studio"
		} else {
			key = lm.Dir
		}
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], lm)
	}

	m.localTabs = nil
	for _, key := range order {
		models := groups[key]
		items := make([]list.Item, len(models))
		for i, lm := range models {
			sizeGB := float64(lm.Size) / (1024 * 1024 * 1024)
			var desc string
			if lm.Source == model.SourceLMStudio {
				desc = fmt.Sprintf("%.1f GB — %s/%s", sizeGB, lm.Publisher, lm.ModelName)
			} else {
				desc = fmt.Sprintf("%.1f GB", sizeGB)
			}
			items[i] = menuItem{
				title: lm.Filename,
				desc:  desc,
			}
		}
		l := list.New(items, list.NewDefaultDelegate(), m.width, m.height-4)
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		m.localTabs = append(m.localTabs, localTab{label: key, list: l})
	}
	m.localTabIdx = 0
	m.current = viewLocalModels
	return m, nil
}

// --- View ---

func (m Model) View() string {
	var b strings.Builder

	switch m.current {
	case viewMenu:
		b.WriteString(m.menu.View())
	case viewRemoteReleases:
		b.WriteString(m.releases.View())
	case viewBackendSelect:
		b.WriteString(m.backends.View())
	case viewInstalledRuntimes:
		b.WriteString(m.installed.View())
	case viewModelSearch:
		b.WriteString(fmt.Sprintf("\n  Search GGUF Models\n\n  %s\n\n  Press Enter to search, Esc to cancel\n", m.searchInput.View()))
	case viewModelResults:
		b.WriteString(m.modelResults.View())
	case viewModelFiles:
		b.WriteString(m.modelFiles.View())
	case viewLocalModels:
		b.WriteString(m.viewLocalModels())
	case viewSettings:
		b.WriteString(m.viewSettings())
	case viewLoading:
		b.WriteString(fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.status))
	}

	if m.status != "" && m.current == viewMenu {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginLeft(2)
		b.WriteString("\n" + style.Render(m.status))
	}

	return b.String()
}

func (m Model) viewLocalModels() string {
	if len(m.localTabs) == 0 {
		return "\n  No models found\n"
	}

	var b strings.Builder
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Padding(0, 2)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Padding(0, 2)

	b.WriteString("\n ")
	for i, tab := range m.localTabs {
		if i == m.localTabIdx {
			b.WriteString(activeStyle.Render("[ " + tab.label + " ]"))
		} else {
			b.WriteString(inactiveStyle.Render("  " + tab.label + "  "))
		}
	}
	b.WriteString("\n")

	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginLeft(2)
	b.WriteString(hintStyle.Render("← → switch tab  |  q back"))
	b.WriteString("\n")

	b.WriteString(m.localTabs[m.localTabIdx].list.View())
	return b.String()
}

func (m Model) viewSettings() string {
	titleStyle := lipgloss.NewStyle().Bold(true).MarginLeft(2).MarginTop(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginLeft(4)
	valueStyle := lipgloss.NewStyle().MarginLeft(6)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginLeft(2).MarginTop(1)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Settings"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Config file:"))
	b.WriteString("\n")
	b.WriteString(valueStyle.Render(m.cfgPath))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Runtime directory:"))
	b.WriteString("\n")
	b.WriteString(valueStyle.Render(m.cfg.RuntimeDir))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Default backend:"))
	b.WriteString("\n")
	b.WriteString(valueStyle.Render(m.cfg.DefaultBackend))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Model directories:"))
	b.WriteString("\n")
	if len(m.cfg.ModelDirs) == 0 {
		b.WriteString(valueStyle.Render("(none)"))
	} else {
		for _, d := range m.cfg.ModelDirs {
			b.WriteString(valueStyle.Render("- " + d))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	b.WriteString(labelStyle.Render("LM Studio directory:"))
	b.WriteString("\n")
	if m.cfg.LMStudioDir == "" {
		b.WriteString(valueStyle.Render("(not set)"))
	} else {
		b.WriteString(valueStyle.Render(m.cfg.LMStudioDir))
	}
	b.WriteString("\n")

	b.WriteString(hintStyle.Render("Press Enter to open config folder  |  q to back"))

	if m.status != "" {
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).MarginLeft(2)
		b.WriteString("\n")
		b.WriteString(statusStyle.Render(m.status))
	}

	return b.String()
}

// --- Commands ---

func fetchReleasesCmd() tea.Cmd {
	return func() tea.Msg {
		releases, err := runtime.FetchReleases(20)
		return releasesMsg{releases: releases, err: err}
	}
}

func downloadRuntimeCmd(mgr *runtime.Manager, tag, backend string, asset runtime.Asset) tea.Cmd {
	return func() tea.Msg {
		dirName := runtime.RuntimeDirName(tag, backend)
		err := mgr.Download(tag, backend, asset, nil)
		return runtimeDownloadMsg{dirName: dirName, err: err}
	}
}

func listInstalledCmd(mgr *runtime.Manager) tea.Cmd {
	return func() tea.Msg {
		runtimes, err := mgr.List()
		return installedRuntimesMsg{runtimes: runtimes, err: err}
	}
}

func searchModelsCmd(query string) tea.Cmd {
	return func() tea.Msg {
		models, err := model.SearchGGUF(query, 20)
		return modelSearchMsg{models: models, err: err}
	}
}

func fetchModelFilesCmd(repoID string) tea.Cmd {
	return func() tea.Msg {
		files, err := model.FetchGGUFFiles(repoID)
		return modelFilesMsg{files: files, err: err}
	}
}

func downloadModelCmd(mgr *model.Manager, file model.GGUFFile, destDir string) tea.Cmd {
	return func() tea.Msg {
		err := mgr.Download(file, destDir, nil)
		return modelDownloadMsg{filename: file.Filename, err: err}
	}
}

func listLocalModelsCmd(mgr *model.Manager) tea.Cmd {
	return func() tea.Msg {
		models, err := mgr.List()
		return localModelsMsg{models: models, err: err}
	}
}
