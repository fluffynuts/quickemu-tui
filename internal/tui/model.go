// Package tui is the Bubble Tea front end. All VM logic lives in package qemu;
// everything slow (monitor calls, qemu-img) runs inside tea.Cmds so the UI
// never blocks.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

const (
	pollInterval  = 2500 * time.Millisecond
	statusTimeout = time.Second
	logTailBytes  = 128 * 1024
)

type mode int

const (
	modeNormal mode = iota
	modePrompt
	modeConfirm
	modeMedia
	modeLogs
	modeMenu
	modeSnapDelete
	modeSnapRevert
	modeError
	modeInstallPick
	modeInstallProgress
	modeDefaults
)

type pane int

const (
	paneVMs pane = iota
	paneSnapshots
)

// Options configure the TUI.
type Options struct {
	Root       string   // directory holding quickemu *.conf files
	Quickemu   string   // explicit quickemu path; empty means look it up on PATH
	CachePath  string   // where the installable-systems list is cached; empty disables caching
	ConfigPath string   // user config file; empty if there is nowhere to save settings
	Defaults   []string // default .conf lines, merged into VMs (see qemu.MergeDefaults)
	Version    string   // shown in the header's top-right corner; empty hides it
}

// vmInfo is gathered off the UI thread on every poll, so View() never does IO.
type vmInfo struct {
	status qemu.Status
	conf   map[string]string
	paths  qemu.Paths
	ports  map[string]int
	err    error
}

type diskState struct {
	loaded  bool
	missing bool
	info    qemu.DiskInfo
	err     error
}

// Model is the Bubble Tea model.
type Model struct {
	opts      Options
	vms       []qemu.VM
	infos     map[string]vmInfo
	disks     map[string]diskState
	launching map[string]bool

	cursor     int
	snapCursor int
	pane       pane
	mode       mode
	menuCursor int
	returnMode mode
	showHelp   bool
	width      int
	height     int

	busy         int
	spin         spinner.Model
	spinning     bool
	pollInFlight bool

	flash    string
	flashErr bool

	input       textinput.Model
	promptTitle string
	onSubmit    func(m *Model, value string) tea.Cmd

	confirmText    string
	confirmDefault confirmDefault
	onConfirm      func(m *Model) tea.Cmd

	mediaVM      qemu.VM
	media        []qemu.BlockDevice
	mediaCursor  int
	mediaErr     error
	mediaLoading bool

	delVM     qemu.VM
	delCursor int
	delPicked map[string]bool // snapshot key -> ticked

	revVM     qemu.VM
	revCursor int

	defaults []string
	defInput textarea.Model
	defErr   string

	catalog        qemu.Catalog
	catalogLoading bool
	catalogErr     error // the last quickget fetch failed with this
	instStep       installStep
	instFilter     string
	instCursor     int
	instOS         string
	instRelease    string
	install        *installState

	errTitle  string
	errReturn mode
	errBody   string
	errView   viewport.Model

	logIndex int
	logView  viewport.Model
}

var logNames = [...]string{"quickemu log", "launch output", "launch script"}

// --- messages ---------------------------------------------------------------

type tickMsg time.Time

type pollNowMsg struct{}

type infoMsg map[string]vmInfo

type diskMsg struct {
	conf  string
	state diskState
}

type launchExitedMsg struct {
	conf string
	err  error
}

type mediaMsg struct {
	devices []qemu.BlockDevice
	err     error
}

type execDoneMsg struct {
	label string
	conf  string
	err   error
}

type opDoneMsg struct {
	label        string
	conf         string
	err          error
	refreshDisk  bool
	refreshMedia bool
	startAfter   bool
	forgetVM     bool // the VM was deleted: drop what we know about it and rescan
}

// --- construction -----------------------------------------------------------

