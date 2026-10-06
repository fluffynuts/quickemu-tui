package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

const labelWidth = 10

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	selStyle     = lipgloss.NewStyle().Reverse(true)
	underStyle   = lipgloss.NewStyle().Underline(true)
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Width(labelWidth)
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	paneStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	activePane   = paneStyle.BorderForeground(lipgloss.Color("12"))
	modalStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(1, 2)
)

const shortKeys = "↑↓ select • tab pane • enter actions • n new VM • d defaults • r refresh • ? help • q quit"

const fullHelp = `  d  default VM options (e.g. gl="off"): added to new VMs, and to existing ones on start if unset
  n  install a new VM (quickget): pick OS, release, edition; shows download progress
  ↑/k ↓/j  move          tab    switch VM list / snapshot list      r  refresh
  enter    open the actions menu for the selected VM. Inside it, press an item's key
           (s start, p shutdown, K force stop, c create, a revert, d delete snapshots, m media,
           Q quick settings, e edit, l logs, x ssh, o open folder, R rename VM, D delete VM) or move to it and press enter.
  ?  toggle help         q      quit (VMs keep running)`

// View renders the UI.
func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	header := m.viewHeader()
	footer := m.viewFooter()
	bodyHeight := max(5, m.height-lipgloss.Height(header)-lipgloss.Height(footer))

	var body string
	switch m.mode {
	case modeNormal:
		body = m.viewMain(bodyHeight)
	case modeMenu:
		body = overlay(m.viewMain(bodyHeight), m.viewMenu(), m.width, bodyHeight)
	default:
		body = lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, m.viewModal())
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) viewHeader() string {
	h := titleStyle.Render("quickemu-tui") + "  " + dimStyle.Render(tildify(m.opts.Root))
	if m.busy > 0 || len(m.launching) > 0 {
		h += "  " + m.spin.View()
	}
	h += m.installHeader()
	return withRightText(h, m.versionLabel(), m.width)
}

// versionLabel is the header's version, e.g. "v0.1.5" ("dev" for local builds).
func (m Model) versionLabel() string {
	v := m.opts.Version
	if v == "" || v == "dev" {
		return v
	}
	return "v" + v
}

// withRightText pads line so text sits at its right edge, within width. The
// text is dropped when the line is too long to leave room for it.
func withRightText(line, text string, width int) string {
	if text == "" {
		return line
	}
	gap := width - lipgloss.Width(line) - lipgloss.Width(text)
	if gap < 2 {
		return line
	}
	return line + strings.Repeat(" ", gap) + dimStyle.Render(text)
}

func (m Model) viewFooter() string {
	flash := ""
	if m.flash != "" {
		style := okStyle
		if m.flashErr {
			style = errStyle
		}
		flash = style.Render(trunc(m.flash, m.width))
	}
	return flash + "\n" + m.viewKeys()
}

func (m Model) viewKeys() string {
	keys := shortKeys
	switch m.mode {
	case modePrompt:
		keys = "enter confirm • esc cancel"
	case modeConfirm:
		keys = "y yes • n no • enter no"
		if m.confirmDefault == defaultYes {
			keys = "y yes • n no • enter yes"
		}
	case modeMedia:
		keys = "↑↓ select • enter/c insert image • e eject • r refresh • esc close"
	case modeDefaults:
		keys = "ctrl+s save • esc discard"
	case modeQuickSettings:
		keys = "↑↓ choose • space tick • tab/←→ switch • enter/esc save and close"
	case modeInstallPick:
		keys = "type to filter • ↑↓ pgup pgdn • enter select • esc back"
	case modeInstallProgress:
		keys = "esc keep running in background • c cancel"
	case modeError:
		keys = "↑↓ pgup pgdn home end scroll • esc/enter close"
	case modeSnapDelete:
		keys = "↑↓ move • space tick • a all/none • enter delete ticked • esc cancel"
	case modeSnapRevert:
		keys = "↑↓ move • enter revert • s revert and start • esc cancel"
	case modeMenu:
		keys = "↑↓ select • enter or item key run • esc close"
	case modeLogs:
		keys = "tab next file • r reload • ↑↓ pgup pgdn scroll • esc close"
	default:
		if m.showHelp {
			return dimStyle.Render(fullHelp)
		}
	}
	return dimStyle.Render(trunc(keys, m.width))
}

