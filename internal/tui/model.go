package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/desktopgame/llama-launcher/internal/config"
	"github.com/desktopgame/llama-launcher/internal/runtime"
)

type view int

const (
	viewMenu view = iota
	viewRemoteReleases
	viewBackendSelect
	viewInstalledRuntimes
	viewDownloading
)

type menuItem struct {
	title string
	desc  string
}

func (i menuItem) Title() string       { return i.title }
func (i menuItem) Description() string { return i.desc }
func (i menuItem) FilterValue() string { return i.title }

type Model struct {
	cfg     *config.Config
	cfgPath string
	manager *runtime.Manager
	current view
	menu    list.Model
	// release selection
	releases        list.Model
	fetchedReleases []runtime.Release
	// backend selection
	backends        list.Model
	selectedRelease *runtime.Release
	classifiedMap   map[string]runtime.AssetInfo // backend name -> parsed asset
	// installed runtimes
	installed list.Model
	// ui
	spinner spinner.Model
	status  string
	width   int
	height  int
}

func NewModel() Model {
	cfgPath := config.DefaultPath()
	cfg, _ := config.Load(cfgPath)
	mgr := runtime.NewManager(cfg.RuntimeDir)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	items := []list.Item{
		menuItem{title: "Download Runtime", desc: "Download a new llama.cpp version from GitHub"},
		menuItem{title: "Installed Runtimes", desc: "View and manage installed llama.cpp versions"},
	}

	menuList := list.New(items, list.NewDefaultDelegate(), 0, 0)
	menuList.Title = "llama-launcher"
	menuList.SetShowStatusBar(false)

	return Model{
		cfg:     cfg,
		cfgPath: cfgPath,
		manager: mgr,
		current: viewMenu,
		menu:    menuList,
		spinner: s,
	}
}

// messages
type releasesMsg struct {
	releases []runtime.Release
	err      error
}

type downloadMsg struct {
	dirName string
	err     error
}

type installedMsg struct {
	runtimes []runtime.InstalledRuntime
	err      error
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			switch m.current {
			case viewMenu:
				return m, tea.Quit
			case viewBackendSelect:
				m.current = viewRemoteReleases
				return m, nil
			default:
				m.current = viewMenu
				m.status = ""
				return m, nil
			}
		case "enter":
			return m.handleEnter()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.menu.SetSize(msg.Width, msg.Height-2)
		if len(m.fetchedReleases) > 0 {
			m.releases.SetSize(msg.Width, msg.Height-2)
		}
		return m, nil

	case releasesMsg:
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

	case downloadMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Download failed: %v", msg.err)
		} else {
			m.status = fmt.Sprintf("Downloaded %s successfully", msg.dirName)
		}
		m.current = viewMenu
		return m, nil

	case installedMsg:
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
	}
	return m, cmd
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.current {
	case viewMenu:
		i, ok := m.menu.SelectedItem().(menuItem)
		if !ok {
			return m, nil
		}
		switch i.title {
		case "Download Runtime":
			m.current = viewDownloading
			m.status = "Fetching releases..."
			return m, tea.Batch(m.spinner.Tick, fetchReleases())
		case "Installed Runtimes":
			m.current = viewDownloading
			m.status = "Loading..."
			return m, tea.Batch(m.spinner.Tick, m.listInstalled())
		}

	case viewRemoteReleases:
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

		// build backend list, default backend first
		var items []list.Item
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

	case viewBackendSelect:
		i, ok := m.backends.SelectedItem().(menuItem)
		if !ok || m.selectedRelease == nil {
			return m, nil
		}
		// strip " (default)" suffix
		backendName := strings.TrimSuffix(i.title, " (default)")
		info, ok := m.classifiedMap[backendName]
		if !ok {
			return m, nil
		}
		tag := m.selectedRelease.TagName
		dirName := runtime.RuntimeDirName(tag, backendName)
		m.current = viewDownloading
		m.status = fmt.Sprintf("Downloading %s...", dirName)
		return m, tea.Batch(m.spinner.Tick, m.download(tag, backendName, info.Asset))

	case viewInstalledRuntimes:
		i, ok := m.installed.SelectedItem().(menuItem)
		if !ok {
			return m, nil
		}
		if err := m.manager.Remove(i.title); err != nil {
			m.status = fmt.Sprintf("Failed to remove: %v", err)
		} else {
			m.status = fmt.Sprintf("Removed %s", i.title)
		}
		m.current = viewMenu
		return m, nil
	}
	return m, nil
}

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
	case viewDownloading:
		b.WriteString(fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.status))
	}

	if m.status != "" && m.current == viewMenu {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginLeft(2)
		b.WriteString("\n" + style.Render(m.status))
	}

	return b.String()
}

// commands

func fetchReleases() tea.Cmd {
	return func() tea.Msg {
		releases, err := runtime.FetchReleases(20)
		return releasesMsg{releases: releases, err: err}
	}
}

func (m Model) download(tag, backend string, asset runtime.Asset) tea.Cmd {
	return func() tea.Msg {
		dirName := runtime.RuntimeDirName(tag, backend)
		err := m.manager.Download(tag, backend, asset, nil)
		return downloadMsg{dirName: dirName, err: err}
	}
}

func (m Model) listInstalled() tea.Cmd {
	return func() tea.Msg {
		runtimes, err := m.manager.List()
		return installedMsg{runtimes: runtimes, err: err}
	}
}