// New builds the initial model.
func New(opts Options) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	ti := textinput.New()
	ti.CharLimit = 4096
	ti.Prompt = "› "

	m := Model{
		opts:         opts,
		infos:        make(map[string]vmInfo),
		disks:        make(map[string]diskState),
		launching:    make(map[string]bool),
		spin:         sp,
		input:        ti,
		logView:      viewport.New(80, 20),
		errView:      viewport.New(80, 10),
		defaults:     opts.Defaults,
		defInput:     newDefaultsInput(),
		pollInFlight: true, // Init issues the first poll
	}
	m.catalogLoading = true // Init starts the fetch
	m.rediscover()
	return m
}

// Init starts polling.
func (m Model) Init() tea.Cmd {
	return batch(tick(), gatherCmd(m.vms), m.loadSelectedDisk(),
		readCatalogCacheCmd(m.opts.CachePath), fetchCatalogCmd(m.opts.Quickemu, m.opts.CachePath))
}

// --- commands ---------------------------------------------------------------

// batch is tea.Batch that tolerates nil commands.
func batch(cmds ...tea.Cmd) tea.Cmd {
	var live []tea.Cmd
	for _, c := range cmds {
		if c != nil {
			live = append(live, c)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return tea.Batch(live...)
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func gatherCmd(vms []qemu.VM) tea.Cmd {
	vms = append([]qemu.VM(nil), vms...)
	return func() tea.Msg {
		statuses := qemu.QueryAll(vms, statusTimeout)
		infos := make(map[string]vmInfo, len(vms))
		for _, vm := range vms {
			info := vmInfo{status: statuses[vm.ConfPath]}
			info.conf, info.err = vm.Conf()
			if info.err == nil {
				info.paths, info.err = vm.Paths()
			}
			info.ports = qemu.ReadPorts(vm)
			infos[vm.ConfPath] = info
		}
		return infoMsg(infos)
	}
}

func loadDisk(vm qemu.VM) tea.Cmd {
	return func() tea.Msg {
		p, err := vm.Paths()
		if err != nil {
			return diskMsg{conf: vm.ConfPath, state: diskState{loaded: true, err: err}}
		}
		if _, err := os.Stat(p.Disk); os.IsNotExist(err) {
			return diskMsg{conf: vm.ConfPath, state: diskState{loaded: true, missing: true}}
		}
		info, err := qemu.GetDiskInfo(p.Disk)
		return diskMsg{conf: vm.ConfPath, state: diskState{loaded: true, info: info, err: err}}
	}
}

func loadMedia(vm qemu.VM) tea.Cmd {
	return func() tea.Msg {
		devices, err := qemu.ListBlockDevices(vm)
		return mediaMsg{devices: devices, err: err}
	}
}

func editorCommand(path string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "nano"
	}
	parts := strings.Fields(editor) // allows e.g. EDITOR="code -w"
	return exec.Command(parts[0], append(parts[1:], path)...)
}

// --- model helpers ----------------------------------------------------------

func (m Model) selected() (qemu.VM, bool) {
	if m.cursor < 0 || m.cursor >= len(m.vms) {
		return qemu.VM{}, false
	}
	return m.vms[m.cursor], true
}

func (m Model) vmByConf(conf string) (qemu.VM, bool) {
	for _, vm := range m.vms {
		if vm.ConfPath == conf {
			return vm, true
		}
	}
	return qemu.VM{}, false
}

// isUp counts a VM we've just launched as up, so nobody snapshots it mid-boot.
func (m Model) isUp(vm qemu.VM) bool {
	return m.infos[vm.ConfPath].status.IsUp() || m.launching[vm.ConfPath]
}

func (m Model) snapshots(vm qemu.VM) []qemu.Snapshot {
	ds := m.disks[vm.ConfPath]
	if !ds.loaded || ds.missing || ds.err != nil {
		return nil
	}
	return ds.info.Snapshots
}

func (m Model) loadSelectedDisk() tea.Cmd {
	if vm, ok := m.selected(); ok {
		return loadDisk(vm)
	}
	return nil
}

func (m *Model) setFlash(text string, isErr bool) {
	m.flash = text
	m.flashErr = isErr
}

func (m *Model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spin.Tick
}

func (m *Model) pollNow() tea.Cmd {
	if m.pollInFlight {
		return nil
	}
	m.pollInFlight = true
	return gatherCmd(m.vms)
}

func (m *Model) rediscover() {
	vms, err := qemu.Discover(m.opts.Root)
	if err != nil {
		m.setFlash(err.Error(), true)
		return
	}
	current := ""
	if vm, ok := m.selected(); ok {
		current = vm.ConfPath
	}
	previous := m.cursor
	m.vms = vms
	m.cursor = clamp(previous, 0, len(vms)-1) // if the selected VM is gone, stay near it
	for i, vm := range vms {
		if vm.ConfPath == current {
			m.cursor = i
		}
	}
}

// startOp runs fn in the background and reports back with an opDoneMsg.
func (m *Model) startOp(label string, vm qemu.VM, after opDoneMsg, fn func() error) tea.Cmd {
	m.busy++
	m.setFlash(label+"…", false)
	after.label = label
	after.conf = vm.ConfPath
	return batch(m.ensureSpin(), func() tea.Msg {
		after.err = fn()
		return after
	})
}

func (m *Model) startVM(vm qemu.VM) tea.Cmd {
	q, err := qemu.FindQuickemu(m.opts.Quickemu)
	if err != nil {
		m.setFlash("quickemu not found on PATH (pass -quickemu)", true)
		return nil
	}
	// fill in the user's default options the conf hasn't set (never overriding it)
	added, mergeErr := applyDefaultsTo(m.defaults, vm.ConfPath)
	cmd, err := qemu.Start(vm, q)
	if err != nil {
		m.setFlash("Starting "+vm.Name()+" failed: "+firstLine(err.Error()), true)
		m.showError("Starting "+vm.Name()+" failed", err.Error())
		return nil
	}
	conf := vm.ConfPath
	m.launching[conf] = true
	m.setFlash("Starting "+vm.Name()+"…"+defaultsNote(added, mergeErr), mergeErr != nil)
	return batch(
		m.ensureSpin(),
		func() tea.Msg { return launchExitedMsg{conf: conf, err: cmd.Wait()} },
		tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return pollNowMsg{} }),
	)
}

