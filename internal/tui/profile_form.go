package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/huh"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/desktopgame/llama-launcher/internal/model"
	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
)

// profile creation steps
type profileStep int

const (
	stepSelectModel profileStep = iota
	stepSelectRuntime
	stepSelectMMProj
	stepEditParams
)

type profileFormState struct {
	step    profileStep
	editing *profile.Profile

	// step 1: model selection
	modelList     list.Model
	models        []model.LocalModel
	selectedModel *model.LocalModel

	// step 2: runtime selection
	runtimeList     list.Model
	runtimes        []runtime.InstalledRuntime
	selectedRuntime *runtime.InstalledRuntime

	// step 3: mmproj selection
	mmprojList     list.Model
	mmprojs        []string
	selectedMMProj string

	// step 4: huh form for parameters
	form        *huh.Form
	profileName string
	contextSize string
	gpuLayers   string
	flashAttn   bool
	noMmap      bool
	extraArgs   string
}

// --- messages ---

type profilesMsg struct {
	profiles []*profile.Profile
	err      error
}

type profileFormDataMsg struct {
	editing  *profile.Profile
	models   []model.LocalModel
	runtimes []runtime.InstalledRuntime
	mmprojs  []string
	err      error
}

type profileSavedMsg struct {
	name string
	err  error
}

// --- init form ---

func newProfileFormState(
	editing *profile.Profile,
	models []model.LocalModel,
	runtimes []runtime.InstalledRuntime,
	mmprojs []string,
	width, height int,
) profileFormState {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}
	listH := height - 4

	pf := profileFormState{
		step:     stepSelectModel,
		editing:  editing,
		models:   models,
		runtimes: runtimes,
		mmprojs:  mmprojs,
	}

	// model list
	modelItems := make([]list.Item, len(models))
	for i, lm := range models {
		label := lm.Filename
		if lm.Source == model.SourceLMStudio {
			label = fmt.Sprintf("%s (%s/%s)", lm.Filename, lm.Publisher, lm.ModelName)
		}
		sizeGB := float64(lm.Size) / (1024 * 1024 * 1024)
		modelItems[i] = menuItem{title: label, desc: fmt.Sprintf("%.1f GB — %s", sizeGB, lm.Dir)}
	}
	pf.modelList = list.New(modelItems, list.NewDefaultDelegate(), width, listH)
	pf.modelList.Title = "Select Model"

	// runtime list
	rtItems := make([]list.Item, len(runtimes))
	for i, rt := range runtimes {
		rtItems[i] = menuItem{
			title: rt.DirName,
			desc:  fmt.Sprintf("%s [%s]", rt.Tag, rt.Backend),
		}
	}
	pf.runtimeList = list.New(rtItems, list.NewDefaultDelegate(), width, listH)
	pf.runtimeList.Title = "Select Runtime"

	// mmproj list
	mmprojItems := []list.Item{menuItem{title: "(none)", desc: "No multimodal projector"}}
	for _, mp := range mmprojs {
		mmprojItems = append(mmprojItems, menuItem{title: filepath.Base(mp), desc: mp})
	}
	pf.mmprojList = list.New(mmprojItems, list.NewDefaultDelegate(), width, listH)
	pf.mmprojList.Title = "Select mmproj (for vision models)"

	// pre-fill if editing
	if editing != nil {
		pf.profileName = editing.Name
		if editing.ContextSize > 0 {
			pf.contextSize = strconv.Itoa(editing.ContextSize)
		}
		if editing.GPULayers > 0 {
			pf.gpuLayers = strconv.Itoa(editing.GPULayers)
		}
		pf.flashAttn = editing.FlashAttention
		pf.noMmap = editing.NoMmap
		pf.extraArgs = editing.ExtraArgs
	}

	return pf
}

func (pf *profileFormState) buildHuhForm(width int) {
	if width <= 0 {
		width = 80
	}

	numValidator := func(s string) error {
		if s == "" {
			return nil
		}
		if _, err := strconv.Atoi(s); err != nil {
			return fmt.Errorf("must be a number")
		}
		return nil
	}

	pf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Profile Name").
				Value(&pf.profileName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Context Size (empty = default)").
				Value(&pf.contextSize).
				Validate(numValidator),
			huh.NewInput().
				Title("GPU Layers (empty = default)").
				Value(&pf.gpuLayers).
				Validate(numValidator),
			huh.NewConfirm().
				Title("Flash Attention").
				Value(&pf.flashAttn),
			huh.NewConfirm().
				Title("Disable mmap").
				Value(&pf.noMmap),
			huh.NewText().
				Title("Extra Args (free-form llama-server options)").
				Value(&pf.extraArgs),
		),
	).WithWidth(width).WithShowHelp(true)
}

func (pf *profileFormState) toProfile() *profile.Profile {
	ctxSize, _ := strconv.Atoi(pf.contextSize)
	gpuLayers, _ := strconv.Atoi(pf.gpuLayers)

	p := &profile.Profile{
		Name:           strings.TrimSpace(pf.profileName),
		ContextSize:    ctxSize,
		GPULayers:      gpuLayers,
		FlashAttention: pf.flashAttn,
		NoMmap:         pf.noMmap,
		ExtraArgs:      pf.extraArgs,
		MMProjPath:     pf.selectedMMProj,
	}
	if pf.selectedModel != nil {
		p.ModelPath = pf.selectedModel.Path
	}
	if pf.selectedRuntime != nil {
		p.RuntimeDirName = pf.selectedRuntime.DirName
	}
	return p
}

// --- Update ---

