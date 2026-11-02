package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/config"
	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// everyOtherDialog opens each dialog of this group in a model with one VM that
// has a loaded disk and a snapshot, with the buttons it should have.
func everyOtherDialog(t *testing.T) map[string]struct {
	open    func(m *Model)
	buttons string
} {
	t.Helper()
	return map[string]struct {
		open    func(m *Model)
		buttons string
	}{
		"menu":               {func(m *Model) { m.openMenu() }, "Run,Close"},
		"snap revert":        {func(m *Model) { m.openSnapRevert(m.vms[0]) }, "Revert,Revert and start,Cancel"},
		"snap delete":        {func(m *Model) { m.openSnapDelete(m.vms[0]) }, "Delete ticked,Cancel"},
		"resize":             {func(m *Model) { m.openResize(m.vms[0], false) }, "Choose,Cancel"},
		"media":              {func(m *Model) { m.mode, m.mediaVM = modeMedia, m.vms[0] }, "Insert…,Eject,Reload,Close"},
		"logs":               {func(m *Model) { m.mode = modeLogs; m.loadLog() }, "Reload,Close"},
		"error":              {func(m *Model) { m.showError("boom", "it broke") }, "Close"},
		"notice":             {func(m *Model) { m.showNotice("fyi", "all fine") }, "Close"},
		"default VM options": {func(m *Model) { m.openDefaults() }, "Save,Cancel"},
	}
}

func dialogModel(t *testing.T) Model {
	t.Helper()
	m := newMouseModel(t, "alpha")
	m.disks[m.vms[0].ConfPath] = diskState{loaded: true, info: qemu.DiskInfo{
		VirtualSize: 40 * gibBytes, Snapshots: []qemu.Snapshot{{ID: "1", Name: "pristine"}},
	}}
	return m
}

func TestEveryOtherDialogHasItsButtonsDefaultFirst(t *testing.T) {
	for name, tc := range everyOtherDialog(t) {
		t.Run(name, func(t *testing.T) {
			m := dialogModel(t)
			tc.open(&m)
			if got := labelsOf(m); got != tc.buttons {
				t.Fatalf("buttons = %q, want %q", got, tc.buttons)
			}
			s := ansi.Strip(m.dialogBox())
			for _, label := range strings.Split(tc.buttons, ",") {
				if !strings.Contains(s, buttonText(label)) {
					t.Errorf("%s isn't drawn:\n%s", buttonText(label), s)
				}
			}
		})
	}
}

func TestTabReachesEveryButtonOfEveryOtherDialogThenWraps(t *testing.T) {
	for name, tc := range everyOtherDialog(t) {
		t.Run(name, func(t *testing.T) {
			m := dialogModel(t)
			tc.open(&m)
			var seen []string
			for range strings.Count(tc.buttons, ",") + 1 {
				m, _ = send(m, tabKey)
				seen = append(seen, focused(m))
			}
			if got := strings.Join(seen, ","); got != tc.buttons {
				t.Fatalf("tab order = %q, want %q", got, tc.buttons)
			}
			m, _ = send(m, tabKey)
			if focused(m) != "" {
				t.Fatalf("tab past the last button focused %q, want the dialog's own controls", focused(m))
			}
		})
	}
}

func TestEscClosesEveryOtherDialog(t *testing.T) {
	for name, tc := range everyOtherDialog(t) {
		t.Run(name, func(t *testing.T) {
			m := dialogModel(t)
			tc.open(&m)
			m, _ = send(m, escKey)
			if m.mode != modeNormal {
				t.Fatalf("mode %v after esc", m.mode)
			}
		})
	}
}

func TestTheCloseOrCancelButtonOfEveryOtherDialogCloses(t *testing.T) {
	for name, tc := range everyOtherDialog(t) {
		t.Run(name, func(t *testing.T) {
			m := dialogModel(t)
			tc.open(&m)
			labels := strings.Split(tc.buttons, ",")
			m = pressButton(t, m, labels[len(labels)-1])
			if m.mode != modeNormal {
				t.Fatalf("mode %v after %s", m.mode, labels[len(labels)-1])
			}
		})
	}
}