func (m *Model) askPrompt(title, initial string, onSubmit func(m *Model, value string) tea.Cmd) tea.Cmd {
	m.returnMode = m.mode
	m.mode = modePrompt
	m.promptTitle = title
	m.onSubmit = onSubmit
	m.input.Width = max(20, min(70, m.width-16))
	m.input.SetValue(initial)
	m.input.CursorEnd()
	return m.input.Focus()
}

// confirmDefault is what enter means in a y/n dialog: no for anything
// destructive, so a stray enter can't lose data.
type confirmDefault bool

const (
	defaultNo  confirmDefault = false
	defaultYes confirmDefault = true
)

func (m *Model) askConfirm(text string, def confirmDefault, onYes func(m *Model) tea.Cmd) {
	m.returnMode = m.mode
	m.mode = modeConfirm
	m.confirmText = text
	m.confirmDefault = def
	m.onConfirm = onYes
}

func (m *Model) move(delta int) tea.Cmd {
	if m.pane == paneSnapshots {
		if vm, ok := m.selected(); ok {
			m.snapCursor = clamp(m.snapCursor+delta, 0, len(m.snapshots(vm))-1)
		}
		return nil
	}
	next := clamp(m.cursor+delta, 0, len(m.vms)-1)
	if next == m.cursor {
		return nil
	}
	m.cursor = next
	m.snapCursor = 0
	if vm, ok := m.selected(); ok && !m.disks[vm.ConfPath].loaded {
		return loadDisk(vm)
	}
	return nil
}

func (m *Model) loadLog() {
	vm, ok := m.selected()
	if !ok {
		m.logView.SetContent("")
		return
	}
	p, err := vm.Paths()
	if err != nil {
		m.logView.SetContent(err.Error())
		return
	}
	files := [...]string{p.QuickemuLog, p.LaunchLog, p.LaunchScript}
	text := strings.ReplaceAll(qemu.ReadTail(files[m.logIndex], logTailBytes), "\t", "    ")
	m.logView.SetContent(lipgloss.NewStyle().Width(m.logView.Width).Render(text))
	m.logView.GotoBottom()
}

