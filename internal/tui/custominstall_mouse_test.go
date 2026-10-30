package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// clickCmd is click, keeping the command the click produced.
func clickCmd(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	x, y := screenAt(t, m, text, anywhere)
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return next.(Model), cmd
}

func clickText(t *testing.T, m Model, text string) Model {
	t.Helper()
	m, _ = clickCmd(t, m, text)
	return m
}

// clickPickerRow clicks the i'th row of the install picker's list.
func clickPickerRow(m Model, i int) Model {
	box := m.dialogBox()
	bx, by := m.modalAt(box)
	return at(m, bx+modalInsetX+2, by+modalInsetY+m.instListTop()+i, tea.MouseButtonLeft)
}

// arrowAt is where the ‹ and › of the settings row whose title starts with title are drawn.
func arrowAt(t *testing.T, m Model, title, value string) (leftX, rightX, y int) {
	t.Helper()
	x, y := screenAt(t, m, title, anywhere)
	left, right := customArrowCols(value)
	leftX, rightX = x-1+left, x-1+right
	line := []rune(strings.Split(ansi.Strip(m.View()), "\n")[y])
	if line[leftX] != '‹' || line[rightX] != '›' {
		t.Fatalf("the arrows aren't where customArrowCols says (%d, %d): %q", leftX, rightX, string(line))
	}
	return leftX, rightX, y
}

func TestCustomISOFlowCanBeDrivenByTheMouse(t *testing.T) {
	m := newInstallModel(t)
	root := m.opts.Root
	src := filepath.Join(t.TempDir(), "mouse.iso")
	writeISO(t, src)

	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = clickText(t, m, "Custom ISO…")
	if m.mode != modePrompt {
		t.Fatalf("clicking the row left mode %v, want the prompt", m.mode)
	}
	m = typed(m, src)
	m = clickText(t, m, buttonText("Next")) // the source
	m = clickText(t, m, buttonText("Next")) // the suggested name
	m = clickText(t, m, buttonText("Next")) // no checksum
	if m.instStep != stepCustomOptions {
		t.Fatalf("step %v (flash %q), want the settings", m.instStep, m.flash)
	}

	// pick a row, change it with the arrows, and a click elsewhere on a row only selects it
	left, right, y := arrowAt(t, m, "Boot", "efi")
	m = at(m, right, y, tea.MouseButtonLeft)
	if m.custom.row != customRowBoot || m.custom.choice[customRowBoot] != 1 {
		t.Fatalf("row %d, boot choice %d; want the boot row on legacy", m.custom.row, m.custom.choice[customRowBoot])
	}
	m = at(m, right, y, tea.MouseButtonLeft) // already the last choice
	if m.custom.choice[customRowBoot] != 1 {
		t.Errorf("stepped past the last choice: %d", m.custom.choice[customRowBoot])
	}
	_, _, yL := arrowAt(t, m, "Boot", "legacy")
	m = at(m, left, yL, tea.MouseButtonLeft)
	if m.custom.choice[customRowBoot] != 0 {
		t.Errorf("‹ didn't go back: %d", m.custom.choice[customRowBoot])
	}
	_, right, y = arrowAt(t, m, "Boot", "efi")
	m = at(m, right, y, tea.MouseButtonLeft) // legacy, for good
	osX, _, osY := arrowAt(t, m, "System type", "linux")
	m = at(m, osX+3, osY, tea.MouseButtonLeft) // on the value: selects the row only
	if m.custom.row != customRowOS || m.custom.choice[customRowOS] != 0 {
		t.Errorf("row %d, os choice %d", m.custom.row, m.custom.choice[customRowOS])
	}

	m = clickText(t, m, buttonText("Next"))
	if m.instStep != stepCPU {
		t.Fatalf("step %v, want the CPU step", m.instStep)
	}
	m = clickText(t, m, "2 cores")
	if m.instStep != stepRAM || m.instCores != "2" {
		t.Fatalf("step %v cores %q", m.instStep, m.instCores)
	}
	m = clickPickerRow(m, 1) // 4G: after auto
	if m.mode != modeConfirm || m.instRAM != "4G" {
		t.Fatalf("mode %v ram %q", m.mode, m.instRAM)
	}

	m, cmd := clickCmd(t, m, buttonText("Yes"))
	if m.mode != modeInstallProgress || cmd == nil {
		t.Fatalf("mode %v after Yes", m.mode)
	}
	m = clickText(t, m, buttonText("Background"))
	if m.mode != modeNormal || m.install == nil {
		t.Fatalf("mode %v, install running %v; want it in the background", m.mode, m.install != nil)
	}
	m = finishInstall(t, m, cmd)

	conf, err := qemu.VM{ConfPath: filepath.Join(root, "mouse.conf")}.Conf()
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"boot": "legacy", "guest_os": "linux", "cpu_cores": "2", "ram": "4G"} {
		if conf[k] != want {
			t.Errorf("%s = %q, want %q", k, conf[k], want)
		}
	}
}

