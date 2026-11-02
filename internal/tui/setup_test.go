package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func setupSend(s Setup, msgs ...tea.Msg) (Setup, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = s.Update(msg)
		s = next.(Setup)
	}
	return s, cmd
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func setupFocused(s Setup) string {
	if s.focus < 0 {
		return ""
	}
	return setupButtons[s.focus].label
}

func TestSetupEnterInTheTextBoxSaves(t *testing.T) {
	s, cmd := setupSend(NewSetup("/vms"), enter)
	if s.Dir != "/vms" || s.Cancelled() || !quits(cmd) {
		t.Fatalf("Dir=%q cancelled=%v quits=%v", s.Dir, s.Cancelled(), quits(cmd))
	}
}

func TestSetupEnterWithNothingTypedDoesNotSave(t *testing.T) {
	s := NewSetup("")
	s, cmd := setupSend(s, enter)
	if s.Dir != "" || quits(cmd) {
		t.Fatalf("saved an empty directory: Dir=%q", s.Dir)
	}
}

func TestSetupTabGoesFromTheTextBoxToSaveThenQuitThenBack(t *testing.T) {
	s := NewSetup("/vms")
	var seen []string
	for range 3 {
		s, _ = setupSend(s, tabKey)
		seen = append(seen, setupFocused(s))
	}
	if got := strings.Join(seen, ","); got != "Save,Quit," {
		t.Fatalf("tab order %q, want Save,Quit then the text box", got)
	}
	s, _ = setupSend(s, shiftTabKey)
	if setupFocused(s) != "Quit" {
		t.Fatalf("shift+tab from the text box: %q, want Quit", setupFocused(s))
	}
}

func TestSetupButtonsPressWithEnterOrSpace(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{"enter": enter, "space": spaceKey} {
		s, cmd := setupSend(NewSetup("/vms"), tabKey, key)
		if s.Dir != "/vms" || !quits(cmd) {
			t.Errorf("%s on Save: Dir=%q quits=%v", name, s.Dir, quits(cmd))
		}
		s, cmd = setupSend(NewSetup("/vms"), tabKey, tabKey, key)
		if !s.Cancelled() || s.Dir != "" || !quits(cmd) {
			t.Errorf("%s on Quit: cancelled=%v Dir=%q quits=%v", name, s.Cancelled(), s.Dir, quits(cmd))
		}
	}
}

func TestSetupEscQuits(t *testing.T) {
	s, cmd := setupSend(NewSetup("/vms"), escKey)
	if !s.Cancelled() || !quits(cmd) {
		t.Fatalf("cancelled=%v quits=%v", s.Cancelled(), quits(cmd))
	}
}

func TestSetupTypingFromAButtonGoesBackToTheTextBox(t *testing.T) {
	s, _ := setupSend(NewSetup("/vms"), tabKey, runes("2"))
	if setupFocused(s) != "" {
		t.Fatalf("%q still focused", setupFocused(s))
	}
	s, _ = setupSend(s, enter)
	if s.Dir != "/vms2" {
		t.Fatalf("Dir=%q, want the typed character appended", s.Dir)
	}
}

func TestSetupDrawsItsButtonsAndHints(t *testing.T) {
	v := ansi.Strip(NewSetup("/vms").View())
	for _, want := range []string{"[ Save ]", "[ Quit ]", "[x]", "tab"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "[ Save ]") > strings.Index(v, "[ Quit ]") {
		t.Errorf("the default, Save, should come first:\n%s", v)
	}
}

func clickSetup(t *testing.T, s Setup, text string) (Setup, tea.Cmd) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(s.View()), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			col := ansi.StringWidth(line[:i]) // columns, not bytes: the border is multi-byte
			return setupSend(s, tea.MouseMsg{X: col + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		}
	}
	t.Fatalf("no %q on screen:\n%s", text, ansi.Strip(s.View()))
	return s, nil
}