func clamp(v, lo, hi int) int {
	if hi < lo || v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- update -----------------------------------------------------------------

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.logView.Width = max(20, msg.Width-6)
		m.logView.Height = max(3, msg.Height-7)
		m.sizeErrView()
		if m.mode == modeLogs {
			m.loadLog()
		}
		return m, nil

	case spinner.TickMsg:
		if m.busy == 0 && len(m.launching) == 0 {
			m.spinning = false // let the tick loop die while idle
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		m.rediscover()
		return m, batch(tick(), m.pollNow())

	case catalogMsg:
		return m.onCatalog(msg)

	case installProgressMsg:
		return m.onInstallProgress(msg)

	case installDoneMsg:
		return m.onInstallDone(msg)

	case pollNowMsg:
		return m, m.pollNow()

	case infoMsg:
		m.pollInFlight = false
		m.infos = map[string]vmInfo(msg)
		for conf := range m.launching {
			if m.infos[conf].status.IsUp() {
				delete(m.launching, conf)
			}
		}
		return m, m.loadSelectedDisk() // keeps disk usage + snapshot list fresh

	case diskMsg:
		m.disks[msg.conf] = msg.state
		if vm, ok := m.selected(); ok && vm.ConfPath == msg.conf {
			m.snapCursor = clamp(m.snapCursor, 0, len(m.snapshots(vm))-1)
		}
		return m, nil

	case opDoneMsg:
		m.busy--
		if msg.err != nil {
			m.setFlash(msg.label+" failed: "+firstLine(msg.err.Error()), true)
			m.showError(msg.label+" failed", msg.err.Error())
		} else {
			m.setFlash(msg.label+" ✓", false)
		}
		if msg.forgetVM && msg.err == nil {
			delete(m.infos, msg.conf)
			delete(m.disks, msg.conf)
			m.snapCursor = 0
			m.rediscover()
		}
		cmds := []tea.Cmd{m.pollNow()}
		if msg.forgetVM && msg.err == nil {
			cmds = append(cmds, m.loadSelectedDisk())
		}
		if vm, ok := m.vmByConf(msg.conf); ok {
			if msg.refreshDisk {
				cmds = append(cmds, loadDisk(vm))
			}
			if msg.refreshMedia && m.mode == modeMedia {
				m.mediaLoading = true
				cmds = append(cmds, loadMedia(vm))
			}
			if msg.startAfter && msg.err == nil {
				cmds = append(cmds, m.startVM(vm))
			}
		}
		return m, batch(cmds...)

	case launchExitedMsg:
		delete(m.launching, msg.conf)
		if msg.err != nil {
			detail := ""
			if vm, ok := m.vmByConf(msg.conf); ok {
				if p, err := vm.Paths(); err == nil {
					detail = qemu.LastLine(qemu.ReadTail(p.LaunchLog, 64*1024))
				}
			}
			m.setFlash(fmt.Sprintf("quickemu exited (%v): %s", msg.err, detail), true)
			m.showError("quickemu exited", fmt.Sprintf("%v\n\n%s", msg.err, detail))
		}
		return m, m.pollNow()

	case mediaMsg:
		m.mediaLoading = false
		m.mediaErr = msg.err
		m.media = m.media[:0]
		for _, d := range msg.devices {
			if d.Removable {
				m.media = append(m.media, d)
			}
		}
		m.mediaCursor = clamp(m.mediaCursor, 0, len(m.media)-1)
		return m, nil

	case execDoneMsg:
		if msg.err != nil {
			m.setFlash(msg.label+": "+firstLine(msg.err.Error()), true)
			m.showError(msg.label+" failed", msg.err.Error())
		} else {
			m.setFlash(msg.label+" done", false)
		}
		cmds := []tea.Cmd{m.pollNow()}
		if vm, ok := m.vmByConf(msg.conf); ok {
			cmds = append(cmds, loadDisk(vm))
		}
		return m, batch(cmds...)

	case tea.KeyMsg:
		switch m.mode {
		case modePrompt:
			return m.handlePromptKey(msg)
		case modeConfirm:
			return m.handleConfirmKey(msg.String())
		case modeMedia:
			return m.handleMediaKey(msg.String())
		case modeLogs:
			return m.handleLogsKey(msg)
		case modeMenu:
			return m.handleMenuKey(msg.String())
		case modeSnapDelete:
			return m.handleSnapDeleteKey(msg.String())
		case modeSnapRevert:
			return m.handleSnapRevertKey(msg.String())
		case modeError:
			return m.handleErrorKey(msg)
		case modeInstallPick:
			return m.handleInstallPickKey(msg)
		case modeInstallProgress:
			return m.handleInstallProgressKey(msg.String())
		case modeDefaults:
			return m.handleDefaultsKey(msg)
		default:
			return m.handleNormalKey(msg.String())
		}
	}

	// anything else (cursor blink, mouse, ...) goes to the active component
	var cmd tea.Cmd
	switch m.mode {
	case modePrompt:
		m.input, cmd = m.input.Update(msg)
	case modeLogs:
		m.logView, cmd = m.logView.Update(msg)
	case modeError:
		m.errView, cmd = m.errView.Update(msg)
	case modeDefaults:
		m.defInput, cmd = m.defInput.Update(msg)
	}
	return m, cmd
}

