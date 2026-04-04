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

// profileFormValues holds form-bound values via pointers so they survive
// Bubble Tea's value-copy semantics.
type profileFormValues struct {
	profileName string
	modelPath   string
	runtimeDir  string
	contextSize string
	gpuLayers   string
	flashAttn   bool
	noMmap      bool
	mmprojPath  string
	extraArgs   string
}

type profileFormState struct {
	form    *huh.Form
	editing *profile.Profile
	vals    *profileFormValues // pointer so huh bindings survive copies
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

// --- build form ---

func newProfileFormState(
	editing *profile.Profile,
	models []model.LocalModel,
	runtimes []runtime.InstalledRuntime,
	mmprojs []string,
	width int,
) profileFormState {
	if width <= 0 {
		width = 80
	}

	vals := &profileFormValues{}
	pf := profileFormState{editing: editing, vals: vals}

	// pre-fill if editing
	if editing != nil {
		vals.profileName = editing.Name
		vals.modelPath = editing.ModelPath
		vals.runtimeDir = editing.RuntimeDirName
		if editing.ContextSize > 0 {
			vals.contextSize = strconv.Itoa(editing.ContextSize)
		}
		if editing.GPULayers > 0 {
			vals.gpuLayers = strconv.Itoa(editing.GPULayers)
		}
		vals.flashAttn = editing.FlashAttention
		vals.noMmap = editing.NoMmap
		vals.mmprojPath = editing.MMProjPath
		vals.extraArgs = editing.ExtraArgs
	}

	// model options
	modelOptions := make([]huh.Option[string], 0, len(models))
	for _, lm := range models {
		label := lm.Filename
		if lm.Source == model.SourceLMStudio {
			label = fmt.Sprintf("%s (%s/%s)", lm.Filename, lm.Publisher, lm.ModelName)
		}
		modelOptions = append(modelOptions, huh.NewOption(label, lm.Path))
	}

	// runtime options
	runtimeOptions := make([]huh.Option[string], 0, len(runtimes))
	for _, rt := range runtimes {
		label := fmt.Sprintf("%s [%s]", rt.Tag, rt.Backend)
		runtimeOptions = append(runtimeOptions, huh.NewOption(label, rt.DirName))
	}

	// mmproj options
	mmprojOptions := []huh.Option[string]{huh.NewOption("(none)", "")}
	for _, mp := range mmprojs {
		mmprojOptions = append(mmprojOptions, huh.NewOption(filepath.Base(mp), mp))
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
				Value(&vals.profileName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Model").
				Options(modelOptions...).
				Value(&vals.modelPath),
			huh.NewSelect[string]().
				Title("Runtime").
				Options(runtimeOptions...).
				Value(&vals.runtimeDir),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Context Size (empty = default)").
				Value(&vals.contextSize).
				Validate(numValidator),
			huh.NewInput().
				Title("GPU Layers (empty = default)").
				Value(&vals.gpuLayers).
				Validate(numValidator),
			huh.NewConfirm().
				Title("Flash Attention").
				Value(&vals.flashAttn),
			huh.NewConfirm().
				Title("Disable mmap").
				Value(&vals.noMmap),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("mmproj (for vision models)").
				Options(mmprojOptions...).
				Value(&vals.mmprojPath),
			huh.NewText().
				Title("Extra Args (free-form llama-server options)").
				Value(&vals.extraArgs),
		),
	).WithWidth(width).WithShowHelp(true)

	return pf
}

func (pf *profileFormState) toProfile() *profile.Profile {
	v := pf.vals
	ctxSize, _ := strconv.Atoi(v.contextSize)
	gpuLayers, _ := strconv.Atoi(v.gpuLayers)

	return &profile.Profile{
		Name:           strings.TrimSpace(v.profileName),
		ModelPath:      v.modelPath,
		RuntimeDirName: v.runtimeDir,
		ContextSize:    ctxSize,
		GPULayers:      gpuLayers,
		FlashAttention: v.flashAttn,
		NoMmap:         v.noMmap,
		MMProjPath:     v.mmprojPath,
		ExtraArgs:      v.extraArgs,
	}
}

// --- Update ---

func (m Model) updateProfileForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	pf := &m.profileForm

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	formModel, cmd := pf.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		pf.form = f

		if f.State == huh.StateCompleted {
			p := pf.toProfile()
			mgr := m.profManager
			var removeOld bool
			oldName := ""
			if pf.editing != nil && pf.editing.Name != p.Name {
				oldName = pf.editing.Name
				removeOld = true
			}
			m.current = viewLoading
			m.status = fmt.Sprintf("Saving profile \"%s\"...", p.Name)
			return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
				if removeOld {
					mgr.Remove(oldName)
				}
				err := mgr.Save(p)
				return profileSavedMsg{name: p.Name, err: err}
			})
		}
		if f.State == huh.StateAborted {
			m.current = viewMenu
			return m, nil
		}
	}

	return m, cmd
}

// --- View ---

func (m Model) viewProfileForm() string {
	pf := &m.profileForm
	if pf.form != nil {
		return pf.form.View()
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
	m.profileList.Title = "Profiles (enter to edit, d to delete, q to back)"
	m.current = viewProfiles
	return m, nil
}

func (m Model) handleProfileDelete() (tea.Model, tea.Cmd) {
	i, ok := m.profileList.SelectedItem().(menuItem)
	if !ok || i.title == "+ New Profile" {
		return m, nil
	}

	if err := m.profManager.Remove(i.title); err != nil {
		m.status = fmt.Sprintf("Failed to remove: %v", err)
	} else {
		m.status = fmt.Sprintf("Removed profile \"%s\"", i.title)
	}
	// reload profile list
	return m, listProfilesCmd(m.profManager)
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

	m.profileForm = newProfileFormState(msg.editing, msg.models, msg.runtimes, msg.mmprojs, m.width)
	m.current = viewProfileForm
	return m, m.profileForm.form.Init()
}

// --- Commands ---

func listProfilesCmd(mgr *profile.Manager) tea.Cmd {
	return func() tea.Msg {
		profiles, err := mgr.List()
		return profilesMsg{profiles: profiles, err: err}
	}
}
