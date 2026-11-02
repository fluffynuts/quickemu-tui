package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	tabKey      = tea.KeyMsg{Type: tea.KeyTab}
	shiftTabKey = tea.KeyMsg{Type: tea.KeyShiftTab}
	leftKey     = tea.KeyMsg{Type: tea.KeyLeft}
	rightKey    = tea.KeyMsg{Type: tea.KeyRight}
	spaceKey    = tea.KeyMsg{Type: tea.KeySpace}
	escKey      = tea.KeyMsg{Type: tea.KeyEsc}
)

// confirmProbe opens a confirmation and records what it ended up doing.
type confirmProbe struct {
	accepted, declined int
}

func openConfirm(t *testing.T, def confirmDefault) (Model, *confirmProbe) {
	t.Helper()
	m := newMouseModel(t, "alpha")
	p := &confirmProbe{}
	m.askConfirm("Sure?", def, func(*Model) tea.Cmd { p.accepted++; return nil })
	m.onDecline = func(*Model) tea.Cmd { p.declined++; return nil }
	return m, p
}

// focused is the label of the focused button, or "" when none is.
func focused(m Model) string {
	if !m.btnFocused {
		return ""
	}
	return m.dialogButtons()[m.btnIdx].label
}

func TestConfirmOpensWithNothingFocusedAndEnterDoesNothing(t *testing.T) {
	for _, def := range []confirmDefault{defaultYes, defaultNo} {
		m, p := openConfirm(t, def)
		if focused(m) != "" {
			t.Fatalf("default %v: %q focused on open", def, focused(m))
		}
		m, _ = send(m, enter)
		if m.mode != modeConfirm || p.accepted+p.declined != 0 {
			t.Fatalf("default %v: enter acted: mode=%v accepted=%d declined=%d", def, m.mode, p.accepted, p.declined)
		}
	}
}

func TestFirstTabLandsOnTheDefaultButton(t *testing.T) {
	m, _ := openConfirm(t, defaultYes)
	m, _ = send(m, tabKey)
	if focused(m) != "Yes" {
		t.Fatalf("default yes: first tab focused %q", focused(m))
	}
	m, _ = openConfirm(t, defaultNo)
	m, _ = send(m, tabKey)
	if focused(m) != "No" {
		t.Fatalf("default no: first tab focused %q", focused(m))
	}
}

func TestTabCyclesThroughTheButtonsAndWraps(t *testing.T) {
	m, _ := openConfirm(t, defaultNo)
	var seen []string
	for range 3 {
		m, _ = send(m, tabKey)
		seen = append(seen, focused(m))
	}
	if got := strings.Join(seen, ","); got != "No,Yes,No" {
		t.Fatalf("tab order = %s, want No,Yes,No", got)
	}
}

func TestShiftTabGoesTheOtherWayFromTheLastButton(t *testing.T) {
	m, _ := openConfirm(t, defaultYes)
	m, _ = send(m, shiftTabKey)
	if focused(m) != "No" {
		t.Fatalf("shift+tab from nothing focused %q, want the last button", focused(m))
	}
	m, _ = send(m, shiftTabKey)
	if focused(m) != "Yes" {
		t.Fatalf("then %q, want Yes", focused(m))
	}
}

func TestEnterAndSpacePressTheFocusedButton(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{"enter": enter, "space": spaceKey} {
		m, p := openConfirm(t, defaultYes)
		m, _ = send(m, tabKey, key)
		if p.accepted != 1 || m.mode != modeNormal {
			t.Errorf("%s on Yes: accepted=%d mode=%v", name, p.accepted, m.mode)
		}
		m, p = openConfirm(t, defaultYes)
		m, _ = send(m, tabKey, tabKey, key)
		if p.declined != 1 || p.accepted != 0 {
			t.Errorf("%s on No: accepted=%d declined=%d", name, p.accepted, p.declined)
		}
	}
}

func TestEnterOnTheNonDefaultButtonPressesThatOne(t *testing.T) {
	m, p := openConfirm(t, defaultNo)
	m, _ = send(m, tabKey, tabKey, enter) // No, then Yes
	if p.accepted != 1 {
		t.Fatalf("accepted=%d declined=%d", p.accepted, p.declined)
	}
	_ = m
}

func TestArrowsMoveBetweenButtonsOnceOneIsFocused(t *testing.T) {
	m, _ := openConfirm(t, defaultYes)
	m, _ = send(m, tabKey, rightKey)
	if focused(m) != "No" {
		t.Fatalf("right: %q", focused(m))
	}
	m, _ = send(m, leftKey)
	if focused(m) != "Yes" {
		t.Fatalf("left: %q", focused(m))
	}
}

