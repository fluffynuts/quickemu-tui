package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTwoVMModel(t *testing.T) (Model, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		must(t, os.MkdirAll(filepath.Join(root, name), 0o755))
		must(t, os.WriteFile(filepath.Join(root, name, "disk.qcow2"), []byte("0123456789"), 0o644))
		must(t, os.WriteFile(filepath.Join(root, name+".conf"), []byte("disk_img=\""+name+"/disk.qcow2\"\n"), 0o644))
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 40
	return m, root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteVMNeedsConfirmationAndRemovesFolderAndConf(t *testing.T) {
	m, root := newTwoVMModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown}) // beta
	m.openMenu()

	// the menu offers it
	found := false
	for _, it := range m.menuItems() {
		found = found || (it.key == "D" && strings.Contains(it.label, "Delete VM"))
	}
	if !found {
		t.Fatal("menu has no Delete VM item")
	}

	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want a confirmation", m.mode)
	}
	for _, want := range []string{"Permanently delete beta", "beta.conf", "beta/", "1 file", "cannot be undone"} {
		if !strings.Contains(m.confirmText, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, m.confirmText)
		}
	}

	// declining deletes nothing
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if _, err := os.Stat(filepath.Join(root, "beta", "disk.qcow2")); err != nil {
		t.Fatalf("declined, but files are gone: %v", err)
	}

	// accepting deletes
	m.openMenu()
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	for i := 0; i < 10 && m.busy > 0; i++ {
		var batch tea.Msg = cmd()
		if bm, ok := batch.(tea.BatchMsg); ok {
			for _, c := range bm {
				if c == nil {
					continue
				}
				if d, ok := c().(opDoneMsg); ok {
					next, c2 := m.Update(d)
					m, cmd = next.(Model), c2
				}
			}
		} else if d, ok := batch.(opDoneMsg); ok {
			next, c2 := m.Update(d)
			m, cmd = next.(Model), c2
		}
	}
	if m.busy != 0 {
		t.Fatal("delete never completed")
	}
	if _, err := os.Stat(filepath.Join(root, "beta")); !os.IsNotExist(err) {
		t.Errorf("beta folder still exists (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "beta.conf")); !os.IsNotExist(err) {
		t.Errorf("beta.conf still exists (err=%v)", err)
	}
	for _, kept := range []string{"alpha.conf", "alpha/disk.qcow2", "gamma.conf", "gamma/disk.qcow2"} {
		if _, err := os.Stat(filepath.Join(root, kept)); err != nil {
			t.Errorf("%s was lost: %v", kept, err)
		}
	}
	if len(m.vms) != 2 || m.vms[m.cursor].Name() != "gamma" {
		t.Errorf("list = %d VMs, selected %q; want 2 VMs with gamma selected", len(m.vms), m.vms[m.cursor].Name())
	}
	if !strings.Contains(m.flash, "Delete beta") || m.flashErr {
		t.Errorf("flash = %q", m.flash)
	}
}

func TestDeleteVMRefusedWhileRunning(t *testing.T) {
	m, root := newTwoVMModel(t)
	m.launching[m.vms[0].ConfPath] = true // counts as up
	m.openMenu()
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if m.mode == modeConfirm || !m.flashErr {
		t.Fatalf("running VM offered for deletion: mode=%v flash=%q", m.mode, m.flash)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha.conf")); err != nil {
		t.Fatal(err)
	}
}
