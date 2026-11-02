package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// dialogOpeners opens every dialog mode. A new mode with no entry here fails
// TestEveryDialogModeFollowsTheButtonRules, which is the point: a dialog added
// without buttons, tab stops and key hints would be the odd one out (ADR 0018).
func dialogOpeners() map[mode]func(*Model) {
	return map[mode]func(*Model){
		modePrompt:          func(m *Model) { m.askPrompt("Name?", "Save", "x", nil) },
		modeConfirm:         func(m *Model) { m.askConfirm("Sure?", defaultYes, nil) },
		modeMedia:           func(m *Model) { m.mode, m.mediaVM = modeMedia, m.vms[0] },
		modeLogs:            func(m *Model) { m.mode = modeLogs; m.loadLog() },
		modeMenu:            func(m *Model) { m.openMenu() },
		modeSnapDelete:      func(m *Model) { m.openSnapDelete(m.vms[0]) },
		modeSnapRevert:      func(m *Model) { m.openSnapRevert(m.vms[0]) },
		modeResize:          func(m *Model) { m.openResize(m.vms[0], false) },
		modeError:           func(m *Model) { m.showError("boom", "it broke") },
		modeInstallPick:     func(m *Model) { m.openInstall() },
		modeInstallProgress: func(m *Model) { m.install = &installState{title: "x", percent: -1, cancel: func() {}}; m.openInstall() },
		modeDefaults:        func(m *Model) { m.openDefaults() },
		modeQuickSettings:   func(m *Model) { m.openQuickSettings(m.vms[0]) },
	}
}

func TestEveryDialogModeFollowsTheButtonRules(t *testing.T) {
	openers := dialogOpeners()
	for md := modeNormal + 1; md < modeCount; md++ {
		open, ok := openers[md]
		if !ok {
			t.Errorf("mode %d has no opener in dialogOpeners: add it, and give the dialog its buttons, tab stops and key hints (ADR 0018)", md)
			continue
		}
		t.Run(modeName(md), func(t *testing.T) {
			m := dialogModel(t) // one VM with a disk and a snapshot
			cat, err := qemu.ParseCatalog([]byte(testCSV))
			if err != nil {
				t.Fatal(err)
			}
			m.catalog = cat // for the install picker
			open(&m)
			if m.mode != md {
				t.Fatalf("the opener left mode %d, not %d", m.mode, md)
			}
			buttons := m.dialogButtons()
			if len(buttons) == 0 {
				t.Fatal("no buttons")
			}

			// they are drawn, in order, under distinct names (a click finds a button by its text)
			box := ansi.Strip(m.dialogBox())
			seen, at := map[string]bool{}, -1
			for _, b := range buttons {
				if seen[b.label] {
					t.Errorf("two buttons are called %q", b.label)
				}
				seen[b.label] = true
				i := strings.Index(box, buttonText(b.label))
				if i < 0 {
					t.Errorf("%s isn't drawn:\n%s", buttonText(b.label), box)
					continue
				}
				if i < at {
					t.Errorf("%s is drawn before the button ahead of it in the tab order", buttonText(b.label))
				}
				at = i
				if b.press == nil {
					t.Errorf("%s does nothing", buttonText(b.label))
				}
			}

			// tab goes through the dialog's own stops, then the buttons, then round again
			stops := m.bodyStops()
			steps := stops + len(buttons)
			start := m
			for n := 1; n <= steps; n++ {
				next, _ := m.Update(tabKey)
				m = next.(Model)
				// tabs 1..stops-1 stay in the dialog, stops..stops+buttons-1 are the buttons,
				// and the last one comes back round
				wantButton := n >= stops && n < stops+len(buttons)
				if stops > 0 && wantButton != m.btnFocused {
					t.Fatalf("tab %d: button focused is %v, want %v", n, m.btnFocused, wantButton)
				}
			}
			if stops > 0 && (m.btnFocused || m.bodyStop() != start.bodyStop()) {
				t.Errorf("after %d tabs (%d stops, %d buttons) the focus hasn't come round: button %v, stop %d", steps, stops, len(buttons), m.btnFocused, m.bodyStop())
			}
			if stops == 0 && (!m.btnFocused || m.btnIdx != len(buttons)-1) {
				t.Errorf("a dialog with no stops of its own should cycle its buttons: focused %v index %d of %d", m.btnFocused, m.btnIdx, len(buttons))
			}

			// the key hints mention the tab order, and esc gets out
			m = start
			if hint := ansi.Strip(m.viewKeys()); !strings.Contains(hint, "tab") {
				t.Errorf("the key hints don't mention tab: %q", hint)
			}
			next, _ := m.Update(escKey)
			if next.(Model).mode == md {
				t.Errorf("esc didn't leave the dialog")
			}
		})
	}
}

func modeName(md mode) string {
	names := map[mode]string{
		modePrompt: "prompt", modeConfirm: "confirm", modeMedia: "media", modeLogs: "logs", modeMenu: "menu",
		modeSnapDelete: "snap delete", modeSnapRevert: "snap revert", modeResize: "resize", modeError: "error",
		modeInstallPick: "install pick", modeInstallProgress: "install progress", modeDefaults: "defaults",
		modeQuickSettings: "quick settings",
	}
	if n, ok := names[md]; ok {
		return n
	}
	return "mode"
}
