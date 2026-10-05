package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// menuItem is one entry in the actions menu. Choosing it runs the same code
// as pressing its key while the menu is open.
type menuItem struct {
	key      string
	label    string
	disabled bool // shown dimmed; choosing it still runs, so the user sees why it can't
	gap      bool // blank line before this item
}

// menuItems builds the actions menu for the selected VM, dimming whatever
// doesn't apply in its current state.
func (m Model) menuItems() []menuItem {
	vm, ok := m.selected()
	if !ok {
		return nil
	}
	info := m.infos[vm.ConfPath]
	up := m.isUp(vm)
	monitorUp := info.status.IsUp() && info.status.State != qemu.Busy
	_, hasSSH := info.ports["ssh"]

	noSnap := len(m.snapshots(vm)) == 0

	return []menuItem{
		{key: "s", label: "Start", disabled: up},
		{key: "p", label: "Shut down (ACPI)", disabled: !info.status.IsUp()},
		{key: "K", label: "Force stop", disabled: !up},

		{key: "c", label: "Create snapshot", disabled: up, gap: true},
		{key: "a", label: "Revert to snapshot…", disabled: up || noSnap},
		{key: "d", label: "Delete snapshot…", disabled: up || noSnap},

		{key: "m", label: "Removable media…", disabled: !monitorUp, gap: true},
		{key: "e", label: "Edit .conf"},
		{key: "l", label: "View logs"},
		{key: "x", label: "SSH in", disabled: !hasSSH || !info.status.IsUp()},
		{key: "o", label: "Open VM folder"},

		{key: "D", label: "Delete VM…", disabled: up, gap: true},
	}
}

func (m *Model) openMenu() {
	if _, ok := m.selected(); !ok {
		return
	}
	m.mode = modeMenu
	m.menuCursor = 0
}

func (m Model) handleMenuKey(key string) (tea.Model, tea.Cmd) {
	items := m.menuItems()
	switch key {
	case "ctrl+c":
		return m.requestQuit()
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.menuCursor = clamp(m.menuCursor-1, 0, len(items)-1)
	case "down", "j":
		m.menuCursor = clamp(m.menuCursor+1, 0, len(items)-1)
	case "enter":
		m.mode = modeNormal
		if m.menuCursor < 0 || m.menuCursor >= len(items) {
			return m, nil
		}
		return m.runAction(items[m.menuCursor].key)
	}
	// an item's hotkey is the same as choosing it
	for _, it := range items {
		if it.key == key {
			m.mode = modeNormal
			return m.runAction(key)
		}
	}
	return m, nil
}

func (m Model) viewMenu() string {
	vm, _ := m.selected()
	lines := []string{titleStyle.Render(vm.Name()), ""}
	items := m.menuItems()
	labelW := 0
	for _, it := range items {
		labelW = max(labelW, lipgloss.Width(it.label))
	}
	for i, it := range items {
		if it.gap {
			lines = append(lines, "")
		}
		line := fmt.Sprintf(" %-*s  %s ", labelW, it.label, it.key)
		switch {
		case i == m.menuCursor:
			line = selStyle.Render(line)
		case it.disabled:
			line = dimStyle.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", dimStyle.Render("↑↓ select • enter/key run • esc close"))
	return modalStyle.Render(strings.Join(lines, "\n"))
}

// overlay draws box centred on top of base, leaving the rest of base visible.
func overlay(base, box string, w, h int) string {
	rows := strings.Split(base, "\n")
	for len(rows) < h {
		rows = append(rows, "")
	}
	boxRows := strings.Split(box, "\n")
	bw := lipgloss.Width(box)
	x := max(0, (w-bw)/2)
	y := max(0, (h-len(boxRows))/2)
	for i, br := range boxRows {
		if y+i >= len(rows) {
			break
		}
		row := rows[y+i]
		left := ansi.Truncate(row, x, "")
		if pad := x - lipgloss.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		rows[y+i] = left + "\x1b[0m" + br + "\x1b[0m" + ansi.TruncateLeft(row, x+bw, "")
	}
	return strings.Join(rows, "\n")
}
