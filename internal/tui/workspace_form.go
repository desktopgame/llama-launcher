package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/huh"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/swap"
	"github.com/desktopgame/llama-launcher/internal/workspace"
)

// workspace creation steps
type wsStep int

const (
	wsStepNameAndSelect wsStep = iota // huh form: name + profile multi-select
	wsStepEntryList                   // list of entries, enter to edit each
	wsStepEditEntry                   // huh form: edit one entry's resident + TTL
)

type wsFormValues struct {
	wsName           string
	selectedProfiles []string
}

type wsEntryEdit struct {
	resident bool
	ttl      string
}

type wsFormState struct {
	step    wsStep
	editing *workspace.Workspace
	vals    *wsFormValues

	// step 1: huh form
	form *huh.Form

	// step 2: entry list
	entries     []workspace.Entry
	entryList   list.Model
	editingIdx  int
	entryForm   *huh.Form
	entryVals   *wsEntryEdit
}

// --- messages ---

type workspacesMsg struct {
	workspaces []*workspace.Workspace
	err        error
}

type wsFormDataMsg struct {
	editing  *workspace.Workspace
	profiles []*profile.Profile
	err      error
}

type wsSavedMsg struct {
	name string
	err  error
}

// --- form construction ---

func newWsFormState(
	editing *workspace.Workspace,
	profiles []*profile.Profile,
	width int,
) wsFormState {
	if width <= 0 {
		width = 80
	}

	vals := &wsFormValues{}

	profileOptions := make([]huh.Option[string], 0, len(profiles))
	for _, p := range profiles {
		label := fmt.Sprintf("%s (%s)", p.Name, filepath.Base(p.ModelPath))
		profileOptions = append(profileOptions, huh.NewOption(label, p.Name))
	}

	if editing != nil {
		vals.wsName = editing.Name
		for _, e := range editing.Entries {
			vals.selectedProfiles = append(vals.selectedProfiles, e.ProfileName)
		}
	}

	fs := wsFormState{
		step:    wsStepNameAndSelect,
		editing: editing,
		vals:    vals,
	}

	fs.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Workspace Name").
				Value(&vals.wsName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),
			huh.NewMultiSelect[string]().
				Title("Select Profiles").
				Value(&vals.selectedProfiles).
				Options(profileOptions...),
		),
	).WithWidth(width).WithShowHelp(true)

	// pre-populate entries if editing
	if editing != nil {
		fs.entries = make([]workspace.Entry, len(editing.Entries))
		copy(fs.entries, editing.Entries)
	}

	return fs
}

func (fs *wsFormState) buildEntryList(width, height int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}

	// sync entries with selected profiles
	existing := make(map[string]workspace.Entry)
	for _, e := range fs.entries {
		existing[e.ProfileName] = e
	}
	var newEntries []workspace.Entry
	for _, name := range fs.vals.selectedProfiles {
		if e, ok := existing[name]; ok {
			newEntries = append(newEntries, e)
		} else {
			newEntries = append(newEntries, workspace.Entry{
				ProfileName: name,
				Resident:    false,
				TTL:         300,
			})
		}
	}
	fs.entries = newEntries

	items := make([]list.Item, len(fs.entries))
	for i, e := range fs.entries {
		resLabel := "on-demand"
		if e.Resident {
			resLabel = "resident"
		}
		items[i] = menuItem{
			title: e.ProfileName,
			desc:  fmt.Sprintf("[%s] TTL: %ds", resLabel, e.TTL),
		}
	}

	fs.entryList = list.New(items, list.NewDefaultDelegate(), width, height-4)
	fs.entryList.Title = "Entries (enter to edit, Ctrl+S to save, q to back)"
}

func (fs *wsFormState) buildEntryForm(idx int, width int) {
	if width <= 0 {
		width = 80
	}
	fs.editingIdx = idx
	e := fs.entries[idx]
	fs.entryVals = &wsEntryEdit{
		resident: e.Resident,
		ttl:      strconv.Itoa(e.TTL),
	}
	fs.entryForm = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Resident (%s)", e.ProfileName)).
				Value(&fs.entryVals.resident),
			huh.NewInput().
				Title("TTL (seconds)").
				Value(&fs.entryVals.ttl).
				Validate(func(s string) error {
					if _, err := strconv.Atoi(s); err != nil {
						return fmt.Errorf("must be a number")
					}
					return nil
				}),
		),
	).WithWidth(width).WithShowHelp(true)
}

func (fs *wsFormState) toWorkspace() *workspace.Workspace {
	return &workspace.Workspace{
		Name:    strings.TrimSpace(fs.vals.wsName),
		Entries: fs.entries,
	}
}