func TestYAndNStillAnswerAndEscStillDismisses(t *testing.T) {
	m, p := openConfirm(t, defaultNo)
	m, _ = send(m, runes("y"))
	if p.accepted != 1 {
		t.Errorf("y: accepted=%d", p.accepted)
	}
	m, p = openConfirm(t, defaultYes)
	m, _ = send(m, runes("n"))
	if p.declined != 1 {
		t.Errorf("n: declined=%d", p.declined)
	}
	m, p = openConfirm(t, defaultYes)
	m, _ = send(m, escKey)
	if m.mode != modeNormal || p.accepted+p.declined != 0 {
		t.Errorf("esc: mode=%v accepted=%d declined=%d (esc dismisses without answering)", m.mode, p.accepted, p.declined)
	}
}

func TestADifferentDialogStartsUnfocused(t *testing.T) {
	m, _ := openConfirm(t, defaultYes)
	m, _ = send(m, tabKey)
	if focused(m) == "" {
		t.Fatal("setup: nothing focused")
	}
	m.askConfirm("Another?", defaultYes, nil) // as a confirmation does when it leads to the next
	if focused(m) != "" {
		t.Fatalf("%q still focused in a new dialog", focused(m))
	}
}

func TestConfirmShowsItsButtonsDefaultFirstAndNoYNHint(t *testing.T) {
	m, _ := openConfirm(t, defaultYes)
	s := screen(m)
	if strings.Index(s, "[ Yes ]") < 0 || strings.Index(s, "[ Yes ]") > strings.Index(s, "[ No ]") {
		t.Errorf("default yes: want Yes before No:\n%s", s)
	}
	if strings.Contains(s, "Y/n") || strings.Contains(s, "y/N") {
		t.Errorf("the y/n hint is still drawn:\n%s", s)
	}
	m, _ = openConfirm(t, defaultNo)
	s = screen(m)
	if strings.Index(s, "[ No ]") < 0 || strings.Index(s, "[ No ]") > strings.Index(s, "[ Yes ]") {
		t.Errorf("default no: want No before Yes:\n%s", s)
	}
}

func TestButtonStylesMarkTheDefaultAndTheFocus(t *testing.T) {
	green, blue := lipgloss.Color("10"), lipgloss.Color("12")
	def, other := buttonStyle(true, false), buttonStyle(false, false)
	if def.GetForeground() != green || !def.GetBold() {
		t.Errorf("default button: fg=%v bold=%v, want bold green", def.GetForeground(), def.GetBold())
	}
	if other.GetForeground() != blue || other.GetBold() {
		t.Errorf("other button: fg=%v bold=%v, want plain blue", other.GetForeground(), other.GetBold())
	}
	if !buttonStyle(true, true).GetReverse() || !buttonStyle(false, true).GetReverse() {
		t.Error("a focused button isn't reversed")
	}
	if def.GetReverse() || other.GetReverse() {
		t.Error("an unfocused button is reversed")
	}
}

func TestHotkeyLetterIsPickedOutOfTheLabel(t *testing.T) {
	for _, c := range []struct {
		label              string
		hotkey             rune
		before, key, after string
	}{
		{"Yes", 'y', "", "Y", "es"},
		{"No", 'n', "", "N", "o"},
		{"Revert and start", 's', "Revert and ", "s", "tart"}, // the first match, whatever its case
		{"Cancel", 0, "Cancel", "", ""},
		{"Save", 'x', "Save", "", ""}, // a hotkey the label doesn't contain is left alone
	} {
		before, key, after := splitHotkey(c.label, c.hotkey)
		if before != c.before || key != c.key || after != c.after {
			t.Errorf("splitHotkey(%q, %q) = %q %q %q, want %q %q %q", c.label, c.hotkey, before, key, after, c.before, c.key, c.after)
		}
	}
	if got := ansi.Strip(renderButton(dialogButton{label: "Yes", hotkey: 'y'}, true, false)); got != "[ Yes ]" {
		t.Errorf("rendered text = %q", got)
	}
}

func TestClickingAConfirmButtonPressesIt(t *testing.T) {
	m, p := openConfirm(t, defaultYes)
	m = click(t, m, "[ No ]", anywhere, tea.MouseButtonLeft)
	if p.declined != 1 {
		t.Fatalf("clicking No: accepted=%d declined=%d", p.accepted, p.declined)
	}
	m, p = openConfirm(t, defaultNo)
	m = click(t, m, "[ Yes ]", anywhere, tea.MouseButtonLeft)
	if p.accepted != 1 {
		t.Fatalf("clicking Yes: accepted=%d declined=%d", p.accepted, p.declined)
	}
	_ = m
}

