package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func labelsOf(m Model) string {
	var out []string
	for _, b := range m.dialogButtons() {
		out = append(out, b.label)
	}
	return strings.Join(out, ",")
}

func openInstallPicker(t *testing.T) Model {
	t.Helper()
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	return m
}

func TestInstallPickerButtonsFollowTheStep(t *testing.T) {
	m := openInstallPicker(t)
	if got := labelsOf(m); got != "Select,Cancel" {
		t.Errorf("first step: %s, want Select,Cancel (nowhere to go back to)", got)
	}
	m = typed(m, "ubuntu")
	m, _ = keyOf(m, enter)
	if got := labelsOf(m); got != "Select,Back,Cancel" {
		t.Errorf("release step: %s, want Select,Back,Cancel", got)
	}
	if v := screen(m); !strings.Contains(v, "[ Select ]") || !strings.Contains(v, "[ Back ]") || !strings.Contains(v, "[ Cancel ]") {
		t.Errorf("buttons not drawn:\n%s", v)
	}
}

func TestInstallPickerTabGoesFromTheListToTheButtons(t *testing.T) {
	m := openInstallPicker(t)
	m, _ = send(m, tabKey)
	if focused(m) != "Select" {
		t.Fatalf("first tab focused %q, want Select", focused(m))
	}
	m, _ = send(m, tabKey, tabKey)
	if focused(m) != "" {
		t.Fatalf("tab past the last button focused %q, want back in the list", focused(m))
	}
}

func TestInstallPickerSelectButtonChoosesTheHighlightedRow(t *testing.T) {
	m := openInstallPicker(t)
	m = typed(m, "ubuntu")
	m = pressButton(t, m, "Select")
	if m.instStep != stepRelease || m.instOS != "ubuntu" {
		t.Fatalf("step %v os %q, want the release step of ubuntu", m.instStep, m.instOS)
	}
}

func TestInstallPickerBackGoesBackEvenWithAFilterTyped(t *testing.T) {
	m := openInstallPicker(t)
	m = typed(m, "ubuntu")
	m, _ = keyOf(m, enter)
	m = typed(m, "2")
	m = pressButton(t, m, "Back")
	if m.instStep != stepOS || m.instFilter != "" {
		t.Fatalf("step %v filter %q, want the first step with no filter", m.instStep, m.instFilter)
	}
}

func TestInstallPickerCancelClosesIt(t *testing.T) {
	m := openInstallPicker(t)
	m = pressButton(t, m, "Cancel")
	if m.mode != modeNormal {
		t.Fatalf("mode %v", m.mode)
	}
}

func TestTypingOnAButtonOfTheInstallPickerFiltersTheList(t *testing.T) {
	m := openInstallPicker(t)
	m, _ = send(m, tabKey)
	m, _ = send(m, runes("w"))
	if focused(m) != "" || m.instFilter != "w" {
		t.Fatalf("focused %q filter %q, want the filter typed into", focused(m), m.instFilter)
	}
}

func TestInstallPickerEnterStillChoosesARow(t *testing.T) {
	m := openInstallPicker(t)
	m = typed(m, "ubuntu")
	m, _ = keyOf(m, enter)
	if m.instStep != stepRelease {
		t.Fatalf("step %v", m.instStep)
	}
}

// --- custom ISO settings ---

func customSettings(t *testing.T) Model {
	t.Helper()
	m := newInstallModel(t)
	src := filepath.Join(t.TempDir(), "d.iso")
	writeISO(t, src)
	return customAtSettings(t, m, src, "")
}

func TestCustomSettingsEnterDoesNotAdvance(t *testing.T) {
	m := customSettings(t)
	m, _ = send(m, enter)
	if m.instStep != stepCustomOptions {
		t.Fatalf("enter moved on to step %v", m.instStep)
	}
}

func TestCustomSettingsHaveNextThenBack(t *testing.T) {
	m := customSettings(t)
	if got := labelsOf(m); got != "Next,Back" {
		t.Fatalf("buttons %s, want Next,Back", got)
	}
	back := pressButton(t, m, "Back")
	if back.instStep != stepOS || back.custom.active {
		t.Errorf("Back: step %v active %v, want the first step with the draft dropped", back.instStep, back.custom.active)
	}
	next := pressButton(t, m, "Next")
	if next.instStep != stepCPU {
		t.Errorf("Next: step %v, want the CPU step", next.instStep)
	}
}

func TestWheelOnCustomSettingsMovesRowsAndNeverFocusesAButton(t *testing.T) {
	m := customSettings(t)
	for range 6 {
		m = at(m, 0, 0, tea.MouseButtonWheelDown)
	}
	if m.custom.row != customRows-1 || focused(m) != "" {
		t.Fatalf("row %d focused %q, want the last row and no button", m.custom.row, focused(m))
	}
}

// --- install progress ---

func progressModel(t *testing.T) Model {
	t.Helper()
	m := newInstallModel(t)
	m.install = &installState{title: "Fedora 41", percent: -1, cancel: func() {}}
	m.openInstall()
	return m
}

func TestInstallProgressHasBackgroundThenCancelAndEnterDoesNothing(t *testing.T) {
	m := progressModel(t)
	if m.mode != modeInstallProgress || labelsOf(m) != "Background,Cancel" {
		t.Fatalf("mode %v buttons %s", m.mode, labelsOf(m))
	}
	m, _ = send(m, enter)
	if m.mode != modeInstallProgress {
		t.Fatalf("enter closed the dialog (mode %v)", m.mode)
	}
	m, _ = send(m, tabKey)
	if focused(m) != "Background" {
		t.Fatalf("first tab focused %q, want the default, Background", focused(m))
	}
	m, _ = send(m, enter)
	if m.mode != modeNormal || m.install == nil {
		t.Fatalf("Background: mode %v, install running %v", m.mode, m.install != nil)
	}
}

func TestInstallProgressCancelButtonAsksFirst(t *testing.T) {
	m := progressModel(t)
	m = pressButton(t, m, "Cancel")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "Cancel the install of Fedora 41") {
		t.Fatalf("mode %v text %q", m.mode, m.confirmText)
	}
}

func TestProgressDialogDrawsItsButtons(t *testing.T) {
	m := progressModel(t)
	v := ansi.Strip(m.viewInstallProgress())
	if !strings.Contains(v, "[ Background ]") || !strings.Contains(v, "[ Cancel ]") {
		t.Fatalf("buttons missing:\n%s", v)
	}
}