// --- Update ---

func (m Model) updateWsForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	fs := &m.wsForm

	switch fs.step {
	case wsStepNameAndSelect:
		return m.updateWsNameAndSelect(msg)
	case wsStepEntryList:
		return m.updateWsEntryList(msg)
	case wsStepEditEntry:
		return m.updateWsEditEntry(msg)
	}
	return m, nil
}

func (m Model) updateWsNameAndSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	fs := &m.wsForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	formModel, cmd := fs.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		fs.form = f

		if f.State == huh.StateCompleted {
			if len(fs.vals.selectedProfiles) == 0 {
				m.status = "Select at least one profile"
				m.statusError = true
				m.current = viewMenu
				return m, nil
			}
			fs.buildEntryList(m.width, m.height)
			fs.step = wsStepEntryList
			return m, nil
		}
		if f.State == huh.StateAborted {
			m.current = viewMenu
			return m, nil
		}
	}
	return m, cmd
}

func (m Model) updateWsEntryList(msg tea.Msg) (tea.Model, tea.Cmd) {
	fs := &m.wsForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			// go back to name/select step
			fs.step = wsStepNameAndSelect
			fs.form = nil // will be re-initialized in handleWsFormDataMsg
			m.current = viewMenu
			return m, nil
		case "enter":
			idx := fs.entryList.Index()
			if idx >= 0 && idx < len(fs.entries) {
				fs.buildEntryForm(idx, m.width)
				fs.step = wsStepEditEntry
				return m, fs.entryForm.Init()
			}
			return m, nil
		case "ctrl+s":
			ws := fs.toWorkspace()
			mgr := m.wsMgr
			var removeOld bool
			oldName := ""
			if fs.editing != nil && fs.editing.Name != ws.Name {
				oldName = fs.editing.Name
				removeOld = true
			}
			m.current = viewLoading
			m.status = fmt.Sprintf("Saving workspace \"%s\"...", ws.Name)
			return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
				if removeOld {
					mgr.Remove(oldName)
				}
				err := mgr.Save(ws)
				return wsSavedMsg{name: ws.Name, err: err}
			})
		}
	}

	var cmd tea.Cmd
	fs.entryList, cmd = fs.entryList.Update(msg)
	return m, cmd
}

func (m Model) updateWsEditEntry(msg tea.Msg) (tea.Model, tea.Cmd) {
	fs := &m.wsForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	formModel, cmd := fs.entryForm.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		fs.entryForm = f

		if f.State == huh.StateCompleted {
			ttl, _ := strconv.Atoi(fs.entryVals.ttl)
			fs.entries[fs.editingIdx].Resident = fs.entryVals.resident
			fs.entries[fs.editingIdx].TTL = ttl
			fs.buildEntryList(m.width, m.height)
			fs.step = wsStepEntryList
			return m, nil
		}
		if f.State == huh.StateAborted {
			fs.step = wsStepEntryList
			return m, nil
		}
	}
	return m, cmd
}

// --- View ---

func (m Model) viewWsForm() string {
	fs := &m.wsForm

	switch fs.step {
	case wsStepNameAndSelect:
		if fs.form != nil {
			return fs.form.View()
		}
	case wsStepEntryList:
		return fs.entryList.View()
	case wsStepEditEntry:
		if fs.entryForm != nil {
			return fs.entryForm.View()
		}
	}
	return ""
}

// --- Workspace list handlers ---

func (m Model) handleWorkspacesMsg(msg workspacesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusError = true
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	m.fetchedWorkspaces = msg.workspaces

	items := []list.Item{
		menuItem{title: "+ New Workspace", desc: "Create a new workspace"},
	}
	for _, w := range msg.workspaces {
		entryNames := make([]string, len(w.Entries))
		for i, e := range w.Entries {
			tag := "od"
			if e.Resident {
				tag = "res"
			}
			entryNames[i] = fmt.Sprintf("%s(%s)", e.ProfileName, tag)
		}
		desc := strings.Join(entryNames, ", ")
		items = append(items, menuItem{title: w.Name, desc: desc})
	}

	w, h := m.width, m.height-2
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 20
	}
	listW := w / 2
	m.workspaceList = list.New(items, list.NewDefaultDelegate(), listW, h)
	m.workspaceList.Title = "Workspaces (s start, x stop, enter edit, d del, q back)"
	m.current = viewWorkspaces
	return m, nil
}

