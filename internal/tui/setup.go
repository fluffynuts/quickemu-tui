package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Setup is the first-run dialog asking where the quickemu VMs live.
type Setup struct {
	input     textinput.Model
	Dir       string // the confirmed directory; empty if cancelled
	cancelled bool
}

// NewSetup returns the dialog pre-populated with def.
func NewSetup(def string) Setup {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 4096
	ti.Width = 60
	ti.SetValue(def)
	ti.CursorEnd()
	ti.Focus()
	return Setup{input: ti}
}

// Cancelled reports whether the user quit without confirming.
func (s Setup) Cancelled() bool { return s.cancelled }

func (s Setup) Init() tea.Cmd { return textinput.Blink }

func (s Setup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
			if v := strings.TrimSpace(s.input.Value()); v != "" {
				s.Dir = v
				return s, tea.Quit
			}
			return s, nil
		case "esc", "ctrl+c":
			s.cancelled = true
			return s, tea.Quit
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s Setup) View() string {
	body := titleStyle.Render("Welcome to quickemu-tui") + "\n\n" +
		"Where are your quickemu VMs kept?\n" +
		dimStyle.Render("(the directory containing your *.conf files)") + "\n\n" +
		s.input.View() + "\n\n" +
		dimStyle.Render("enter: save   esc: quit")
	return "\n" + modalStyle.Render(body) + "\n"
}
