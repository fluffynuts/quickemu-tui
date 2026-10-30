package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Mouse support maps a click onto the same layout the views draw, using the
// helpers they share (bodyArea, listWidth, detailHead, windowStart, menuItemAt,
// qsLayout).
//
// On the main screen a click selects a VM or snapshot; clicking the selected
// VM, or right-clicking any, opens its actions menu, where clicking an item
// runs it and clicking outside closes it. In quick settings a click picks a
// choice or ticks the checkbox, and clicking outside saves and closes, like
// esc. The wheel moves through whatever list is under it, and elsewhere stands
// in for ↑/↓. Every dialog has a close button ([x] in its top border) that
// does what esc does there, and those that ask something have buttons
// ([ Save ], [ Yes ] [ No ] etc.) that press the key they stand for. In the
// install picker a click on a row chooses it, and on the custom ISO settings a
// click on a row selects it, and on its ‹ or › changes it.

// modal content starts inside modalStyle's border and padding
const (
	modalInsetX = 3
	modalInsetY = 2
)

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	wheel := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		wheel = -1
	case tea.MouseButtonWheelDown:
		wheel = 1
	case tea.MouseButtonLeft, tea.MouseButtonRight:
	default:
		return m, nil
	}
	if m.mode != modeNormal && wheel == 0 && m.onCloseButton(msg) {
		return m.closeDialog()
	}
	if wheel == 0 {
		if key, ok := m.clickedDialogButton(msg); ok {
			return m.Update(key)
		}
	}
	switch m.mode {
	case modeNormal:
		return m.mouseMain(msg, wheel)
	case modeMenu:
		return m.mouseMenu(msg, wheel)
	case modeQuickSettings:
		return m.mouseQuickSettings(msg, wheel)
	case modeInstallPick:
		if wheel == 0 {
			return m.mouseInstallPick(msg)
		}
		if m.instStep == stepCustomOptions {
			// ↑↓ would change what a row says, which a stray wheel mustn't
			key := tea.KeyMsg{Type: tea.KeyTab}
			if wheel < 0 {
				key = tea.KeyMsg{Type: tea.KeyShiftTab}
			}
			return m.Update(key)
		}
	}
	if wheel != 0 {
		key := tea.KeyMsg{Type: tea.KeyDown}
		if wheel < 0 {
			key = tea.KeyMsg{Type: tea.KeyUp}
		}
		return m.Update(key)
	}
	return m, nil
}

func (m Model) mouseMain(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	top, h := m.bodyArea()
	if msg.Y < top || msg.Y >= top+h {
		return m, nil
	}
	clicked := paneVMs
	if msg.X >= m.listWidth() {
		clicked = paneSnapshots
	}
	if wheel != 0 {
		m.pane = clicked
		return m, m.move(wheel)
	}
	if clicked == paneVMs {
		// the pane's border and its "VMs" title come before the rows
		i := windowStart(len(m.vms), m.cursor, h-3) + msg.Y - top - 2
		if msg.Y-top < 2 || i >= len(m.vms) {
			m.pane = paneVMs
			return m, nil
		}
		again := i == m.cursor && m.pane == paneVMs
		m.pane = paneVMs
		cmd := m.move(i - m.cursor)
		if again || msg.Button == tea.MouseButtonRight {
			m.openMenu()
		}
		return m, cmd
	}
	m.pane = paneSnapshots
	if vm, ok := m.selected(); ok {
		snaps := m.snapshots(vm)
		// border, the detail lines, a blank, then the snapshots' title and
		// column headings
		first := top + 1 + len(m.detailHead(vm, m.width-m.listWidth()-4)) + 3
		rows := h - 2 - (first - top - 1)
		if i := windowStart(len(snaps), m.snapCursor, rows) + msg.Y - first; msg.Y >= first && i < len(snaps) {
			m.snapCursor = i
		}
	}
	return m, nil
}

func (m Model) onCloseButton(msg tea.MouseMsg) bool {
	box := m.dialogBox()
	x, y := m.modalAt(box)
	left := x + lipgloss.Width(box) - closeButtonOffset
	return msg.Y == y && msg.X >= left && msg.X < left+lipgloss.Width(closeButton)
}

