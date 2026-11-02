package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// setupButtons are the first-run dialog's buttons, default first. They follow
// its one text box in the tab order, as in every other dialog.
var setupButtons = []dialogButton{{label: "Save"}, {label: "Quit"}}

// Setup is the first-run dialog asking where the quickemu VMs live.
type Setup struct {
	input     textinput.Model
	focus     int    // -1: the text box; otherwise an index into setupButtons
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
	return Setup{input: ti, focus: -1}
}

// Cancelled reports whether the user quit without confirming.
func (s Setup) Cancelled() bool { return s.cancelled }

func (s Setup) Init() tea.Cmd { return textinput.Blink }

func (s Setup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s.key(msg)
	case tea.MouseMsg:
		return s.mouse(msg)
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s Setup) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return s.quit()
	case "tab":
		s.focus = s.focus + 1
		if s.focus >= len(setupButtons) {
			s.focus = -1
		}
		return s, s.syncFocus()
	case "shift+tab":
		s.focus = s.focus - 1
		if s.focus < -1 {
			s.focus = len(setupButtons) - 1
		}
		return s, s.syncFocus()
	}
	if s.focus >= 0 {
		switch msg.String() {
		case "left":
			s.focus = (s.focus + len(setupButtons) - 1) % len(setupButtons)
			return s, nil
		case "right":
			s.focus = (s.focus + 1) % len(setupButtons)
			return s, nil
		case "enter", " ":
			return s.press(s.focus)
		}
		// anything else is for the text box
		s.focus = -1
		cmd := s.syncFocus()
		next, cmd2 := s.key(msg)
		return next, tea.Batch(cmd, cmd2)
	}
	if msg.String() == "enter" {
		return s.press(0)
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

// press does what button n does.
func (s Setup) press(n int) (tea.Model, tea.Cmd) {
	if n == 1 {
		return s.quit()
	}
	if v := strings.TrimSpace(s.input.Value()); v != "" {
		s.Dir = v
		return s, tea.Quit
	}
	return s, nil
}

func (s Setup) quit() (tea.Model, tea.Cmd) {
	s.cancelled = true
	return s, tea.Quit
}

// syncFocus gives the text box the cursor only while the focus is on it.
func (s *Setup) syncFocus() tea.Cmd {
	if s.focus >= 0 {
		s.input.Blur()
		return nil
	}
	return s.input.Focus()
}

// the dialog is drawn one line down, as View does
const setupTop = 1

func (s Setup) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || (msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight) {
		return s, nil
	}
	box := s.box()
	left := lipgloss.Width(box) - closeButtonOffset
	if msg.Y == setupTop && msg.X >= left && msg.X < left+lipgloss.Width(closeButton) {
		return s.quit()
	}
	if i := buttonIndexAt(box, 0, setupTop, setupButtons, msg.X, msg.Y); i >= 0 {
		return s.press(i)
	}
	return s, nil
}

func (s Setup) box() string {
	body := titleStyle.Render("Welcome to quickemu-tui") + "\n\n" +
		"Where are your quickemu VMs kept?\n" +
		dimStyle.Render("(the directory containing your *.conf files)") + "\n\n" +
		s.input.View() + "\n\n" +
		renderButtons(setupButtons, s.focus) + "\n\n" +
		dimStyle.Render("enter in the text box saves • tab buttons • esc quit")
	return withClose(modalStyle.Render(body))
}

func (s Setup) View() string {
	return "\n" + s.box() + "\n"
}
