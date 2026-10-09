package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sizeErrView fits the error pane to the terminal.
func (m *Model) sizeErrView() {
	m.errView.Width = max(20, min(100, m.width-8))
	m.errView.Height = max(3, min(20, m.height-12))
	if m.mode == modeError {
		m.setErrContent()
	}
}

func (m *Model) setErrContent() {
	text := strings.ReplaceAll(m.errText(), "\t", "    ")
	m.errView.SetContent(lipgloss.NewStyle().Width(m.errView.Width).Render(text))
}

// errText is kept on the viewport's content source so resizes can re-wrap it.
func (m Model) errText() string { return m.errBody }

// showError opens a scrollable dialog with the full text of an error. It
// returns to whatever was on screen when closed.
func (m *Model) showError(title, text string) {
	if m.mode != modeError {
		m.errReturn = m.mode
	}
	m.mode = modeError
	m.errTitle = title
	m.errBody = strings.TrimSpace(text)
	m.sizeErrView()
	m.setErrContent()
	m.errView.GotoTop()
}

func (m Model) handleErrorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.interruptQuit()
	case "esc", "q", "enter":
		m.mode = m.errReturn
		return m, nil
	case "home", "g":
		m.errView.GotoTop()
		return m, nil
	case "end", "G":
		m.errView.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.errView, cmd = m.errView.Update(msg)
	return m, cmd
}