func (m Model) updateProfileForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	pf := &m.profileForm

	switch pf.step {
	case stepSelectModel:
		return m.updateProfileSelectModel(msg)
	case stepSelectRuntime:
		return m.updateProfileSelectRuntime(msg)
	case stepSelectMMProj:
		return m.updateProfileSelectMMProj(msg)
	case stepEditParams:
		return m.updateProfileEditParams(msg)
	}
	return m, nil
}

func (m Model) updateProfileSelectModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	pf := &m.profileForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			m.current = viewProfiles
			return m, nil
		case "enter":
			idx := pf.modelList.Index()
			if idx >= 0 && idx < len(pf.models) {
				pf.selectedModel = &pf.models[idx]
				pf.step = stepSelectRuntime
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	pf.modelList, cmd = pf.modelList.Update(msg)
	return m, cmd
}

func (m Model) updateProfileSelectRuntime(msg tea.Msg) (tea.Model, tea.Cmd) {
	pf := &m.profileForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			pf.step = stepSelectModel
			return m, nil
		case "enter":
			idx := pf.runtimeList.Index()
			if idx >= 0 && idx < len(pf.runtimes) {
				pf.selectedRuntime = &pf.runtimes[idx]
				pf.step = stepSelectMMProj
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	pf.runtimeList, cmd = pf.runtimeList.Update(msg)
	return m, cmd
}

func (m Model) updateProfileSelectMMProj(msg tea.Msg) (tea.Model, tea.Cmd) {
	pf := &m.profileForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			pf.step = stepSelectRuntime
			return m, nil
		case "enter":
			idx := pf.mmprojList.Index()
			if idx == 0 {
				pf.selectedMMProj = ""
			} else if idx > 0 && idx <= len(pf.mmprojs) {
				pf.selectedMMProj = pf.mmprojs[idx-1]
			}
			// build huh form and transition
			pf.buildHuhForm(m.width)
			pf.step = stepEditParams
			return m, pf.form.Init()
		}
	}

	var cmd tea.Cmd
	pf.mmprojList, cmd = pf.mmprojList.Update(msg)
	return m, cmd
}

func (m Model) updateProfileEditParams(msg tea.Msg) (tea.Model, tea.Cmd) {
	pf := &m.profileForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	// delegate to huh form
	formModel, cmd := pf.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		pf.form = f

		if f.State == huh.StateCompleted {
			p := pf.toProfile()
			if p.Name == "" {
				m.status = "Profile name is required"
				return m, nil
			}
			mgr := m.profManager
			return m, func() tea.Msg {
				err := mgr.Save(p)
				return profileSavedMsg{name: p.Name, err: err}
			}
		}
		if f.State == huh.StateAborted {
			pf.step = stepSelectMMProj
			return m, nil
		}
	}

	return m, cmd
}

// --- View ---

func (m Model) viewProfileForm() string {
	pf := &m.profileForm

	switch pf.step {
	case stepSelectModel:
		return pf.modelList.View()
	case stepSelectRuntime:
		return pf.runtimeList.View()
	case stepSelectMMProj:
		return pf.mmprojList.View()
	case stepEditParams:
		if pf.form != nil {
			return pf.form.View()
		}
	}
	return ""
}

// --- Profile list handlers ---

func (m Model) handleProfilesMsg(msg profilesMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	m.fetchedProfiles = msg.profiles

	items := []list.Item{
		menuItem{title: "+ New Profile", desc: "Create a new profile"},
	}
	for _, p := range msg.profiles {
		desc := fmt.Sprintf("%s — %s", p.RuntimeDirName, filepath.Base(p.ModelPath))
		if p.ExtraArgs != "" {
			desc += " [+args]"
		}
		items = append(items, menuItem{title: p.Name, desc: desc})
	}

	w, h := m.width, m.height-2
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 20
	}
	m.profileList = list.New(items, list.NewDefaultDelegate(), w, h)
	m.profileList.Title = "Profiles (enter to edit, q to back)"
	m.current = viewProfiles
	return m, nil
}

func (m Model) handleProfilesEnter() (tea.Model, tea.Cmd) {
	i, ok := m.profileList.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}

	var editing *profile.Profile
	if i.title != "+ New Profile" {
		for _, p := range m.fetchedProfiles {
			if p.Name == i.title {
				editing = p
				break
			}
		}
	}

	modelMgr := m.modelManager
	rtMgr := m.rtManager
	m.current = viewLoading
	m.status = "Loading models and runtimes..."

	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		models, err := modelMgr.List()
		if err != nil {
			return profileFormDataMsg{err: err}
		}
		runtimes, err := rtMgr.List()
		if err != nil {
			return profileFormDataMsg{err: err}
		}
		mmprojs := modelMgr.ListMMProj()
		return profileFormDataMsg{
			editing:  editing,
			models:   models,
			runtimes: runtimes,
			mmprojs:  mmprojs,
		}
	})
}

func (m Model) handleProfileFormDataMsg(msg profileFormDataMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = fmt.Sprintf("Error: %v", msg.err)
		m.current = viewMenu
		return m, nil
	}
	if len(msg.models) == 0 {
		m.status = "No models found. Download or configure models first."
		m.current = viewMenu
		return m, nil
	}
	if len(msg.runtimes) == 0 {
		m.status = "No runtimes installed. Download a runtime first."
		m.current = viewMenu
		return m, nil
	}

	m.profileForm = newProfileFormState(msg.editing, msg.models, msg.runtimes, msg.mmprojs, m.width, m.height)
	m.current = viewProfileForm
	return m, nil
}

// --- Commands ---

func listProfilesCmd(mgr *profile.Manager) tea.Cmd {
	return func() tea.Msg {
		profiles, err := mgr.List()
		return profilesMsg{profiles: profiles, err: err}
	}
}
