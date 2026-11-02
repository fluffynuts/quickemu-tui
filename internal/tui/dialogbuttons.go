package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Every dialog draws its buttons the same way, and they take part in the tab
// order after the dialog's own controls: tab goes through those first (the text
// box, the settings), then the buttons, then wraps. The first button is the
// default: it is drawn green and bold, the others blue, and the focused one in
// reverse. Enter presses a focused button and does nothing otherwise (a text
// box, or a list's highlighted row, still takes it); a click presses a button
// directly.

// dialogButton is a button a dialog draws. The first of a dialog's buttons is
// its default.
type dialogButton struct {
	label  string
	hotkey rune // a key that does the same; underlined in the label. 0 for none
	press  func(Model) (tea.Model, tea.Cmd)
}

// keyPress is a button that does what a key does in the dialog's handler.
func keyPress(key tea.KeyMsg) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		m.btnFocused = false // or the key would press this button again
		return m.update(key)
	}
}

var (
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
)

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func buttonText(label string) string { return "[ " + label + " ]" }

var (
	defaultButtonStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	otherButtonStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

// buttonStyle is how a button looks: the default green and bold, the others
// blue, and the focused one reversed.
func buttonStyle(isDefault, isFocused bool) lipgloss.Style {
	s := otherButtonStyle
	if isDefault {
		s = defaultButtonStyle
	}
	return s.Reverse(isFocused)
}

// splitHotkey cuts label around the first letter matching hotkey, ignoring
// case; with none (or no hotkey) the whole label comes back as before.
func splitHotkey(label string, hotkey rune) (before, key, after string) {
	if hotkey != 0 {
		for i, r := range label {
			if unicode.ToLower(r) == unicode.ToLower(hotkey) {
				return label[:i], string(r), label[i+len(string(r)):]
			}
		}
	}
	return label, "", ""
}

func renderButton(b dialogButton, isDefault, isFocused bool) string {
	style := buttonStyle(isDefault, isFocused)
	before, key, after := splitHotkey(b.label, b.hotkey)
	if key == "" {
		return style.Render(buttonText(b.label))
	}
	return style.Render("[ "+before) + style.Underline(true).Render(key) + style.Render(after+" ]")
}

// renderButtons draws the buttons in a row; focus is the index of the focused
// one, or -1.
func renderButtons(buttons []dialogButton, focus int) string {
	parts := make([]string, len(buttons))
	for i, b := range buttons {
		parts[i] = renderButton(b, i == 0, i == focus)
	}
	return strings.Join(parts, "  ")
}

// buttonIndexAt is which of buttons is drawn under the screen position (px, py),
// or -1, given the dialog box as drawn with its top left at (bx, by).
func buttonIndexAt(box string, bx, by int, buttons []dialogButton, px, py int) int {
	lines := strings.Split(ansi.Strip(box), "\n")
	for n, b := range buttons {
		text := buttonText(b.label)
		// a button is below anything typed, so look from the bottom up
		for i := len(lines) - 1; i >= 0; i-- {
			if before, _, found := strings.Cut(lines[i], text); found {
				left := bx + ansi.StringWidth(before)
				if py == by+i && px >= left && px < left+ansi.StringWidth(text) {
					return n
				}
				break
			}
		}
	}
	return -1
}

// buttonRow is the open dialog's buttons, drawn.
func (m Model) buttonRow() string {
	focus := -1
	if m.btnFocused {
		focus = m.btnIdx
	}
	return renderButtons(m.dialogButtons(), focus)
}

// dialogButtons are the buttons the open dialog draws, default first, or none.
func (m Model) dialogButtons() []dialogButton {
	switch m.mode {
	case modePrompt:
		return []dialogButton{
			{label: m.promptButton, press: func(m Model) (tea.Model, tea.Cmd) { return m.submitPrompt() }},
			{label: "Cancel", press: func(m Model) (tea.Model, tea.Cmd) { return m.cancelPrompt() }},
		}
	case modeQuickSettings:
		return []dialogButton{
			{label: qsButton, press: func(m Model) (tea.Model, tea.Cmd) { return m.saveQuickSettings() }},
			{label: "Cancel", press: func(m Model) (tea.Model, tea.Cmd) { return m.cancelQuickSettings() }},
		}
	case modeConfirm:
		yes := dialogButton{label: confirmYes, hotkey: 'y', press: func(m Model) (tea.Model, tea.Cmd) { return m.acceptConfirm() }}
		no := dialogButton{label: confirmNo, hotkey: 'n', press: func(m Model) (tea.Model, tea.Cmd) { return m.declineConfirm(true) }}
		if m.confirmDefault == defaultYes {
			return []dialogButton{yes, no}
		}
		return []dialogButton{no, yes}
	case modeInstallProgress:
		return []dialogButton{
			{label: progressBackground, press: func(m Model) (tea.Model, tea.Cmd) { return m.installToBackground() }},
			{label: progressCancel, hotkey: 'c', press: keyPress(runeKey('c'))},
		}
	case modeInstallPick:
		if m.instStep == stepCustomOptions {
			return []dialogButton{
				{label: customNextButton, press: func(m Model) (tea.Model, tea.Cmd) { return m.customNext() }},
				{label: "Back", press: func(m Model) (tea.Model, tea.Cmd) { return m.customBack() }},
			}
		}
		buttons := []dialogButton{{label: "Select", press: keyPress(keyEnter)}}
		if m.instStep != stepOS {
			buttons = append(buttons, dialogButton{label: "Back", press: func(m Model) (tea.Model, tea.Cmd) { return m.installBack() }})
		}
		return append(buttons, dialogButton{label: "Cancel", press: func(m Model) (tea.Model, tea.Cmd) {
			m.mode = modeNormal
			return m, nil
		}})
	case modeMenu:
		return []dialogButton{
			{label: "Run", press: keyPress(keyEnter)},
			{label: "Close", press: keyPress(keyEsc)},
		}
	case modeSnapRevert:
		return []dialogButton{
			{label: "Revert", press: keyPress(keyEnter)},
			{label: "Revert and start", hotkey: 's', press: keyPress(runeKey('s'))},
			{label: "Cancel", press: keyPress(keyEsc)},
		}
	case modeSnapDelete:
		return []dialogButton{
			{label: "Delete ticked", press: keyPress(keyEnter)},
			{label: "Cancel", press: keyPress(keyEsc)},
		}
	case modeResize:
		return []dialogButton{
			{label: "Choose", press: keyPress(keyEnter)},
			{label: "Cancel", press: keyPress(keyEsc)},
		}
	case modeMedia:
		return []dialogButton{
			{label: "Insert…", press: keyPress(runeKey('c'))},
			{label: "Eject", hotkey: 'e', press: keyPress(runeKey('e'))},
			{label: "Reload", hotkey: 'r', press: keyPress(runeKey('r'))},
			{label: "Close", press: keyPress(keyEsc)},
		}
	case modeLogs:
		return []dialogButton{
			{label: "Reload", hotkey: 'r', press: keyPress(runeKey('r'))},
			{label: "Close", press: keyPress(keyEsc)},
		}
	case modeError:
		return []dialogButton{{label: "Close", press: keyPress(keyEsc)}}
	case modeDefaults:
		return []dialogButton{
			{label: "Save", press: func(m Model) (tea.Model, tea.Cmd) { return m.saveDefaults() }},
			{label: "Cancel", press: keyPress(keyEsc)},
		}
	}
	return nil
}

// bodyStops is how many tab stops the open dialog has before its buttons:
// the settings of a dialog made of several, one for a text box or a list, none
// for a plain question.
func (m Model) bodyStops() int {
	switch m.mode {
	case modePrompt, modeMenu, modeSnapRevert, modeSnapDelete, modeResize, modeMedia, modeLogs, modeError, modeDefaults:
		return 1
	case modeQuickSettings:
		return qsFocuses
	case modeInstallPick:
		if m.instStep == stepCustomOptions {
			return customRows
		}
		return 1
	}
	return 0
}

// bodyStop is the tab stop the dialog's own focus is on.
func (m Model) bodyStop() int {
	switch m.mode {
	case modeQuickSettings:
		return m.qs.group
	case modeInstallPick:
		if m.instStep == stepCustomOptions {
			return m.custom.row
		}
	}
	return 0
}

func (m *Model) setBodyStop(stop int) {
	switch m.mode {
	case modeQuickSettings:
		m.qs.group = stop
	case modeInstallPick:
		if m.instStep == stepCustomOptions {
			m.custom.row = stop
		}
	}
}

// dialogKey identifies the dialog on screen, so that a new one starts with no
// button focused. A step of the install wizard is a different dialog; so is a
// second question asked while one is open (see askConfirm).
type dialogKey struct {
	mode   mode
	step   installStep
	serial int
}

func (m Model) dialogKey() dialogKey {
	return dialogKey{mode: m.mode, step: m.instStep, serial: m.dialogSerial}
}

// Update is update, then forgetting the button focus if the dialog changed.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.dialogKey()
	next, cmd := m.update(msg)
	if nm, ok := next.(Model); ok && nm.dialogKey() != before {
		nm.btnFocused = false
		return nm, cmd
	}
	return next, cmd
}

