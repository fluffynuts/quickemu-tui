package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	return func(m Model) (tea.Model, tea.Cmd) { return m.update(key) }
}

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
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	switch m.mode {
	case modePrompt:
		return []dialogButton{
			{label: m.promptButton, press: func(m Model) (tea.Model, tea.Cmd) { return m.submitPrompt() }},
			{label: "Cancel", press: func(m Model) (tea.Model, tea.Cmd) { return m.cancelPrompt() }},
		}
	case modeQuickSettings:
		return []dialogButton{{label: qsButton, press: keyPress(enter)}}
	case modeConfirm:
		yes := dialogButton{label: confirmYes, hotkey: 'y', press: func(m Model) (tea.Model, tea.Cmd) { return m.acceptConfirm() }}
		no := dialogButton{label: confirmNo, hotkey: 'n', press: func(m Model) (tea.Model, tea.Cmd) { return m.declineConfirm(true) }}
		if m.confirmDefault == defaultYes {
			return []dialogButton{yes, no}
		}
		return []dialogButton{no, yes}
	case modeInstallProgress:
		return []dialogButton{
			{label: progressBackground, press: keyPress(tea.KeyMsg{Type: tea.KeyEsc})},
			{label: progressCancel, hotkey: 'c', press: keyPress(runeKey('c'))},
		}
	case modeInstallPick:
		if m.instStep == stepCustomOptions {
			return []dialogButton{{label: customNextButton, press: keyPress(enter)}}
		}
	}
	return nil
}

// usesButtonFocus is whether the dialog's buttons are in its tab order.
func (m Model) usesButtonFocus() bool {
	switch m.mode {
	case modeConfirm, modePrompt:
		return true
	}
	return false
}

// bodyStops is how many tab stops the open dialog has before its buttons:
// the settings of a dialog made of several, one for a text box or a list, none
// for a plain question.
func (m Model) bodyStops() int {
	switch m.mode {
	case modePrompt:
		return 1
	}
	return 0
}

// bodyStop is the tab stop the dialog's own focus is on.
func (m Model) bodyStop() int { return 0 }

func (m *Model) setBodyStop(int) {}

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
	if len(buttons) == 0 || !m.usesButtonFocus() {
		return m, nil, false
	}
	switch msg.String() {
	case "tab":
		m.moveFocus(len(buttons), +1)
		return m, m.syncTextFocus(), true
	case "shift+tab":
		m.moveFocus(len(buttons), -1)
		return m, m.syncTextFocus(), true
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
	if m.mode != modePrompt {
		return nil
	}
	if m.btnFocused {
		m.input.Blur()
		return nil
	}
	return m.input.Focus()
}