func TestSetupButtonsAndCloseButtonRespondToClicks(t *testing.T) {
	s, cmd := clickSetup(t, NewSetup("/vms"), "[ Save ]")
	if s.Dir != "/vms" || !quits(cmd) {
		t.Errorf("Save: Dir=%q quits=%v", s.Dir, quits(cmd))
	}
	s, cmd = clickSetup(t, NewSetup("/vms"), "[ Quit ]")
	if !s.Cancelled() || !quits(cmd) {
		t.Errorf("Quit: cancelled=%v quits=%v", s.Cancelled(), quits(cmd))
	}
	s, cmd = clickSetup(t, NewSetup("/vms"), "[x]")
	if !s.Cancelled() || !quits(cmd) {
		t.Errorf("[x]: cancelled=%v quits=%v", s.Cancelled(), quits(cmd))
	}
}

func TestSetupTextBoxHasTheCursorOnlyWhileItHasTheFocus(t *testing.T) {
	s := NewSetup("/vms")
	if !s.input.Focused() {
		t.Fatal("the text box should start focused")
	}
	s, _ = setupSend(s, tabKey) // Save
	if s.input.Focused() {
		t.Error("the text box still has the cursor while Save is focused")
	}
	s, _ = setupSend(s, tabKey, tabKey) // Quit, then back to the text box
	if !s.input.Focused() {
		t.Error("the text box lost the cursor when the focus came back to it")
	}
	s, _ = setupSend(s, shiftTabKey) // Quit
	if s.input.Focused() {
		t.Error("shift+tab onto a button left the cursor in the text box")
	}
}

func TestSavingAnEmptyDirectoryExplainsWhyNothingHappened(t *testing.T) {
	for name, press := range map[string]func(Setup) (Setup, tea.Cmd){
		"enter in the box":  func(s Setup) (Setup, tea.Cmd) { return setupSend(s, enter) },
		"the Save button":   func(s Setup) (Setup, tea.Cmd) { return setupSend(s, tabKey, enter) },
		"a click on Save":   func(s Setup) (Setup, tea.Cmd) { return clickSetup(t, s, "[ Save ]") },
		"space on the Save": func(s Setup) (Setup, tea.Cmd) { return setupSend(s, tabKey, spaceKey) },
	} {
		s := NewSetup("")
		if strings.Contains(ansi.Strip(s.View()), "Enter a directory") {
			t.Fatalf("%s: the message is there before anything was tried", name)
		}
		s, cmd := press(s)
		if s.Dir != "" || quits(cmd) {
			t.Errorf("%s: saved an empty directory (Dir=%q)", name, s.Dir)
		}
		if v := ansi.Strip(s.View()); !strings.Contains(v, "Enter a directory") {
			t.Errorf("%s: no explanation shown:\n%s", name, v)
		}
	}
}

func TestTheEmptyDirectoryMessageGoesAwayOnTheNextKeyAndTheDialogKeepsItsSize(t *testing.T) {
	s := NewSetup("")
	quiet := strings.Count(s.View(), "\n")
	s, _ = setupSend(s, enter)
	if got := strings.Count(s.View(), "\n"); got != quiet {
		t.Errorf("the message changed the dialog's height from %d to %d lines, which would shift the buttons under the mouse", quiet, got)
	}
	s, _ = setupSend(s, runes("/"))
	if strings.Contains(ansi.Strip(s.View()), "Enter a directory") {
		t.Error("the message is still there after typing")
	}
	s, cmd := setupSend(s, enter)
	if s.Dir != "/" || !quits(cmd) {
		t.Errorf("Dir=%q quits=%v, want it saved now there is something", s.Dir, quits(cmd))
	}
}

func TestABlankDirectoryOfOnlySpacesIsRefusedToo(t *testing.T) {
	s, cmd := setupSend(NewSetup("   "), enter)
	if s.Dir != "" || quits(cmd) || !strings.Contains(ansi.Strip(s.View()), "Enter a directory") {
		t.Fatalf("Dir=%q quits=%v:\n%s", s.Dir, quits(cmd), ansi.Strip(s.View()))
	}
}