// handleDialogFocusKey moves focus among the dialog's tab stops and buttons,
// and presses a focused button. It reports whether it used the key.
func (m Model) handleDialogFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	buttons := m.dialogButtons()
	if len(buttons) == 0 {
		return m, nil, false
	}
	switch msg.String() {
	case "tab", "shift+tab":
		dir := +1
		if msg.String() == "shift+tab" {
			dir = -1
		}
		m.moveFocus(len(buttons), dir)
		// not inline in the return: the order of evaluating m against a call
		// that changes it isn't defined
		cmd := m.syncTextFocus()
		return m, cmd, true
	}
	if !m.btnFocused {
		return m, nil, false
	}
	switch msg.String() {
	case "left":
		m.btnIdx = (m.btnIdx + len(buttons) - 1) % len(buttons)
		return m, nil, true
	case "right":
		m.btnIdx = (m.btnIdx + 1) % len(buttons)
		return m, nil, true
	case "enter", " ":
		next, cmd := buttons[m.btnIdx].press(m)
		return next, cmd, true
	case "esc", "ctrl+c":
		return m, nil, false
	}
	// anything else is for the dialog's own controls: back to them first
	m.btnFocused = false
	cmd := m.syncTextFocus()
	next, cmd2 := m.update(msg)
	return next, batch(cmd, cmd2), true
}