func TestClickingAButtonOfEveryOtherDialogPressesIt(t *testing.T) {
	for name, tc := range everyOtherDialog(t) {
		t.Run(name, func(t *testing.T) {
			m := dialogModel(t)
			tc.open(&m)
			labels := strings.Split(tc.buttons, ",")
			m = click(t, m, buttonText(labels[len(labels)-1]), anywhere, tea.MouseButtonLeft)
			if m.mode != modeNormal {
				t.Fatalf("mode %v after clicking %s", m.mode, labels[len(labels)-1])
			}
		})
	}
}

// --- the dialogs where enter used to do something else ---

func TestEnterDoesNothingInTheErrorDialogUntilItsButtonIsFocused(t *testing.T) {
	m := dialogModel(t)
	m.showError("boom", "it broke")
	m, _ = send(m, enter)
	if m.mode != modeError {
		t.Fatalf("enter closed the dialog (mode %v)", m.mode)
	}
	m = pressButton(t, m, "Close")
	if m.mode != modeNormal {
		t.Fatalf("Close: mode %v", m.mode)
	}
}

func TestErrorDialogStillScrollsAndQClosesIt(t *testing.T) {
	m := dialogModel(t)
	m.showError("boom", strings.Repeat("line\n", 100))
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.errView.YOffset == 0 {
		t.Error("page down didn't scroll")
	}
	m, _ = send(m, runes("q"))
	if m.mode != modeNormal {
		t.Errorf("q: mode %v", m.mode)
	}
}

func TestErrorDialogReturnsToWhatWasUnderIt(t *testing.T) {
	m := dialogModel(t)
	m.openMenu()
	m.showError("boom", "it broke")
	m = pressButton(t, m, "Close")
	if m.mode != modeMenu {
		t.Fatalf("mode %v, want back at the menu", m.mode)
	}
}

func TestLogsSwitchFileWithTheArrowsNotTab(t *testing.T) {
	m := dialogModel(t)
	m.mode = modeLogs
	m.loadLog()
	m, _ = send(m, rightKey)
	if m.logIndex != 1 {
		t.Fatalf("right: file %d, want 1", m.logIndex)
	}
	m, _ = send(m, rightKey, rightKey)
	if m.logIndex != 0 {
		t.Fatalf("right past the last: file %d, want it to wrap to 0", m.logIndex)
	}
	m, _ = send(m, leftKey)
	if m.logIndex != len(logNames)-1 {
		t.Fatalf("left from the first: file %d, want the last", m.logIndex)
	}
	m, _ = send(m, tabKey)
	if m.logIndex != len(logNames)-1 || focused(m) != "Reload" {
		t.Fatalf("tab changed the file (%d) or missed the Reload button (%q)", m.logIndex, focused(m))
	}
}

func TestLogsSayThatTheArrowsSwitchFile(t *testing.T) {
	m := dialogModel(t)
	m.mode = modeLogs
	m.loadLog()
	if v := ansi.Strip(m.dialogBox()); !strings.Contains(v, "← →") {
		t.Errorf("the dialog doesn't say the arrows switch file:\n%s", v)
	}
	if k := ansi.Strip(m.viewKeys()); !strings.Contains(k, "←/→") {
		t.Errorf("the key hints don't either: %q", k)
	}
}

func TestLogsReloadButtonRereadsTheFile(t *testing.T) {
	m := dialogModel(t)
	p, _ := m.vms[0].Paths()
	must(t, os.MkdirAll(p.VMDir, 0o755))
	m.mode = modeLogs
	m.loadLog()
	must(t, os.WriteFile(p.QuickemuLog, []byte("fresh log text"), 0o644))
	m = pressButton(t, m, "Reload")
	if !strings.Contains(m.logView.View(), "fresh log text") {
		t.Fatalf("not reloaded:\n%s", m.logView.View())
	}
	if m.mode != modeLogs {
		t.Fatalf("Reload closed the dialog (mode %v)", m.mode)
	}
}

