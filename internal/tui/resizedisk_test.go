package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
	"github.com/fluffynuts/quickemu-tui/internal/testtool"
)

const gibBytes = int64(1) << 30

func resizeModel(t *testing.T, virtual int64, snaps ...string) Model {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vm.conf"), []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: root})
	m.width, m.height = 100, 30
	info := qemu.DiskInfo{VirtualSize: virtual}
	for _, s := range snaps {
		info.Snapshots = append(info.Snapshots, qemu.Snapshot{ID: s, Name: s})
	}
	m.disks[m.vms[0].ConfPath] = diskState{loaded: true, info: info}
	return m
}

func send(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m, cmd
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func screen(m Model) string { return ansi.Strip(m.View()) }

func TestMenuOffersResizeOnlyWhileTheVMIsOff(t *testing.T) {
	m := resizeModel(t, 40*gibBytes)
	find := func() menuItem {
		for _, it := range m.menuItems() {
			if it.key == "z" {
				return it
			}
		}
		t.Fatal("no resize item in the menu")
		return menuItem{}
	}
	if it := find(); it.label != "Resize disk…" || it.disabled {
		t.Fatalf("stopped VM: %+v", it)
	}
	m.launching[m.vms[0].ConfPath] = true
	if !find().disabled {
		t.Fatal("resize is offered for a VM that is starting")
	}
}

func TestResizePickerOffersPresetsWithTheirResultingSizeAndAnOtherRow(t *testing.T) {
	m := resizeModel(t, 40*gibBytes)
	m.openMenu()
	m, _ = send(m, runes("z"))
	if m.mode != modeResize {
		t.Fatalf("mode = %v, want the resize picker", m.mode)
	}
	s := screen(m)
	for _, want := range []string{"40.0 GiB", "+10G", "50.0 GiB", "+20G", "60.0 GiB", "+50G", "90.0 GiB", "+100G", "140.0 GiB", "Other size"} {
		if !strings.Contains(s, want) {
			t.Errorf("picker is missing %q:\n%s", want, s)
		}
	}
}

func TestResizeRefusesARunningVM(t *testing.T) {
	m := resizeModel(t, 40*gibBytes)
	m.launching[m.vms[0].ConfPath] = true
	m.openMenu()
	m, _ = send(m, runes("z"))
	if m.mode == modeResize || !m.flashErr || !strings.Contains(m.flash, "down first") {
		t.Fatalf("mode=%v flash=%q", m.mode, m.flash)
	}
}

func TestResizeNeedsADisk(t *testing.T) {
	m := resizeModel(t, 40*gibBytes)
	m.disks[m.vms[0].ConfPath] = diskState{loaded: true, missing: true}
	m.openMenu()
	m, _ = send(m, runes("z"))
	if m.mode == modeResize || !m.flashErr || !strings.Contains(m.flash, "No disk yet") {
		t.Fatalf("mode=%v flash=%q", m.mode, m.flash)
	}
}

func TestResizeEscClosesThePicker(t *testing.T) {
	m := resizeModel(t, 40*gibBytes)
	m.openMenu()
	m, _ = send(m, runes("z"), tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Fatalf("mode = %v", m.mode)
	}
}

func TestGrowConfirmationExplainsTheGuestKeepsItsPartitionsAndIsYesByDefault(t *testing.T) {
	text := resizeConfirmText("alpha", qemu.ResizePlan{Current: 40 * gibBytes, Target: 80 * gibBytes})
	for _, want := range []string{"alpha", "40.0 GiB", "80.0 GiB", "partitions"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(strings.ToUpper(text), "DESTROY") || strings.Contains(text, "snapshot") {
		t.Errorf("a plain grow shouldn't carry the shrink or snapshot warnings: %q", text)
	}
}

func TestSnapshotsGetAWarningBecauseRevertingRestoresTheirSize(t *testing.T) {
	text := resizeConfirmText("alpha", qemu.ResizePlan{Current: 40 * gibBytes, Target: 80 * gibBytes, Snapshots: 2})
	if !strings.Contains(text, "2 snapshots") || !strings.Contains(text, "size it was taken at") {
		t.Errorf("no snapshot warning: %q", text)
	}
	one := resizeConfirmText("alpha", qemu.ResizePlan{Current: 40 * gibBytes, Target: 80 * gibBytes, Snapshots: 1})
	if !strings.Contains(one, "1 snapshot") || strings.Contains(one, "1 snapshots") {
		t.Errorf("bad plural: %q", one)
	}
}

func TestShrinkConfirmationIsAStrongWarning(t *testing.T) {
	text := ansi.Strip(resizeConfirmText("alpha", qemu.ResizePlan{Current: 80 * gibBytes, Target: 40 * gibBytes}))
	for _, want := range []string{"SHRINK", "80.0 GiB", "40.0 GiB", "permanently", "partitions", "backup"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestShrinkDefaultsToNoAndGrowToYes(t *testing.T) {
	if resizeConfirmDefault(qemu.ResizePlan{Current: 80 * gibBytes, Target: 40 * gibBytes}) != defaultNo {
		t.Error("a shrink must not be confirmed by a stray enter")
	}
	if resizeConfirmDefault(qemu.ResizePlan{Current: 40 * gibBytes, Target: 80 * gibBytes}) != defaultYes {
		t.Error("a grow should default to yes")
	}
}

func TestResizeFlowWithRealQemuImg(t *testing.T) {
	testtool.Require(t, "qemu-img")
	m := resizeModel(t, 0)
	vm := m.vms[0]
	p, _ := vm.Paths()
	if err := os.MkdirAll(p.VMDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", p.Disk, "64M").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	info, err := qemu.GetDiskInfo(p.Disk)
	if err != nil {
		t.Fatal(err)
	}
	m.disks[vm.ConfPath] = diskState{loaded: true, info: info}

	// grow by typing a size: Other size… → prompt → confirm → run
	m.openMenu()
	m, _ = send(m, runes("z"))
	for range 4 { // past the four presets
		m, _ = send(m, runes("j"))
	}
	m, _ = send(m, enter)
	if m.mode != modePrompt {
		t.Fatalf("mode = %v, want the size prompt", m.mode)
	}
	m.input.SetValue("+64M")
	m, _ = send(m, enter)
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want the confirmation; flash=%q", m.mode, m.flash)
	}
	m, cmd := send(m, runes("y"))
	done := runOps(t, cmd)
	if done.err != nil {
		t.Fatalf("resize failed: %v", done.err)
	}
	if !done.refreshDisk {
		t.Error("the disk pane isn't refreshed after a resize")
	}
	after, err := qemu.GetDiskInfo(p.Disk)
	if err != nil || after.VirtualSize != 128*1024*1024 {
		t.Fatalf("disk = %+v, %v; want 128 MiB", after, err)
	}
}

func TestResizeFlowRejectsABadSizeWithoutLeavingThePicker(t *testing.T) {
	m := resizeModel(t, 40*gibBytes)
	m.openMenu()
	m, _ = send(m, runes("z"))
	for range 4 {
		m, _ = send(m, runes("j"))
	}
	m, _ = send(m, enter)
	m.input.SetValue("40")
	m, _ = send(m, enter)
	if !m.flashErr || !strings.Contains(m.flash, "unit") {
		t.Fatalf("flash = %q (err=%v), want a hint about the unit", m.flash, m.flashErr)
	}
	if m.mode == modeConfirm {
		t.Fatal("went on to confirm a size that doesn't parse")
	}
}

// runOps runs cmd and what it batches, returning the operation's result.
func runOps(t *testing.T, cmd tea.Cmd) opDoneMsg {
	t.Helper()
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
	return done
}