// moveFocus steps the focus one tab stop forward or back, wrapping: the
// dialog's own stops come first, then the buttons.
func (m *Model) moveFocus(buttons, dir int) {
	stops := m.bodyStops()
	if m.btnFocused {
		next := m.btnIdx + dir
		switch {
		case next >= 0 && next < buttons:
			m.btnIdx = next
		case stops > 0:
			m.btnFocused = false
			m.setBodyStop(map[int]int{+1: 0, -1: stops - 1}[dir])
		default:
			m.btnIdx = (next + buttons) % buttons
		}
		return
	}
	pos := m.bodyStop()
	if next := pos + dir; stops > 0 && next >= 0 && next < stops {
		m.setBodyStop(next)
		return
	}
	m.btnFocused = true
	m.btnIdx = map[int]int{+1: 0, -1: buttons - 1}[dir]
}

// syncTextFocus gives the dialog's text box the cursor only while the focus is
// on it.
func (m *Model) syncTextFocus() tea.Cmd {
	switch m.mode {
	case modePrompt:
		if m.btnFocused {
			m.input.Blur()
			return nil
		}
		return m.input.Focus()
	case modeDefaults:
		if m.btnFocused {
			m.defInput.Blur()
			return nil
		}
		return m.defInput.Focus()
	}
	return nil
}