// dialogButton is a button a dialog draws; clicking it presses key.
type dialogButton struct {
	label string
	key   tea.KeyMsg
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// dialogButtons are the buttons the open dialog draws, if any.
func (m Model) dialogButtons() []dialogButton {
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	switch m.mode {
	case modePrompt:
		return []dialogButton{{m.promptButton, enter}}
	case modeQuickSettings:
		return []dialogButton{{qsButton, enter}}
	case modeConfirm:
		return []dialogButton{{confirmYes, runeKey('y')}, {confirmNo, runeKey('n')}}
	case modeInstallProgress:
		return []dialogButton{{progressBackground, tea.KeyMsg{Type: tea.KeyEsc}}, {progressCancel, runeKey('c')}}
	case modeInstallPick:
		if m.instStep == stepCustomOptions {
			return []dialogButton{{customNextButton, enter}}
		}
	}
	return nil
}

// clickedDialogButton is the key of the dialog button under the click, if one is.
func (m Model) clickedDialogButton(msg tea.MouseMsg) (tea.KeyMsg, bool) {
	buttons := m.dialogButtons()
	if len(buttons) == 0 {
		return tea.KeyMsg{}, false
	}
	box := m.dialogBox()
	x, y := m.modalAt(box)
	lines := strings.Split(ansi.Strip(box), "\n")
	for _, b := range buttons {
		text := buttonText(b.label)
		// a button is below anything typed, so look from the bottom up
		for i := len(lines) - 1; i >= 0; i-- {
			if before, _, found := strings.Cut(lines[i], text); found {
				left := x + ansi.StringWidth(before)
				if msg.Y == y+i && msg.X >= left && msg.X < left+ansi.StringWidth(text) {
					return b.key, true
				}
				break
			}
		}
	}
	return tea.KeyMsg{}, false
}

// closeDialog is what esc does in the open dialog, except that the install
// picker, where esc steps back, closes outright.
func (m Model) closeDialog() (tea.Model, tea.Cmd) {
	if m.mode == modeInstallPick {
		m.mode = modeNormal
		return m, nil
	}
	return m.Update(tea.KeyMsg{Type: tea.KeyEsc})
}

// modalAt is where box, centred in the body as View draws it, starts.
func (m Model) modalAt(box string) (x, y int) {
	top, h := m.bodyArea()
	return max(0, (m.width-lipgloss.Width(box))/2), top + max(0, (h-lipgloss.Height(box))/2)
}

func inBox(box string, x, y, px, py int) bool {
	return px >= x && px < x+lipgloss.Width(box) && py >= y && py < y+lipgloss.Height(box)
}

func (m Model) mouseMenu(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	items := m.menuItems()
	if wheel != 0 {
		m.menuCursor = clamp(m.menuCursor+wheel, 0, len(items)-1)
		return m, nil
	}
	box := m.viewMenu()
	x, y := m.modalAt(box)
	if !inBox(box, x, y, msg.X, msg.Y) {
		m.mode = modeNormal
		return m, nil
	}
	if i := menuItemAt(items, msg.Y-y-modalInsetY); i >= 0 {
		m.mode = modeNormal
		return m.runAction(items[i].key)
	}
	return m, nil
}

func (m Model) mouseQuickSettings(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	box := modalStyle.Render(m.viewQuickSettings())
	bx, by := m.modalAt(box)
	if wheel == 0 && !inBox(box, bx, by, msg.X, msg.Y) {
		return m.saveQuickSettings()
	}
	qs := &m.qs
	l := m.qsLayout()
	px, py := msg.X-bx-modalInsetX, msg.Y-by-modalInsetY
	if wheel == 0 && py == l.glY {
		qs.group = qsGL
		qs.gl = !qs.gl
		return m, nil
	}
	if wheel == 0 && py == l.glY+1 {
		qs.group = qsSpice
		qs.spice = !qs.spice
		return m, nil
	}
	for g := range qsGroups {
		at, width := l.at[g], lipgloss.Width(strings.Join(l.cols[g], "\n"))
		if px < at.x || px >= at.x+width || py < at.y || py >= at.y+len(l.cols[g]) {
			continue
		}
		qs.group = g
		if wheel != 0 {
			qs.cursor[g] = clamp(qs.cursor[g]+wheel, 0, len(qs.items[g])-1)
		} else if i := windowStart(len(qs.items[g]), qs.cursor[g], l.listRows) + py - at.y - 1; py > at.y {
			qs.cursor[g] = i
		}
		return m, nil
	}
	return m, nil
}

// mouseInstallPick handles a click in the install picker: on a row, it
// chooses it (as enter does); on the custom ISO settings, see mouseCustomOptions.
func (m Model) mouseInstallPick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	box := m.dialogBox()
	bx, by := m.modalAt(box)
	if !inBox(box, bx, by, msg.X, msg.Y) {
		return m, nil
	}
	px, py := msg.X-bx-modalInsetX, msg.Y-by-modalInsetY
	if m.instStep == stepCustomOptions {
		return m.mouseCustomOptions(px, py)
	}
	items := m.pickerItems()
	rows := m.instListRows()
	start := windowStart(len(items), clamp(m.instCursor, 0, len(items)-1), rows)
	if i := start + py - m.instListTop(); py >= m.instListTop() && i >= start && i < min(len(items), start+rows) {
		m.instCursor = i
		return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return m, nil
}

func (m Model) mouseCustomOptions(px, py int) (tea.Model, tea.Cmd) {
	row := py - customOptionsTop
	if row < 0 || row >= customRows {
		return m, nil
	}
	d := &m.custom
	d.row = row
	values, _ := customRowChoices(row)
	left, right := customArrowCols(values[d.choice[row]])
	switch px {
	case left:
		d.choice[row] = clamp(d.choice[row]-1, 0, len(values)-1)
	case right:
		d.choice[row] = clamp(d.choice[row]+1, 0, len(values)-1)
	}
	return m, nil
}
