package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// openSnapRevert shows the snapshot picker, starting on whatever is highlighted
// in the snapshot pane.
func (m *Model) openSnapRevert(vm qemu.VM) {
	m.mode = modeSnapRevert
	m.revVM = vm
	m.revCursor = clamp(m.snapCursor, 0, len(m.snapshots(vm))-1)
}

func (m Model) handleSnapRevertKey(key string) (tea.Model, tea.Cmd) {
	snaps := m.snapshots(m.revVM)
	if len(snaps) == 0 { // all gone (or the disk vanished) while the dialog was open
		m.mode = modeNormal
		return m, nil
	}
	m.revCursor = clamp(m.revCursor, 0, len(snaps)-1)
	switch key {
	case "ctrl+c":
		return m.requestQuit()
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.revCursor = clamp(m.revCursor-1, 0, len(snaps)-1)
	case "down", "j":
		m.revCursor = clamp(m.revCursor+1, 0, len(snaps)-1)
	case "enter", "s":
		return m.confirmSnapRevert(snaps[m.revCursor], key == "s")
	}
	return m, nil
}

func (m Model) confirmSnapRevert(snap qemu.Snapshot, startAfter bool) (tea.Model, tea.Cmd) {
	if m.busy > 0 {
		m.setFlash("Wait for the current operation to finish", true)
		return m, nil
	}
	vm := m.revVM
	ref := snap.Ref()
	verb := "Revert"
	if startAfter {
		verb = "Revert and start"
	}
	m.askConfirm(fmt.Sprintf("%s %s from '%s'? The current disk state is discarded.", verb, vm.Name(), ref), defaultNo, func(m *Model) tea.Cmd {
		m.mode = modeNormal
		return m.startOp("Revert to '"+ref+"'", vm, opDoneMsg{refreshDisk: true, startAfter: startAfter}, func() error {
			return qemu.ApplySnapshot(vm, ref)
		})
	})
	return m, nil
}

func (m Model) viewSnapRevert() string {
	lines := []string{titleStyle.Render("Revert to snapshot: " + m.revVM.Name()), ""}
	snaps := m.snapshots(m.revVM)
	cursor := clamp(m.revCursor, 0, len(snaps)-1)
	items := make([]string, len(snaps))
	for i, s := range snaps {
		line := fmt.Sprintf(" %-22s %s ", trunc(s.Ref(), 22), s.Date.Format("2006-01-02 15:04"))
		if i == cursor {
			line = selStyle.Render(line)
		}
		items[i] = line
	}
	rows := max(3, min(len(items), m.height-14))
	lines = append(lines, windowAround(items, cursor, rows)...)
	return strings.Join(lines, "\n")
}