func TestDefaultsTabLeavesTheTextBoxAndEnterStillMakesALine(t *testing.T) {
	m := dialogModel(t)
	m.openDefaults()
	m = typed(m, "a")
	m, _ = send(m, enter)
	m = typed(m, "b")
	if !strings.Contains(m.defInput.Value(), "a\nb") {
		t.Fatalf("text = %q, want enter to start a new line", m.defInput.Value())
	}
	m, _ = send(m, tabKey)
	if focused(m) != "Save" || strings.Contains(m.defInput.Value(), "\t") {
		t.Fatalf("tab: focused %q text %q, want the Save button and no tab character", focused(m), m.defInput.Value())
	}
	m, _ = send(m, runes("c"))
	if focused(m) != "" || !strings.HasSuffix(m.defInput.Value(), "bc") {
		t.Fatalf("typing from a button: focused %q text %q", focused(m), m.defInput.Value())
	}
}

func TestDefaultsSaveAndCancelButtons(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	must(t, config.Save(cfg, config.Config{VMDir: "/vms"}))
	open := func() Model {
		m := dialogModel(t)
		m.opts.ConfigPath = cfg
		m.openDefaults()
		m.defInput.SetValue(`gl="off"`)
		return m
	}
	m := pressButton(t, open(), "Cancel")
	if c, _ := config.Load(cfg); len(c.DefaultConf) != 0 || m.mode != modeNormal {
		t.Fatalf("Cancel saved %v (mode %v)", c.DefaultConf, m.mode)
	}
	m = pressButton(t, open(), "Save")
	if c, _ := config.Load(cfg); strings.Join(c.DefaultConf, "|") != `gl="off"` || m.mode != modeNormal {
		t.Fatalf("Save wrote %v (mode %v)", c.DefaultConf, m.mode)
	}
}

// --- the pickers: what each button does ---

func TestMenuRunButtonRunsTheHighlightedItem(t *testing.T) {
	m := dialogModel(t)
	m.openMenu()
	for i, it := range m.menuItems() {
		if it.key == "Q" {
			m.menuCursor = i
		}
	}
	m = pressButton(t, m, "Run")
	if m.mode != modeQuickSettings {
		t.Fatalf("mode %v, want quick settings", m.mode)
	}
}

func TestRevertButtonsAskToRevertWithAndWithoutStarting(t *testing.T) {
	m := dialogModel(t)
	m.openSnapRevert(m.vms[0])
	plain := pressButton(t, m, "Revert")
	if plain.mode != modeConfirm || !strings.HasPrefix(plain.confirmText, "Revert alpha") {
		t.Fatalf("Revert: mode %v text %q", plain.mode, plain.confirmText)
	}
	start := pressButton(t, m, "Revert and start")
	if start.mode != modeConfirm || !strings.HasPrefix(start.confirmText, "Revert and start alpha") {
		t.Fatalf("Revert and start: mode %v text %q", start.mode, start.confirmText)
	}
}

func TestDeleteButtonAsksAboutTheTickedSnapshots(t *testing.T) {
	m := dialogModel(t)
	m.openSnapDelete(m.vms[0])
	m, _ = send(m, spaceKey) // tick "pristine"
	m = pressButton(t, m, "Delete ticked")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "pristine") {
		t.Fatalf("mode %v text %q", m.mode, m.confirmText)
	}
}

func TestResizeChooseButtonAsksForTheHighlightedSize(t *testing.T) {
	m := dialogModel(t)
	m.openResize(m.vms[0], false)
	for range len(resizePresets) {
		m, _ = send(m, runes("j"))
	}
	m = pressButton(t, m, "Choose") // Other size…
	if m.mode != modePrompt {
		t.Fatalf("mode %v, want the size prompt", m.mode)
	}
}

func TestMediaButtonsReloadAndStayOpen(t *testing.T) {
	m := dialogModel(t)
	m.mode, m.mediaVM = modeMedia, m.vms[0]
	m = pressButton(t, m, "Reload")
	if m.mode != modeMedia || !m.mediaLoading {
		t.Fatalf("mode %v loading %v, want it open and asking the monitor again", m.mode, m.mediaLoading)
	}
}