func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.install != nil && m.mode != modeConfirm {
		m.askConfirm("A VM install ("+m.install.title+") is still downloading. Quit and stop it?\nPartly downloaded files are kept so it can resume.", defaultNo, func(m *Model) tea.Cmd {
			m.stopInstall()
			return tea.Quit
		})
		return m, nil
	}
	if m.busy > 0 && m.mode != modeConfirm {
		m.askConfirm("An operation is still running. Quit anyway?", defaultNo, func(*Model) tea.Cmd { return tea.Quit })
		return m, nil
	}
	return m, tea.Quit
}

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "esc":
		m.mode = m.returnMode
		m.input.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		m.mode = m.returnMode
		m.input.Blur()
		fn := m.onSubmit
		m.onSubmit = nil
		if fn == nil {
			return m, nil
		}
		return m, fn(&m, value)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		if m.confirmDefault == defaultYes {
			return m.acceptConfirm()
		}
		m.declineConfirm()
	case "y", "Y":
		return m.acceptConfirm()
	case "n", "N", "esc", "q":
		m.declineConfirm()
	}
	return m, nil
}

func (m Model) acceptConfirm() (tea.Model, tea.Cmd) {
	m.mode = m.returnMode
	fn := m.onConfirm
	m.onConfirm = nil
	if fn == nil {
		return m, nil
	}
	return m, fn(&m)
}

func (m *Model) declineConfirm() {
	m.mode = m.returnMode
	m.onConfirm = nil
}

var mutatingKeys = map[string]bool{
	"s": true, "p": true, "K": true, "c": true, "a": true, "d": true, "e": true, "D": true,
}

func (m Model) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m.requestQuit()
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "tab", "shift+tab":
		if m.pane == paneVMs {
			m.pane = paneSnapshots
		} else {
			m.pane = paneVMs
		}
		return m, nil
	case "up", "k":
		return m, m.move(-1)
	case "down", "j":
		return m, m.move(1)
	case "r":
		m.rediscover()
		return m, batch(m.pollNow(), m.loadSelectedDisk())
	case "enter":
		m.openMenu()
		return m, nil
	case "n":
		return m, m.openInstall()
	case "d":
		return m, m.openDefaults()
	}
	return m, nil
}

