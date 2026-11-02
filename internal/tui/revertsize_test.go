package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
	"github.com/fluffynuts/quickemu-tui/internal/testtool"
)

// revertFixture is a VM whose disk was 64M when "pristine" was taken and is
// 128M now, in a model that has read that.
func revertFixture(t *testing.T, opts Options, resize bool) Model {
	t.Helper()
	testtool.Require(t, "qemu-img")
	opts.Root = t.TempDir()
	if err := os.WriteFile(filepath.Join(opts.Root, "vm.conf"), []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(opts)
	m.width, m.height = 100, 30
	vm := m.vms[0]
	p, _ := vm.Paths()
	if err := os.MkdirAll(p.VMDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", p.Disk, "64M").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := qemu.CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	if resize {
		plan, err := qemu.PlanResize(vm, "128M")
		if err != nil {
			t.Fatal(err)
		}
		if err := qemu.ResizeDisk(vm, plan); err != nil {
			t.Fatal(err)
		}
	}
	info, err := qemu.GetDiskInfo(p.Disk)
	if err != nil {
		t.Fatal(err)
	}
	m.disks[vm.ConfPath] = diskState{loaded: true, info: info}
	return m
}

// revert drives menu -> picker -> confirm, with key choosing the snapshot
// (enter reverts, s reverts and starts), runs the operation and delivers its
// result to the model.
func revert(t *testing.T, m Model, key tea.KeyMsg) (Model, opDoneMsg) {
	t.Helper()
	m.openMenu()
	m, cmd := send(m, runes("a"), key, runes("y"))
	done := runOps(t, cmd)
	if done.err != nil {
		t.Fatalf("revert failed: %v", done.err)
	}
	m, _ = send(m, done)
	return m, done
}

func TestRevertingToASnapshotOfAnotherSizeTellsTheUser(t *testing.T) {
	m, done := revert(t, revertFixture(t, Options{}, true), enter)
	if !strings.Contains(done.note, "64.0 MiB") || !strings.Contains(done.note, "128.0 MiB") {
		t.Fatalf("note = %q, want both sizes", done.note)
	}
	if m.flashErr || !strings.Contains(m.flash, done.note) || !strings.Contains(m.flash, "✓") {
		t.Fatalf("flash = %q (err=%v), want the success message with the note", m.flash, m.flashErr)
	}
}

func TestRevertingToASnapshotOfTheSameSizeHasNoNote(t *testing.T) {
	m, done := revert(t, revertFixture(t, Options{}, false), enter)
	if done.note != "" || strings.Contains(m.flash, "disk") {
		t.Fatalf("note = %q, flash = %q; nothing changed size", done.note, m.flash)
	}
}

func TestTheSizeNoteSurvivesStartingTheVMAfterTheRevert(t *testing.T) {
	fake := writeScript(t, t.TempDir(), "quickemu", "exit 0\n")
	m, done := revert(t, revertFixture(t, Options{Quickemu: fake}, true), runes("s"))
	if done.note == "" || !strings.Contains(m.flash, "64.0 MiB") {
		t.Fatalf("flash = %q, want the size note kept beside the start", m.flash)
	}
}

func TestAFailedStartAfterRevertStillShowsItsError(t *testing.T) {
	m, _ := revert(t, revertFixture(t, Options{Quickemu: filepath.Join(t.TempDir(), "no-quickemu")}, true), runes("s"))
	if !m.flashErr || !strings.Contains(m.flash, "quickemu") {
		t.Fatalf("flash = %q (err=%v), want the start failure, not overwritten by the note", m.flash, m.flashErr)
	}
}
