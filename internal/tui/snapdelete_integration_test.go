package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// Drives the real menu -> checklist -> confirm flow against a real qcow2 disk.
func TestDeleteSnapshotsFlowWithRealQemuImg(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not installed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 30
	vm := m.vms[0]
	p, _ := vm.Paths()
	if err := os.MkdirAll(p.VMDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", p.Disk, "64M").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, tag := range []string{"one", "two", "three"} {
		if err := qemu.CreateSnapshot(vm, tag); err != nil {
			t.Fatal(err)
		}
	}
	info, err := qemu.GetDiskInfo(p.Disk)
	if err != nil {
		t.Fatal(err)
	}
	m.disks[vm.ConfPath] = diskState{loaded: true, info: info}

	send := func(k tea.KeyMsg) tea.Cmd { n, c := m.Update(k); m = n.(Model); return c }
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	m.openMenu()
	send(runes("d"))
	send(tea.KeyMsg{Type: tea.KeySpace}) // tick "one"
	send(runes("j"))
	send(runes("j"))
	send(tea.KeyMsg{Type: tea.KeySpace}) // tick "three"
	send(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := send(runes("y"))

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
		t.Fatalf("delete failed: %v", done.err)
	}

	after, err := qemu.GetDiskInfo(p.Disk)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Snapshots) != 1 || after.Snapshots[0].Name != "two" {
		t.Fatalf("remaining snapshots = %+v, want only 'two'", after.Snapshots)
	}
}
