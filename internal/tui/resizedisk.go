package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// resizePresets are the growth offered as one-key choices; anything else
// (including every shrink) is typed under "Other size…".
var resizePresets = []string{"+10G", "+20G", "+50G", "+100G"}

// resizeOtherRow is the picker row that asks for a typed size.
var resizeOtherRow = len(resizePresets)

const resizeNeedsOff = "Shut the VM down first: its disk can only be resized while it is powered off"

// openResize shows the size picker for vm, or says why it can't.
func (m *Model) openResize(vm qemu.VM, up bool) {
	if up {
		m.setFlash(resizeNeedsOff, true)
		return
	}
	ds := m.disks[vm.ConfPath]
	switch {
	case ds.missing:
		m.setFlash("No disk yet: start the VM once so quickemu creates it", true)
		return
	case !ds.loaded || ds.err != nil:
		m.setFlash("The disk's size isn't known yet: wait for it to load, or press r", true)
		return
	}
	m.mode = modeResize
	m.resVM = vm
	m.resCursor = 0
}

func (m Model) resizeCurrent() int64 { return m.disks[m.resVM.ConfPath].info.VirtualSize }

func (m Model) handleResizeKey(key string) (tea.Model, tea.Cmd) {
	rows := len(resizePresets) + 1
	switch key {
	case "ctrl+c":
		return m.interruptQuit()
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.resCursor = clamp(m.resCursor-1, 0, rows-1)
	case "down", "j":
		m.resCursor = clamp(m.resCursor+1, 0, rows-1)
	case "enter":
		return m.chooseResize(m.resCursor)
	}
	return m, nil
}

func (m Model) chooseResize(row int) (tea.Model, tea.Cmd) {
	if m.busy > 0 {
		m.setFlash("Wait for the current operation to finish", true)
		return m, nil
	}
	vm := m.resVM
	if row < len(resizePresets) {
		return m.planResize(vm, resizePresets[row])
	}
	title := fmt.Sprintf("New size for %s (now %s; for example 64G, +20G, or -5G to shrink)", vm.Name(), qemu.HumanSize(m.resizeCurrent()))
	cmd := m.askPrompt(title, "Next", "", func(m *Model, spec string) tea.Cmd {
		next, cmd := m.planResize(vm, spec)
		*m = next.(Model)
		return cmd
	})
	return m, cmd
}

// planResize works out what spec means for vm and asks for confirmation. A bad
// size is reported in a flash and the picker stays open.
func (m Model) planResize(vm qemu.VM, spec string) (tea.Model, tea.Cmd) {
	plan, err := qemu.PlanResize(vm, spec)
	if err != nil {
		m.setFlash(firstLine(err.Error()), true)
		return m, nil
	}
	m.askConfirm(resizeConfirmText(vm.Name(), plan), resizeConfirmDefault(plan), func(m *Model) tea.Cmd {
		m.mode = modeNormal
		verb := "Grow"
		if plan.Shrinks() {
			verb = "Shrink"
		}
		label := fmt.Sprintf("%s %s's disk to %s", verb, vm.Name(), qemu.HumanSize(plan.Target))
		return m.startOp(label, vm, opDoneMsg{refreshDisk: true}, func() error {
			return qemu.ResizeDisk(vm, plan)
		})
	})
	return m, nil
}

// resizeConfirmDefault is no for a shrink, so a stray enter can't destroy data.
func resizeConfirmDefault(plan qemu.ResizePlan) confirmDefault {
	if plan.Shrinks() {
		return defaultNo
	}
	return defaultYes
}

// resizeConfirmText spells out what the resize does and doesn't do.
func resizeConfirmText(name string, plan qemu.ResizePlan) string {
	var b strings.Builder
	cur, target := qemu.HumanSize(plan.Current), qemu.HumanSize(plan.Target)
	if plan.Shrinks() {
		fmt.Fprintf(&b, "%s\n\n", errStyle.Render(fmt.Sprintf("SHRINK %s's disk from %s to %s?", name, cur, target)))
		fmt.Fprintf(&b, "This permanently destroys everything stored beyond %s. If the guest's partitions or filesystems reach past that, it will be corrupted or won't boot. Shrink them from inside the guest first, and keep a backup.", target)
	} else {
		fmt.Fprintf(&b, "Grow %s's disk from %s to %s?\n\n", name, cur, target)
		b.WriteString("The guest's partitions and filesystems are not grown: an installer will use the new space, but an installed system needs them grown from inside the guest.")
	}
	if plan.Snapshots > 0 {
		fmt.Fprintf(&b, "\n\nThis disk has %s. Reverting to a snapshot restores the disk size it was taken at, undoing this resize; take a new snapshot after resizing.", plural(plan.Snapshots, "snapshot"))
	}
	return b.String()
}

func (m Model) viewResize() string {
	lines := []string{titleStyle.Render("Resize disk: " + m.resVM.Name()), dimStyle.Render("now " + qemu.HumanSize(m.resizeCurrent())), ""}
	for i, spec := range resizePresets {
		d, _ := qemu.ParseDiskSize(spec)
		line := fmt.Sprintf(" %-6s → %s ", spec, qemu.HumanSize(m.resizeCurrent()+d.Bytes))
		if i == m.resCursor {
			line = selStyle.Render(line)
		}
		lines = append(lines, line)
	}
	other := " Other size… "
	if m.resCursor == resizeOtherRow {
		other = selStyle.Render(other)
	}
	return strings.Join(append(lines, other), "\n")
}
