package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

func newMouseModel(t *testing.T, names ...string) Model {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		must(t, os.WriteFile(filepath.Join(root, n+".conf"), []byte(`guest="linux"`+"\n"), 0o644))
	}
	fakeHost(t, 8, 32<<30)
	m := New(Options{Root: root, Quickemu: filepath.Join(root, "no-quickemu")})
	m.width, m.height = 120, 40
	return m
}

// anywhere and the panes are column ranges for screenAt
var (
	anywhere = [2]int{0, 1 << 20}
	vmList   = func(m Model) [2]int { return [2]int{0, m.listWidth()} }
	details  = func(m Model) [2]int { return [2]int{m.listWidth(), 1 << 20} }
)

// screenAt finds text on the rendered screen within the columns cols, so a
// test clicks where the user would see it.
func screenAt(t *testing.T, m Model, text string, cols [2]int) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		runes := []rune(line)
		for x := cols[0]; x+len([]rune(text)) <= min(len(runes), cols[1]); x++ {
			if string(runes[x:x+len([]rune(text))]) == text {
				return x, y
			}
		}
	}
	t.Fatalf("%q isn't on screen:\n%s", text, ansi.Strip(m.View()))
	return 0, 0
}

func click(t *testing.T, m Model, text string, cols [2]int, button tea.MouseButton) Model {
	t.Helper()
	x, y := screenAt(t, m, text, cols)
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button})
	return next.(Model)
}

func at(m Model, x, y int, button tea.MouseButton) Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button})
	return next.(Model)
}

func TestClickingAVMSelectsItThenOpensItsMenu(t *testing.T) {
	m := newMouseModel(t, "alpha", "bravo", "charlie")
	m = click(t, m, "bravo", vmList(m), tea.MouseButtonLeft)
	if vm, _ := m.selected(); vm.Name() != "bravo" || m.mode != modeNormal {
		t.Fatalf("first click: selected %q, mode %v", vm.Name(), m.mode)
	}
	m = click(t, m, "bravo", vmList(m), tea.MouseButtonLeft)
	if m.mode != modeMenu {
		t.Fatalf("clicking the selected VM left mode %v, want the menu", m.mode)
	}
}

func TestRightClickingAVMOpensItsMenu(t *testing.T) {
	m := newMouseModel(t, "alpha", "bravo")
	m = click(t, m, "bravo", vmList(m), tea.MouseButtonRight)
	if vm, _ := m.selected(); vm.Name() != "bravo" || m.mode != modeMenu {
		t.Fatalf("selected %q, mode %v; want bravo's menu", vm.Name(), m.mode)
	}
}

func TestClickingAMenuItemRunsIt(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.openMenu()
	m = click(t, m, "Quick Settings…", anywhere, tea.MouseButtonLeft)
	if m.mode != modeQuickSettings {
		t.Fatalf("mode %v, want quick settings", m.mode)
	}

	m.mode = modeNormal
	m.openMenu()
	m = click(t, m, "Start", anywhere, tea.MouseButtonLeft)
	// quickemu is missing, so starting fails, showing why
	if m.mode != modeError || !strings.Contains(m.flash, "Starting alpha failed") {
		t.Errorf("clicking Start: mode %v, flash %q", m.mode, m.flash)
	}
}

func TestClickingOutsideTheMenuClosesIt(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.openMenu()
	m = at(m, 0, m.height-3, tea.MouseButtonLeft)
	if m.mode != modeNormal {
		t.Errorf("mode %v, want the menu closed", m.mode)
	}
	if m.flash != "" {
		t.Errorf("an action ran: %q", m.flash)
	}
}

func TestMenuGapsAndTitleAreNotItems(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.openMenu()
	items := m.menuItems()
	box := m.viewMenu()
	_, y := m.modalAt(box)
	for _, line := range []int{0, 1, 5} { // title, blank, the gap before Create snapshot
		if i := menuItemAt(items, line); i != -1 {
			t.Errorf("line %d is item %d", line, i)
		}
	}
	m = at(m, m.width/2, y+modalInsetY, tea.MouseButtonLeft) // the title
	if m.mode != modeMenu {
		t.Errorf("clicking the title left mode %v", m.mode)
	}
}