// runAction performs a VM action by its key. Actions are only reachable
// through the actions menu (by choosing the item or pressing its key).
func (m Model) runAction(key string) (tea.Model, tea.Cmd) {
	vm, ok := m.selected()
	if !ok {
		return m, nil
	}
	if mutatingKeys[key] && m.busy > 0 {
		m.setFlash("Wait for the current operation to finish", true)
		return m, nil
	}
	info := m.infos[vm.ConfPath]
	up := m.isUp(vm)
	needsOff := "Shut " + vm.Name() + " down first: snapshots need the VM powered off"

	switch key {
	case "s":
		if up {
			m.setFlash(vm.Name()+" is already running", true)
			return m, nil
		}
		return m, m.startVM(vm)

	case "p":
		if !info.status.IsUp() {
			m.setFlash(vm.Name()+" isn't running", true)
			return m, nil
		}
		if info.status.State == qemu.Busy {
			m.setFlash("The monitor isn't answering (another client attached?); try K", true)
			return m, nil
		}
		return m, m.startOp("ACPI shutdown sent to "+vm.Name(), vm, opDoneMsg{}, func() error {
			return qemu.Shutdown(vm)
		})

	case "K":
		if !up {
			m.setFlash(vm.Name()+" isn't running", true)
			return m, nil
		}
		m.askConfirm("Force-stop "+vm.Name()+"? That's pulling the power cord on the guest.", defaultNo, func(m *Model) tea.Cmd {
			q, err := qemu.FindQuickemu(m.opts.Quickemu)
			if err != nil {
				m.setFlash("quickemu not found on PATH (pass -quickemu)", true)
				return nil
			}
			return m.startOp("Force-stop "+vm.Name(), vm, opDoneMsg{}, func() error {
				return qemu.ForceStop(vm, q)
			})
		})
		return m, nil

	case "m":
		if !info.status.IsUp() || info.status.State == qemu.Busy {
			m.setFlash("Media can only be changed while the VM is running and its monitor answers", true)
			return m, nil
		}
		m.mode = modeMedia
		m.mediaVM = vm
		m.media = nil
		m.mediaErr = nil
		m.mediaCursor = 0
		m.mediaLoading = true
		return m, loadMedia(vm)

	case "e":
		conf := vm.ConfPath
		name := vm.Name()
		return m, tea.ExecProcess(editorCommand(conf), func(err error) tea.Msg {
			return execDoneMsg{label: "Editing " + name + " (changes apply on next start)", conf: conf, err: err}
		})

	case "l":
		m.mode = modeLogs
		m.logIndex = 0
		m.loadLog()
		return m, nil

	case "x":
		port, hasPort := info.ports["ssh"]
		if !hasPort || !info.status.IsUp() {
			m.setFlash("No forwarded ssh port (is the VM running?)", true)
			return m, nil
		}
		conf := vm.ConfPath
		return m, m.askPrompt("SSH into "+vm.Name()+" as user", os.Getenv("USER"), func(m *Model, user string) tea.Cmd {
			if user == "" {
				return nil
			}
			c := exec.Command("ssh", "-p", strconv.Itoa(port), user+"@localhost")
			return tea.ExecProcess(c, func(err error) tea.Msg {
				return execDoneMsg{label: "ssh session", conf: conf, err: err}
			})
		})

	case "o":
		p, err := vm.Paths()
		if err != nil {
			m.setFlash(err.Error(), true)
			return m, nil
		}
		c := exec.Command("xdg-open", p.VMDir)
		if err := c.Start(); err != nil {
			m.setFlash("xdg-open: "+err.Error(), true)
		} else {
			go func() { _ = c.Wait() }()
		}
		return m, nil

	case "c":
		if up {
			m.setFlash(needsOff, true)
			return m, nil
		}
		if ds := m.disks[vm.ConfPath]; ds.missing {
			m.setFlash("No disk yet: start the VM once so quickemu creates it", true)
			return m, nil
		}
		initial := "pristine"
		if len(m.snapshots(vm)) > 0 {
			initial = "snap-" + time.Now().Format("20060102-1504")
		}
		return m, m.askPrompt("New snapshot tag for "+vm.Name(), initial, func(m *Model, tag string) tea.Cmd {
			if err := qemu.ValidateTag(tag); err != nil {
				m.setFlash(err.Error(), true)
				return nil
			}
			return m.startOp("Snapshot '"+tag+"'", vm, opDoneMsg{refreshDisk: true}, func() error {
				return qemu.CreateSnapshot(vm, tag)
			})
		})

	case "D":
		if up {
			m.setFlash("Shut "+vm.Name()+" down first: a running VM can't be deleted", true)
			return m, nil
		}
		plan, err := qemu.PlanDelete(vm)
		if err != nil {
			m.setFlash("Can't delete "+vm.Name()+": "+firstLine(err.Error()), true)
			return m, nil
		}
		m.askConfirm(deleteVMPrompt(vm, plan), defaultNo, func(m *Model) tea.Cmd {
			return m.startOp("Delete "+vm.Name(), vm, opDoneMsg{forgetVM: true}, func() error {
				return qemu.DeleteVM(vm, plan)
			})
		})
		return m, nil

	case "d":
		if up {
			m.setFlash(needsOff, true)
			return m, nil
		}
		if len(m.snapshots(vm)) == 0 {
			m.setFlash("No snapshots to delete", true)
			return m, nil
		}
		m.openSnapDelete(vm)
		return m, nil

	case "a":
		if up {
			m.setFlash(needsOff, true)
			return m, nil
		}
		if len(m.snapshots(vm)) == 0 {
			m.setFlash("No snapshots to revert to", true)
			return m, nil
		}
		m.openSnapRevert(vm)
		return m, nil
	}
	return m, nil
}

