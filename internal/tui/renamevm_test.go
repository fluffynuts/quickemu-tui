package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRenameVMFlowSelectsTheRenamedVM(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	for _, n := range []string{"alpha", "mid"} {
		conf := "guest_os=\"linux\"\ndisk_img=\"" + n + "/disk.qcow2\"\n"
		if err := os.WriteFile(filepath.Join(root, n+".conf"), []byte(conf), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 30

	m.openMenu()
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	if m.mode != modePrompt || m.input.Value() != "alpha" {
		t.Fatalf("mode=%v input=%q, want a prompt prefilled with the name", m.mode, m.input.Value())
	}
	m.input.SetValue("zulu")
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm || m.confirmDefault != defaultYes || !strings.Contains(m.confirmText, "alpha/ → ") {
		t.Fatalf("mode=%v default=%v confirm=%q", m.mode, m.confirmDefault, m.confirmText)
	}
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyEnter}) // Y/n: enter renames

	var done opDoneMsg
	var collect func(tea.Cmd)
	collect = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				collect(sub)
			}
		case opDoneMsg:
			done = msg
		}
	}
	collect(cmd)
	if done.err != nil {
		t.Fatalf("rename failed: %v", done.err)
	}
	next, _ := m.Update(done)
	m = next.(Model)
	vm, ok := m.selected()
	if !ok || vm.Name() != "zulu" {
		t.Fatalf("selected %q, want the renamed VM (zulu, now listed after mid)", vm.Name())
	}
	if _, err := os.Stat(filepath.Join(root, "zulu", "")); err != nil {
		t.Fatalf("folder not renamed: %v", err)
	}
}

func TestRenameVMRefusedWhileRunning(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.launching[m.vms[0].ConfPath] = true
	m.openMenu()
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	if m.mode != modeNormal || !m.flashErr || !strings.Contains(m.flash, "can't be renamed") {
		t.Fatalf("mode=%v flash=%q", m.mode, m.flash)
	}
}