func (m Model) viewMain(h int) string {
	listW := 40
	if m.width < 90 {
		listW = max(21, m.width*4/9)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.viewVMList(listW, h), m.viewDetail(m.width-listW, h))
}

func paneFor(active bool) lipgloss.Style {
	if active {
		return activePane
	}
	return paneStyle
}

func (m Model) viewVMList(w, h int) string {
	inner := w - 4
	rows := make([]string, 0, len(m.vms))
	for i, vm := range m.vms {
		info := m.infos[vm.ConfPath]
		name := trunc(vm.Name(), inner-2)
		if i == m.cursor {
			if m.pane == paneVMs {
				name = selStyle.Render(name)
			} else {
				name = underStyle.Render(name)
			}
		}
		rows = append(rows, stateDot(info.status, m.launching[vm.ConfPath])+" "+name)
	}
	if len(rows) == 0 {
		rows = append(rows, dimStyle.Render(trunc("no VMs yet: n installs one", inner)))
	}
	content := titleStyle.Render("VMs") + "\n" + strings.Join(windowAround(rows, m.cursor, h-3), "\n")
	return paneFor(m.pane == paneVMs).Width(w - 2).Height(h - 2).Render(content)
}

func (m Model) viewDetail(w, h int) string {
	inner := w - 4
	style := paneFor(m.pane == paneSnapshots).Width(w - 2).Height(h - 2)
	vm, ok := m.selected()
	if !ok {
		return style.Render(dimStyle.Render("Select a VM"))
	}

	info := m.infos[vm.ConfPath]
	launching := m.launching[vm.ConfPath] && !info.status.IsUp()
	stateText := info.status.State.String()
	if launching {
		stateText = "starting…"
	}
	lines := []string{titleStyle.Render(trunc(vm.Name(), inner/2)) + "  " + stateDot(info.status, launching) + " " + stateText}
	row := func(label, value string) {
		lines = append(lines, labelStyle.Render(label)+trunc(value, inner-labelWidth))
	}

	switch {
	case info.err != nil:
		lines = append(lines, errStyle.Render(trunc(info.err.Error(), inner)))
	case info.conf == nil:
		lines = append(lines, dimStyle.Render("loading…"))
	default:
		row("conf", tildify(vm.ConfPath))
		row("disk", m.diskSummary(vm, info.paths.Disk))
		row("cpu/ram", or(info.conf["cpu_cores"], "auto")+" cores, "+or(info.conf["ram"], "auto")+" RAM")
		row("display", or(info.conf["display"], "default")+", gl "+or(info.conf["gl"], "default"))
		if port, ok := info.ports["ssh"]; ok && info.status.IsUp() {
			row("ssh", fmt.Sprintf("localhost:%d (x to connect)", port))
		}
		if info.status.State == qemu.Busy {
			lines = append(lines, warnStyle.Render(trunc(info.status.Detail, inner)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, m.viewSnapshots(vm, inner, h-2-len(lines))...)
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) diskSummary(vm qemu.VM, disk string) string {
	ds := m.disks[vm.ConfPath]
	s := tildify(disk)
	switch {
	case !ds.loaded:
		return s
	case ds.missing:
		return s + "  (not created yet)"
	case ds.err != nil:
		return s + "  (" + firstLine(ds.err.Error()) + ")"
	}
	return fmt.Sprintf("%s  %s used of %s", s, qemu.HumanSize(ds.info.ActualSize), qemu.HumanSize(ds.info.VirtualSize))
}

func (m Model) viewSnapshots(vm qemu.VM, inner, rows int) []string {
	title := titleStyle.Render("Snapshots")
	if m.isUp(vm) {
		title += dimStyle.Render("  (shut down to create/revert/delete)")
	}
	out := []string{title}
	ds := m.disks[vm.ConfPath]
	switch {
	case !ds.loaded:
		return append(out, dimStyle.Render("loading…"))
	case ds.missing:
		return append(out, dimStyle.Render(trunc("no disk yet: start the VM to install, then snapshot it", inner)))
	case ds.err != nil:
		return append(out, errStyle.Render(trunc(ds.err.Error(), inner)))
	case len(ds.info.Snapshots) == 0:
		return append(out, dimStyle.Render(trunc("none yet: c creates one (e.g. 'pristine')", inner)))
	}

	const format = "%-4s %-22s %-16s %s"
	out = append(out, dimStyle.Render(trunc(fmt.Sprintf(format, "ID", "TAG", "DATE", "VM STATE"), inner)))
	items := make([]string, 0, len(ds.info.Snapshots))
	for i, s := range ds.info.Snapshots {
		line := trunc(fmt.Sprintf(format, s.ID, s.Name, s.Date.Format("2006-01-02 15:04"), qemu.HumanSize(s.VMStateSize)), inner)
		if i == m.snapCursor && m.pane == paneSnapshots {
			line = selStyle.Render(line)
		}
		items = append(items, line)
	}
	return append(out, windowAround(items, m.snapCursor, rows-len(out))...)
}

func (m Model) viewModal() string {
	switch m.mode {
	case modePrompt:
		return modalStyle.Render(titleStyle.Render(m.promptTitle) + "\n\n" + m.input.View())
	case modeConfirm:
		return modalStyle.Width(max(30, min(70, m.width-4))).Render(m.confirmText + "\n\n" + dimStyle.Render(m.confirmChoices()))
	case modeMedia:
		return modalStyle.Render(m.viewMedia())
	case modeSnapDelete:
		return modalStyle.Render(m.viewSnapDelete())
	case modeSnapRevert:
		return modalStyle.Render(m.viewSnapRevert())
	case modeDefaults:
		return modalStyle.Render(m.viewDefaults())
	case modeQuickSettings:
		return modalStyle.Render(m.viewQuickSettings())
	case modeInstallPick:
		return modalStyle.Render(m.viewInstallPick())
	case modeInstallProgress:
		return modalStyle.Render(m.viewInstallProgress())
	case modeError:
		return modalStyle.BorderForeground(lipgloss.Color("9")).Padding(0, 1).Render(errStyle.Render(m.errTitle) + "\n\n" + m.errView.View())
	case modeLogs:
		return modalStyle.Padding(0, 1).Render(m.viewLogs())
	}
	return ""
}

// confirmChoices is the y/n hint, with the enter default capitalised.
func (m Model) confirmChoices() string {
	if m.confirmDefault == defaultYes {
		return "Y/n"
	}
	return "y/N"
}

func (m Model) viewMedia() string {
	lines := []string{titleStyle.Render("Removable media: " + m.mediaVM.Name()), ""}
	switch {
	case m.mediaLoading && len(m.media) == 0:
		lines = append(lines, dimStyle.Render("asking the QEMU monitor…"))
	case m.mediaErr != nil:
		lines = append(lines, errStyle.Render(m.mediaErr.Error()))
	case len(m.media) == 0:
		lines = append(lines, dimStyle.Render("no removable drives"))
	}
	for i, d := range m.media {
		file := d.File
		if file == "" {
			file = "(empty)"
		}
		line := fmt.Sprintf("%-10s %s", d.Name, trunc(file, max(20, m.width-30)))
		if i == m.mediaCursor {
			line = selStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m Model) viewLogs() string {
	tabs := make([]string, len(logNames))
	for i, name := range logNames {
		if i == m.logIndex {
			tabs[i] = selStyle.Render(" " + name + " ")
		} else {
			tabs[i] = dimStyle.Render(" " + name + " ")
		}
	}
	vmName := ""
	if vm, ok := m.selected(); ok {
		vmName = vm.Name()
	}
	return titleStyle.Render("Logs: "+vmName) + "  " + strings.Join(tabs, " ") + "\n" + m.logView.View()
}

// --- helpers ----------------------------------------------------------------

func stateDot(st qemu.Status, launching bool) string {
	switch {
	case launching && !st.IsUp():
		return warnStyle.Render("◌")
	case st.State == qemu.Paused:
		return warnStyle.Render("●")
	case st.IsUp():
		return okStyle.Render("●")
	}
	return dimStyle.Render("○")
}

// windowAround keeps the cursor visible when there are more lines than rows.
func windowAround(lines []string, cursor, rows int) []string {
	if rows <= 0 {
		return nil
	}
	if len(lines) <= rows {
		return lines
	}
	start := max(0, cursor-rows+1)
	if start+rows > len(lines) {
		start = len(lines) - rows
	}
	return lines[start : start+rows]
}

// trunc shortens plain (unstyled) text to n runes.
func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func tildify(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