func (m Model) handleMediaKey(key string) (tea.Model, tea.Cmd) {
	vm := m.mediaVM
	switch key {
	case "ctrl+c":
		return m.requestQuit()
	case "esc", "q":
		m.mode = modeNormal
		return m, nil
	case "up", "k":
		m.mediaCursor = clamp(m.mediaCursor-1, 0, len(m.media)-1)
		return m, nil
	case "down", "j":
		m.mediaCursor = clamp(m.mediaCursor+1, 0, len(m.media)-1)
		return m, nil
	case "r":
		m.mediaLoading = true
		return m, loadMedia(vm)
	case "enter", "c", "e":
		if m.busy > 0 {
			m.setFlash("Wait for the current operation to finish", true)
			return m, nil
		}
		if m.mediaCursor >= len(m.media) {
			return m, nil
		}
		dev := m.media[m.mediaCursor]
		if key == "e" {
			return m, m.startOp("Eject "+dev.Name, vm, opDoneMsg{refreshMedia: true}, func() error {
				return qemu.EjectMedia(vm, dev.Name)
			})
		}
		initial := ""
		if dev.File != "" {
			initial = vm.Resolve(dev.File)
		} else if p, err := vm.Paths(); err == nil {
			initial = p.VMDir + string(os.PathSeparator)
		}
		return m, m.askPrompt("Image to insert into "+dev.Name, initial, func(m *Model, value string) tea.Cmd {
			if value == "" {
				return nil
			}
			path := vm.Resolve(value)
			if _, err := os.Stat(path); err != nil {
				m.setFlash(err.Error(), true)
				return nil
			}
			return m.startOp("Insert "+filepath.Base(path)+" into "+dev.Name, vm, opDoneMsg{refreshMedia: true}, func() error {
				return qemu.ChangeMedia(vm, dev.Name, path)
			})
		})
	}
	return m, nil
}

func (m Model) handleLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "esc", "q":
		m.mode = modeNormal
		return m, nil
	case "tab":
		m.logIndex = (m.logIndex + 1) % len(logNames)
		m.loadLog()
		return m, nil
	case "shift+tab":
		m.logIndex = (m.logIndex + len(logNames) - 1) % len(logNames)
		m.loadLog()
		return m, nil
	case "r":
		m.loadLog()
		return m, nil
	}
	var cmd tea.Cmd
	m.logView, cmd = m.logView.Update(msg)
	return m, cmd
}