func TestClickingAPickerRowChoosesIt(t *testing.T) {
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = clickText(t, m, "Windows")
	if m.instOS != "windows" {
		t.Errorf("os %q, want windows", m.instOS)
	}
}

func TestClickingOutsideThePickerRowsDoesNothing(t *testing.T) {
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = at(m, 0, 0, tea.MouseButtonLeft) // outside the dialog
	if m.mode != modeInstallPick || m.instStep != stepOS {
		t.Errorf("mode %v step %v", m.mode, m.instStep)
	}
	m = clickText(t, m, "filter:") // inside, but not a row
	if m.mode != modeInstallPick || m.instStep != stepOS {
		t.Errorf("mode %v step %v", m.mode, m.instStep)
	}
}

func TestConfirmDialogsHaveClickableYesAndNo(t *testing.T) {
	for name, tc := range map[string]struct {
		button string
		want   bool
	}{"Yes runs it": {confirmYes, true}, "No doesn't": {confirmNo, false}} {
		t.Run(name, func(t *testing.T) {
			m := newMouseModel(t, "alpha")
			ran := false
			m.askConfirm("Do the thing?", defaultNo, func(m *Model) tea.Cmd { ran = true; return nil })
			m = clickText(t, m, buttonText(tc.button))
			if ran != tc.want || m.mode != modeNormal {
				t.Errorf("ran %v, mode %v", ran, m.mode)
			}
		})
	}
}

func TestProgressDialogCancelButtonAsksFirst(t *testing.T) {
	m := newInstallModel(t)
	cancelled := false
	m.install = &installState{title: "x", percent: -1, msgs: make(chan tea.Msg, 1), cancel: func() { cancelled = true }}
	m.mode = modeInstallProgress
	m = clickText(t, m, buttonText("Cancel"))
	if m.mode != modeConfirm || cancelled {
		t.Fatalf("mode %v cancelled %v, want to be asked", m.mode, cancelled)
	}
	m = clickText(t, m, buttonText(confirmNo))
	if cancelled || m.mode != modeInstallProgress {
		t.Errorf("cancelled %v mode %v after No", cancelled, m.mode)
	}
	m = clickText(t, m, buttonText("Cancel"))
	m = clickText(t, m, buttonText(confirmYes))
	if !cancelled {
		t.Error("Yes didn't cancel")
	}
}

func TestSettingsScreenHasANextButtonAndAWheel(t *testing.T) {
	m := newInstallModel(t)
	src := filepath.Join(t.TempDir(), "w.iso")
	writeISO(t, src)
	m = customAtSettings(t, m, src, "")
	m = at(m, 0, 0, tea.MouseButtonWheelDown) // the wheel stands in for ↓
	if m.custom.row != 1 {
		t.Errorf("row %d after the wheel, want 1", m.custom.row)
	}
	if v := ansi.Strip(m.viewInstallPick()); !strings.Contains(v, buttonText("Next")) {
		t.Errorf("no Next button:\n%s", v)
	}
}
