package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// snapKey identifies a snapshot's checkbox across polls. It is only for the UI:
// what gets passed to qemu-img is the tag (Snapshot.Ref).
func snapKey(s qemu.Snapshot) string {
	if s.ID != "" {
		return s.ID
	}
	return s.Name
}

func (m *Model) openSnapDelete(vm qemu.VM) {
	m.mode = modeSnapDelete
	m.delVM = vm
	m.delCursor = 0
	m.delPicked = make(map[string]bool)
}

func (m Model) handleSnapDeleteKey(key string) (tea.Model, tea.Cmd) {
	snaps := m.snapshots(m.delVM)
	if len(snaps) == 0 { // all gone (or the disk vanished) while the dialog was open
		m.mode = modeNormal
		return m, nil
	}
	m.delCursor = clamp(m.delCursor, 0, len(snaps)-1)
	switch key {
	case "ctrl+c":
		return m.interruptQuit()
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.delCursor = clamp(m.delCursor-1, 0, len(snaps)-1)
	case "down", "j":
		m.delCursor = clamp(m.delCursor+1, 0, len(snaps)-1)
	case " ", "x":
		k := snapKey(snaps[m.delCursor])
		if m.delPicked[k] {
			delete(m.delPicked, k)
		} else {
			m.delPicked[k] = true
		}
	case "a":
		if len(m.pickedSnapshots(snaps)) == len(snaps) {
			m.delPicked = make(map[string]bool)
		} else {
			for _, s := range snaps {
				m.delPicked[snapKey(s)] = true
			}
		}
	case "enter":
		return m.confirmSnapDelete(snaps)
	}
	return m, nil
}

func (m Model) pickedSnapshots(snaps []qemu.Snapshot) []qemu.Snapshot {
	var out []qemu.Snapshot
	for _, s := range snaps {
		if m.delPicked[snapKey(s)] {
			out = append(out, s)
		}
	}
	return out
}

func (m Model) confirmSnapDelete(snaps []qemu.Snapshot) (tea.Model, tea.Cmd) {
	picked := m.pickedSnapshots(snaps)
	if len(picked) == 0 {
		m.setFlash("Tick the snapshots to delete with space", true)
		return m, nil
	}
	if m.busy > 0 {
		m.setFlash("Wait for the current operation to finish", true)
		return m, nil
	}
	vm := m.delVM
	names := make([]string, len(picked))
	refs := make([]string, len(picked))
	for i, s := range picked {
		names[i] = "'" + s.Ref() + "'"
		refs[i] = s.Ref()
	}
	text := fmt.Sprintf("Delete %s of %s?", plural(len(picked), "snapshot"), vm.Name())
	text += "\n\n" + trunc(strings.Join(names, ", "), max(30, min(70, m.width-8)))
	m.askConfirm(text, defaultNo, func(m *Model) tea.Cmd {
		m.mode = modeNormal
		label := "Delete " + plural(len(refs), "snapshot")
		return m.startOp(label, vm, opDoneMsg{refreshDisk: true}, func() error {
			var failed []string
			for i, ref := range refs {
				if err := qemu.DeleteSnapshot(vm, ref); err != nil {
					failed = append(failed, names[i]+": "+err.Error())
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("%d of %d failed:\n\n%s", len(failed), len(refs), strings.Join(failed, "\n\n"))
			}
			return nil
		})
	})
	return m, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func (m Model) viewSnapDelete() string {
	lines := []string{titleStyle.Render("Delete snapshots: " + m.delVM.Name()), ""}
	snaps := m.snapshots(m.delVM)
	cursor := clamp(m.delCursor, 0, len(snaps)-1)
	items := make([]string, len(snaps))
	for i, s := range snaps {
		box := "[ ]"
		if m.delPicked[snapKey(s)] {
			box = "[x]"
		}
		line := fmt.Sprintf(" %s %-22s %s ", box, trunc(s.Ref(), 22), s.Date.Format("2006-01-02 15:04"))
		if i == cursor {
			line = selStyle.Render(line)
		}
		items[i] = line
	}
	rows := max(3, min(len(items), m.height-14))
	lines = append(lines, windowAround(items, cursor, rows)...)
	lines = append(lines, "", dimStyle.Render(fmt.Sprintf("%d of %d ticked", len(m.pickedSnapshots(snaps)), len(snaps))))
	return strings.Join(lines, "\n")
}