func (m Model) handleWorkspacesEnter() (tea.Model, tea.Cmd) {
	i, ok := m.workspaceList.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}

	var editing *workspace.Workspace
	if i.title != "+ New Workspace" {
		for _, w := range m.fetchedWorkspaces {
			if w.Name == i.title {
				editing = w
				break
			}
		}
	}

	profMgr := m.profManager
	m.current = viewLoading
	m.status = "Loading profiles..."

	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		profiles, err := profMgr.List()
		if err != nil {
			return wsFormDataMsg{err: err}
		}
		if len(profiles) == 0 {
			return wsFormDataMsg{err: fmt.Errorf("no profiles found — create a profile first")}
		}
		return wsFormDataMsg{editing: editing, profiles: profiles}
	})
}

func (m Model) handleWsFormDataMsg(msg wsFormDataMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusError = true
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}

	m.wsForm = newWsFormState(msg.editing, msg.profiles, m.width)
	m.current = viewWsForm
	return m, m.wsForm.form.Init()
}

func (m Model) handleWsDelete() (tea.Model, tea.Cmd) {
	i, ok := m.workspaceList.SelectedItem().(menuItem)
	if !ok || i.title == "+ New Workspace" {
		return m, nil
	}

	if err := m.wsMgr.Remove(i.title); err != nil {
		m.statusError = true
		m.status = fmt.Sprintf("Failed to remove: %v", err)
	} else {
		m.status = fmt.Sprintf("Removed workspace \"%s\"", i.title)
	}
	return m, listWorkspacesCmd(m.wsMgr)
}

func (m Model) viewWsDetail() string {
	idx := m.workspaceList.Index()
	wsIdx := idx - 1 // first item is "+ New Workspace"
	if wsIdx < 0 || wsIdx >= len(m.fetchedWorkspaces) {
		return ""
	}
	ws := m.fetchedWorkspaces[wsIdx]

	panelW := m.width/2 - 2
	if panelW <= 0 {
		panelW = 38
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	valueStyle := lipgloss.NewStyle().Bold(true)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Padding(1, 2).
		Width(panelW)

	var b strings.Builder
	b.WriteString(labelStyle.Render("Name:") + " " + valueStyle.Render(ws.Name) + "\n")
	b.WriteString(labelStyle.Render("Entries:") + " " + valueStyle.Render(fmt.Sprintf("%d", len(ws.Entries))) + "\n\n")

	for _, e := range ws.Entries {
		mode := "on-demand"
		if e.Resident {
			mode = "resident"
		}
		b.WriteString(valueStyle.Render(e.ProfileName) + "\n")
		b.WriteString(fmt.Sprintf("  %s  TTL: %ds\n", labelStyle.Render(mode), e.TTL))
	}

	return borderStyle.Render(b.String())
}

// --- Launch handlers ---

type swapStartedMsg struct {
	wsName string
	err    error
}

type swapStoppedMsg struct {
	err error
}

func (m Model) handleWsStart() (tea.Model, tea.Cmd) {
	i, ok := m.workspaceList.SelectedItem().(menuItem)
	if !ok || i.title == "+ New Workspace" {
		return m, nil
	}

	if m.swapProc.IsRunning() {
		m.statusError = true
		m.status = "llama-swap is already running. Stop it first."
		return m, nil
	}

	var ws *workspace.Workspace
	for _, w := range m.fetchedWorkspaces {
		if w.Name == i.title {
			ws = w
			break
		}
	}
	if ws == nil {
		return m, nil
	}

	profMgr := m.profManager
	rtMgr := m.rtManager
	port := m.cfg.Port
	proc := m.swapProc
	m.current = viewLoading
	m.status = fmt.Sprintf("Starting llama-swap with \"%s\"...", ws.Name)

	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		if err := swap.CheckInstalled(); err != nil {
			return swapStartedMsg{err: err}
		}

		configPath, err := swap.GenerateConfig(ws, profMgr, rtMgr, port)
		if err != nil {
			return swapStartedMsg{err: err}
		}

		if err := proc.Start(configPath, port); err != nil {
			return swapStartedMsg{err: err}
		}

		return swapStartedMsg{wsName: ws.Name}
	})
}

func (m Model) handleWsStop() (tea.Model, tea.Cmd) {
	if !m.swapProc.IsRunning() {
		m.statusError = true
		m.status = "llama-swap is not running"
		return m, nil
	}

	proc := m.swapProc
	return m, func() tea.Msg {
		err := proc.Stop()
		return swapStoppedMsg{err: err}
	}
}

// --- Commands ---

func listWorkspacesCmd(mgr *workspace.Manager) tea.Cmd {
	return func() tea.Msg {
		workspaces, err := mgr.List()
		return workspacesMsg{workspaces: workspaces, err: err}
	}
}