func TestClickingASnapshotSelectsIt(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.disks[m.vms[0].ConfPath] = diskState{loaded: true, info: qemu.DiskInfo{Snapshots: []qemu.Snapshot{
		{ID: "1", Name: "pristine"}, {ID: "2", Name: "updated"}, {ID: "3", Name: "broken"},
	}}}
	m = click(t, m, "updated", details(m), tea.MouseButtonLeft)
	if m.pane != paneSnapshots || m.snapCursor != 1 {
		t.Errorf("pane %v, snapshot %d; want the snapshots pane on 1", m.pane, m.snapCursor)
	}
}

func TestWheelMovesTheListUnderIt(t *testing.T) {
	m := newMouseModel(t, "alpha", "bravo")
	x, y := screenAt(t, m, "alpha", vmList(m))
	m = at(m, x, y, tea.MouseButtonWheelDown)
	if m.cursor != 1 {
		t.Errorf("cursor %d after wheel down, want 1", m.cursor)
	}
	m = at(m, x, y, tea.MouseButtonWheelUp)
	if m.cursor != 0 {
		t.Errorf("cursor %d after wheel up, want 0", m.cursor)
	}
}

func TestQuickSettingsClicks(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.openQuickSettings(m.vms[0])
	m = click(t, m, "1024x768", anywhere, tea.MouseButtonLeft)
	if m.qs.group != qsDisplay || m.qs.picked(qsDisplay) != "1024x768" {
		t.Fatalf("group %d, display %q", m.qs.group, m.qs.picked(qsDisplay))
	}
	m = click(t, m, "2 cores", anywhere, tea.MouseButtonLeft)
	if m.qs.group != qsCPU || m.qs.picked(qsCPU) != "2" {
		t.Fatalf("group %d, cpu %q", m.qs.group, m.qs.picked(qsCPU))
	}
	m = click(t, m, "OpenGL", anywhere, tea.MouseButtonLeft)
	if m.qs.group != qsGL || m.qs.gl {
		t.Fatalf("group %d, gl %v; want the checkbox unticked", m.qs.group, m.qs.gl)
	}
	m = at(m, 0, 1, tea.MouseButtonLeft) // outside: saves and closes
	if m.mode != modeNormal {
		t.Fatalf("mode %v, want closed", m.mode)
	}
	data, _ := os.ReadFile(m.vms[0].ConfPath)
	for _, want := range []string{`cpu_cores="2"`, `width="1024"`, `height="768"`, `gl="off"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf(".conf lacks %s:\n%s", want, data)
		}
	}
}

func TestQuickSettingsClicksWhenStacked(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.width = 60 // too narrow for the groups side by side
	m.openQuickSettings(m.vms[0])
	m = click(t, m, "fullscreen", anywhere, tea.MouseButtonLeft)
	if m.qs.picked(qsDisplay) != "fullscreen" {
		t.Errorf("display %q, want fullscreen", m.qs.picked(qsDisplay))
	}
}

func TestWheelInOtherDialogsActsAsArrows(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.showError("boom", strings.Repeat("line\n", 100))
	before := m.errView.YOffset
	m = at(m, 0, 0, tea.MouseButtonWheelDown)
	if m.errView.YOffset <= before {
		t.Errorf("wheel didn't scroll the error dialog")
	}
}

func TestClickingAVMInAScrolledList(t *testing.T) {
	var names []string
	for i := range 30 {
		names = append(names, fmt.Sprintf("vm-%02d", i))
	}
	m := newMouseModel(t, names...)
	m.height = 15
	m.cursor = 25 // the list scrolls to keep it in view
	m = click(t, m, "vm-22", vmList(m), tea.MouseButtonLeft)
	if vm, _ := m.selected(); vm.Name() != "vm-22" {
		t.Errorf("selected %q, want vm-22", vm.Name())
	}
}

func TestEveryDialogHasAWorkingCloseButton(t *testing.T) {
	snaps := []qemu.Snapshot{{ID: "1", Name: "pristine"}}
	for name, tc := range map[string]struct {
		open func(m *Model)
		want mode // the mode closing returns to
	}{
		"menu":          {func(m *Model) { m.openMenu() }, modeNormal},
		"prompt":        {func(m *Model) { m.askPrompt("Name?", "OK", "x", nil) }, modeNormal},
		"confirm":       {func(m *Model) { m.askConfirm("Sure?", defaultYes, nil) }, modeNormal},
		"error":         {func(m *Model) { m.showError("boom", "it broke") }, modeNormal},
		"media":         {func(m *Model) { m.mode, m.mediaVM = modeMedia, m.vms[0] }, modeNormal},
		"logs":          {func(m *Model) { m.mode = modeLogs; m.loadLog() }, modeNormal},
		"snap delete":   {func(m *Model) { m.openSnapDelete(m.vms[0]) }, modeNormal},
		"snap revert":   {func(m *Model) { m.openSnapRevert(m.vms[0]) }, modeNormal},
		"defaults":      {func(m *Model) { m.openDefaults() }, modeNormal},
		"quick setting": {func(m *Model) { m.openQuickSettings(m.vms[0]) }, modeNormal},
		"install pick": {func(m *Model) {
			m.openInstall()
			m.instStep = stepEdition // esc would only step back from here
		}, modeNormal},
		"install progress": {func(m *Model) {
			m.install = &installState{title: "Fedora 41", percent: -1}
			m.openInstall()
		}, modeNormal},
		"confirm over the menu": {func(m *Model) {
			m.mode = modeMenu
			m.askConfirm("Sure?", defaultNo, nil)
		}, modeMenu},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMouseModel(t, "alpha")
			m.disks[m.vms[0].ConfPath] = diskState{loaded: true, info: qemu.DiskInfo{Snapshots: snaps}}
			tc.open(&m)
			opened := m.mode
			if opened == modeNormal {
				t.Fatal("the dialog didn't open")
			}
			m = click(t, m, closeButton, anywhere, tea.MouseButtonLeft)
			if m.mode != tc.want {
				t.Errorf("mode %v after clicking %s, want %v", m.mode, closeButton, tc.want)
			}
		})
	}
}

func TestClickingNextToTheCloseButtonDoesNotClose(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.showError("boom", "it broke")
	x, y := screenAt(t, m, closeButton, anywhere)
	for _, dx := range []int{-1, len(closeButton)} {
		if next := at(m, x+dx, y, tea.MouseButtonLeft); next.mode != modeError {
			t.Errorf("a click %d cells from the button closed the dialog", dx)
		}
	}
}

func TestPromptButtonSubmitsLikeEnter(t *testing.T) {
	m := newMouseModel(t, "alpha")
	got := ""
	m.askPrompt("Name?", "Save", "typed", func(m *Model, v string) tea.Cmd { got = v; return nil })
	m = click(t, m, "[ Save ]", anywhere, tea.MouseButtonLeft)
	if got != "typed" || m.mode != modeNormal {
		t.Errorf("submitted %q, mode %v; want \"typed\" and closed", got, m.mode)
	}
}

func TestClickingBesideThePromptButtonDoesNothing(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.askPrompt("Name?", "Save", "typed", func(m *Model, v string) tea.Cmd { t.Error("submitted"); return nil })
	x, y := screenAt(t, m, "[ Save ]", anywhere)
	for _, dx := range []int{-1, len("[ Save ]")} {
		if next := at(m, x+dx, y, tea.MouseButtonLeft); next.mode != modePrompt {
			t.Errorf("a click %d cells from the button closed the prompt", dx)
		}
	}
}

func TestCreateSnapshotPromptHasASaveButton(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.openMenu()
	m = typed(m, "c")
	if m.mode != modePrompt || m.promptButton != "Save" {
		t.Fatalf("mode %v, button %q; want the snapshot prompt with Save", m.mode, m.promptButton)
	}
	m = click(t, m, "[ Save ]", anywhere, tea.MouseButtonLeft)
	if m.mode == modePrompt {
		t.Error("clicking Save left the prompt open")
	}
}

func TestQuickSettingsSaveButton(t *testing.T) {
	m := newMouseModel(t, "alpha")
	m.openQuickSettings(m.vms[0])
	m = click(t, m, "1920x1080", anywhere, tea.MouseButtonLeft)
	m = click(t, m, "[ Save ]", anywhere, tea.MouseButtonLeft)
	if m.mode != modeNormal {
		t.Fatalf("mode %v, want closed", m.mode)
	}
	if data, _ := os.ReadFile(m.vms[0].ConfPath); !strings.Contains(string(data), `width="1920"`) {
		t.Errorf(".conf not saved:\n%s", data)
	}
}
