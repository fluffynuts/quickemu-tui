package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

func TestOverlayKeepsSurroundingContent(t *testing.T) {
	base := strings.Repeat(strings.Repeat(".", 20)+"\n", 5) + strings.Repeat(".", 20)
	out := overlay(base, "XX\nXX", 20, 6)
	rows := strings.Split(out, "\n")
	if len(rows) != 6 {
		t.Fatalf("got %d rows", len(rows))
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != 20 {
			t.Errorf("row %d width %d, want 20", i, w)
		}
	}
	if !strings.Contains(rows[2], "XX") || !strings.HasPrefix(rows[2], "........") {
		t.Errorf("box not centred over base: %q", rows[2])
	}
	if strings.Contains(rows[0], "X") {
		t.Errorf("box leaked into row 0")
	}
}

func TestMenuEnterOpensAndEscCloses(t *testing.T) {
	m := New(Options{Root: t.TempDir()})
	m.openMenu() // no VMs: stays closed
	if m.mode != modeNormal {
		t.Fatal("menu opened with no VM selected")
	}
}

func TestActionKeysOnlyWorkInsideMenu(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte(`guest="linux"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	key := func(m Model, k string) Model {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		return next.(Model)
	}

	// 'c' (create snapshot) from the listing does nothing
	m = key(m, "c")
	if m.flash != "" || m.mode != modeNormal {
		t.Fatalf("hotkey acted from listing: mode=%v flash=%q", m.mode, m.flash)
	}

	// the same key inside the menu runs the action (here: the snapshot-name prompt)
	m.openMenu()
	if m.mode != modeMenu {
		t.Fatal("menu did not open")
	}
	m = key(m, "c")
	if m.mode != modePrompt {
		t.Fatalf("hotkey ignored in menu: mode=%v flash=%q", m.mode, m.flash)
	}
}

func TestDeleteSnapshotsMultiSelect(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte(`guest="linux"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 30
	conf := m.vms[0].ConfPath
	m.disks[conf] = diskState{loaded: true, info: qemu.DiskInfo{Snapshots: []qemu.Snapshot{
		{ID: "1", Name: "a"}, {ID: "2", Name: "b"}, {ID: "3", Name: "c"},
	}}}
	key := func(m Model, k tea.KeyMsg) Model {
		next, _ := m.Update(k)
		return next.(Model)
	}
	run := func(m Model, s string) Model { return key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }

	m.openMenu()
	m = run(m, "d")
	if m.mode != modeSnapDelete {
		t.Fatalf("mode = %v, want snapshot picker", m.mode)
	}
	// enter with nothing ticked must not proceed
	m = key(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeSnapDelete || !m.flashErr {
		t.Fatalf("empty selection should be refused: mode=%v flash=%q", m.mode, m.flash)
	}
	// tick 1st and 3rd
	m = key(m, tea.KeyMsg{Type: tea.KeySpace})
	m = run(m, "j")
	m = run(m, "j")
	m = key(m, tea.KeyMsg{Type: tea.KeySpace})
	if got := len(m.pickedSnapshots(m.snapshots(m.delVM))); got != 2 {
		t.Fatalf("ticked %d, want 2", got)
	}
	if v := m.viewSnapDelete(); !strings.Contains(v, "[x] a") || !strings.Contains(v, "[ ] b") || !strings.Contains(v, "[x] c") {
		t.Fatalf("checkboxes wrong:\n%s", v)
	}
	m = key(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "2 snapshots") || !strings.Contains(m.confirmText, "'a', 'c'") {
		t.Fatalf("confirm wrong: mode=%v %q", m.mode, m.confirmText)
	}
}

func TestRevertSnapshotPicker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte(`guest="linux"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 30
	conf := m.vms[0].ConfPath
	m.disks[conf] = diskState{loaded: true, info: qemu.DiskInfo{Snapshots: []qemu.Snapshot{
		{ID: "1", Name: "pristine"}, {ID: "2", Name: "configured"},
	}}}
	key := func(m Model, k tea.KeyMsg) Model {
		next, _ := m.Update(k)
		return next.(Model)
	}
	run := func(m Model, s string) Model { return key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }

	m.openMenu()
	for _, it := range m.menuItems() {
		if strings.Contains(it.label, "pristine") {
			t.Fatalf("menu names a specific snapshot: %q", it.label)
		}
	}
	m = run(m, "a")
	if m.mode != modeSnapRevert {
		t.Fatalf("mode = %v, want revert picker", m.mode)
	}
	if v := m.viewSnapRevert(); !strings.Contains(v, "pristine") || !strings.Contains(v, "configured") {
		t.Fatalf("picker should list every snapshot:\n%s", v)
	}
	m = run(m, "j")
	m = key(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "'configured'") || strings.Contains(m.confirmText, "and start") {
		t.Fatalf("confirm wrong: mode=%v %q", m.mode, m.confirmText)
	}
	// declining returns to the picker; s asks to revert and start
	m = run(m, "n")
	if m.mode != modeSnapRevert {
		t.Fatalf("after declining mode = %v, want revert picker", m.mode)
	}
	m = run(m, "s")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "Revert and start") || !strings.Contains(m.confirmText, "'configured'") {
		t.Fatalf("confirm wrong: mode=%v %q", m.mode, m.confirmText)
	}
}

func TestFailedOperationOpensScrollableErrorDialog(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte(`guest="linux"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 30
	m.busy = 1
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("detail line %02d", i))
	}
	next, _ := m.Update(opDoneMsg{label: "Delete 2 snapshots", conf: m.vms[0].ConfPath, err: errors.New(strings.Join(lines, "\n"))})
	m = next.(Model)
	if m.mode != modeError {
		t.Fatalf("mode = %v, want error dialog", m.mode)
	}
	if v := m.viewModal(); !strings.Contains(v, "Delete 2 snapshots failed") || !strings.Contains(v, "detail line 00") || strings.Contains(v, "detail line 59") {
		t.Fatalf("expected title and the top of the text only:\n%s", v)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(Model)
	if v := m.viewModal(); !strings.Contains(v, "detail line 59") {
		t.Fatalf("end key didn't scroll to the bottom:\n%s", v)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := next.(Model).mode; got != modeNormal {
		t.Fatalf("esc returned to mode %v", got)
	}
}
