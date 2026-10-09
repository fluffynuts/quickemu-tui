package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/config"
	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

func newDefaultsInput() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0
	// a thin border so it reads as somewhere to type
	border := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	ta.FocusedStyle.Base = border
	ta.BlurredStyle.Base = border
	return ta
}

// defaultsHeader starts the editor when there are no defaults yet, so it's
// clear the box is empty and the user can just type below it.
const defaultsHeader = "# defaults applied to all machines"

// countOptions is how many of lines set something (not blank or a comment).
func countOptions(lines []string) int {
	n := 0
	for _, l := range lines {
		if _, ok := qemu.DefaultKey(l); ok {
			n++
		}
	}
	return n
}

func (m *Model) openDefaults() tea.Cmd {
	m.mode = modeDefaults
	m.defErr = ""
	m.defInput.SetWidth(max(30, min(70, m.width-12)))
	m.defInput.SetHeight(max(4, min(10, m.height-18)))
	if len(m.defaults) == 0 {
		m.defInput.SetValue(defaultsHeader + "\n")
	} else {
		m.defInput.SetValue(strings.Join(m.defaults, "\n"))
	}
	return m.defInput.Focus()
}

func (m Model) handleDefaultsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.interruptQuit()
	case "esc":
		m.mode = modeNormal
		m.defInput.Blur()
		return m, nil
	case "ctrl+s":
		return m.saveDefaults()
	}
	m.defErr = ""
	var cmd tea.Cmd
	m.defInput, cmd = m.defInput.Update(msg)
	return m, cmd
}

func (m Model) saveDefaults() (tea.Model, tea.Cmd) {
	var lines []string
	for _, l := range strings.Split(m.defInput.Value(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if err := qemu.ValidateDefaults(lines); err != nil {
		m.defErr = err.Error()
		return m, nil
	}
	if m.opts.ConfigPath == "" {
		m.defErr = "no user config directory is available to save into"
		return m, nil
	}
	if err := config.Update(m.opts.ConfigPath, func(c *config.Config) { c.DefaultConf = lines }); err != nil {
		m.defErr = err.Error()
		return m, nil
	}
	m.defaults = lines
	m.mode = modeNormal
	m.defInput.Blur()
	if n := countOptions(lines); n == 0 {
		m.setFlash("Default VM options cleared", false)
	} else {
		m.setFlash(fmt.Sprintf("Saved %d default VM option(s)", n), false)
	}
	return m, nil
}

func (m Model) viewDefaults() string {
	lines := []string{
		titleStyle.Render("Default VM options"),
		"",
		"One option per line, written as it appears in a .conf, e.g.  " + okStyle.Render(`gl="off"`),
		dimStyle.Render("New VMs get these added. Existing VMs get any that their .conf"),
		dimStyle.Render("doesn't already set, when started. A value set in the .conf wins."),
		"",
		m.defInput.View(),
	}
	if m.defErr != "" {
		lines = append(lines, "", errStyle.Render(trunc(m.defErr, max(30, min(70, m.width-12)))))
	}
	lines = append(lines, "", dimStyle.Render("ctrl+s save • esc discard"))
	return strings.Join(lines, "\n")
}

// applyDefaultsTo merges the user's defaults into one conf file.
func applyDefaultsTo(defaults []string, confPath string) ([]string, error) {
	return qemu.ApplyDefaults(confPath, defaults)
}

// defaultsNote describes a merge for appending to a status message.
func defaultsNote(added []string, err error) string {
	switch {
	case err != nil:
		return " (couldn't add default options: " + firstLine(err.Error()) + ")"
	case len(added) > 0:
		return " (added " + strings.Join(added, " ") + " to its .conf)"
	}
	return ""
}