// --- the text prompt ---

type promptProbe struct {
	submitted []string
}

func openPrompt(t *testing.T) (Model, *promptProbe) {
	t.Helper()
	m := newMouseModel(t, "alpha")
	p := &promptProbe{}
	m.askPrompt("Name?", "Save", "start", func(_ *Model, v string) tea.Cmd { p.submitted = append(p.submitted, v); return nil })
	return m, p
}

func TestPromptStartsInTheTextBoxAndEnterThereSubmits(t *testing.T) {
	m, p := openPrompt(t)
	if focused(m) != "" {
		t.Fatalf("%q focused on open", focused(m))
	}
	m, _ = send(m, enter)
	if len(p.submitted) != 1 || p.submitted[0] != "start" || m.mode != modeNormal {
		t.Fatalf("submitted=%v mode=%v", p.submitted, m.mode)
	}
}

func TestPromptTabGoesFromTheTextBoxToItsButtonsThenBack(t *testing.T) {
	m, _ := openPrompt(t)
	var seen []string
	for range 3 {
		m, _ = send(m, tabKey)
		seen = append(seen, focused(m))
	}
	if got := strings.Join(seen, ","); got != "Save,Cancel," {
		t.Fatalf("tab order = %q, want Save,Cancel then the text box", got)
	}
}

func TestPromptEnterOnTheButtonsSubmitsOrCancels(t *testing.T) {
	m, p := openPrompt(t)
	m, _ = send(m, tabKey, enter)
	if len(p.submitted) != 1 {
		t.Fatalf("Save: submitted=%v", p.submitted)
	}
	m, p = openPrompt(t)
	m, _ = send(m, tabKey, tabKey, enter)
	if len(p.submitted) != 0 || m.mode != modeNormal {
		t.Fatalf("Cancel: submitted=%v mode=%v", p.submitted, m.mode)
	}
}

func TestTypingWhileAButtonIsFocusedGoesBackToTheTextBox(t *testing.T) {
	m, p := openPrompt(t)
	m, _ = send(m, tabKey, runes("!"))
	if focused(m) != "" {
		t.Fatalf("%q still focused after typing", focused(m))
	}
	m, _ = send(m, enter)
	if len(p.submitted) != 1 || p.submitted[0] != "start!" {
		t.Fatalf("submitted=%v, want the typed character in the text", p.submitted)
	}
}

func TestPromptEscCancels(t *testing.T) {
	m, p := openPrompt(t)
	m, _ = send(m, escKey)
	if m.mode != modeNormal || len(p.submitted) != 0 {
		t.Fatalf("mode=%v submitted=%v", m.mode, p.submitted)
	}
}

func TestClickingAPromptButtonPressesIt(t *testing.T) {
	m, p := openPrompt(t)
	m = click(t, m, "[ Cancel ]", anywhere, tea.MouseButtonLeft)
	if m.mode != modeNormal || len(p.submitted) != 0 {
		t.Fatalf("Cancel: mode=%v submitted=%v", m.mode, p.submitted)
	}
	m, p = openPrompt(t)
	m = click(t, m, "[ Save ]", anywhere, tea.MouseButtonLeft)
	if len(p.submitted) != 1 {
		t.Fatalf("Save: submitted=%v", p.submitted)
	}
}

func TestAConfirmationLeadingToAnotherStartsUnfocused(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.askConfirm("First?", defaultYes, func(m *Model) tea.Cmd {
		m.askConfirm("Second?", defaultYes, nil)
		return nil
	})
	m, _ = send(m, tabKey, enter) // Yes on the first
	if m.mode != modeConfirm || m.confirmText != "Second?" {
		t.Fatalf("mode=%v text=%q, want the second question", m.mode, m.confirmText)
	}
	if focused(m) != "" {
		t.Fatalf("%q was already focused on the second question", focused(m))
	}
}

func TestClosingADialogForgetsItsFocus(t *testing.T) {
	m, _ := openConfirm(t, defaultYes)
	m, _ = send(m, tabKey, escKey)
	if m.mode != modeNormal || m.btnFocused {
		t.Fatalf("mode=%v btnFocused=%v, want closed with no focus left behind", m.mode, m.btnFocused)
	}
}

// pressButton tabs to the button with that label and presses it with enter.
func pressButton(t *testing.T, m Model, label string) Model {
	t.Helper()
	for range 20 {
		if focused(m) == label {
			m, _ = send(m, enter)
			return m
		}
		m, _ = send(m, tabKey)
	}
	t.Fatalf("no %q button reachable by tab in mode %v", label, m.mode)
	return m
}
