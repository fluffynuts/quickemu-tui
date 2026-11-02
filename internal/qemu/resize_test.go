package qemu

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fluffynuts/quickemu-tui/internal/testtool"
)

const (
	mib = int64(1) << 20
	gib = int64(1) << 30
)

func TestParseDiskSize(t *testing.T) {
	cases := []struct {
		in   string
		want DiskSize
	}{
		{"40G", DiskSize{Bytes: 40 * gib}},
		{"40g", DiskSize{Bytes: 40 * gib}},
		{" 40 GB ", DiskSize{Bytes: 40 * gib}},
		{"40GiB", DiskSize{Bytes: 40 * gib}},
		{"512M", DiskSize{Bytes: 512 * mib}},
		{"1.5T", DiskSize{Bytes: 3 * gib * 512}},
		{"2.5G", DiskSize{Bytes: 5 * gib / 2}},
		{"+20G", DiskSize{Bytes: 20 * gib, Relative: true}},
		{"-5G", DiskSize{Bytes: -5 * gib, Relative: true}},
		{"64k", DiskSize{Bytes: 64 << 10}},
	}
	for _, c := range cases {
		got, err := ParseDiskSize(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDiskSize(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

func TestParseDiskSizeRejects(t *testing.T) {
	for in, wantInMsg := range map[string]string{
		"":        "enter a size",
		"40":      "unit",
		"G":       "number",
		"abc":     "number",
		"40X":     "unit",
		"4 0G":    "unit",
		"0G":      "more than 0",
		"-0G":     "more than 0",
		"1e3G":    "unit",
		"99999T":  "too large",
		"40G40M":  "unit",
		"--5G":    "number",
		"0.0001K": "more than 0",
	} {
		_, err := ParseDiskSize(in)
		if err == nil || !strings.Contains(err.Error(), wantInMsg) {
			t.Errorf("ParseDiskSize(%q) = %v; want an error mentioning %q", in, err, wantInMsg)
		}
	}
}

func TestResolveTarget(t *testing.T) {
	cases := []struct {
		current int64
		spec    DiskSize
		want    int64
		ok      bool
	}{
		{40 * gib, DiskSize{Bytes: 80 * gib}, 80 * gib, true},
		{40 * gib, DiskSize{Bytes: 20 * gib, Relative: true}, 60 * gib, true},
		{40 * gib, DiskSize{Bytes: -10 * gib, Relative: true}, 30 * gib, true},
		{40 * gib, DiskSize{Bytes: -40 * gib, Relative: true}, 0, false},
		{40 * gib, DiskSize{Bytes: -50 * gib, Relative: true}, 0, false},
		{40 * gib, DiskSize{Bytes: 1000}, 1024, true}, // rounded up to qemu's 512-byte sector multiple
	}
	for _, c := range cases {
		got, err := resolveTarget(c.current, c.spec)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("resolveTarget(%d, %+v) = %d, %v; want %d ok=%v", c.current, c.spec, got, err, c.want, c.ok)
		}
	}
}

func TestFormatConfSize(t *testing.T) {
	for in, want := range map[int64]string{
		40 * gib:   "40G",
		1024 * gib: "1T",
		1536 * mib: "1536M",
		100 * mib:  "100M",
		64 << 10:   "64K",
		gib + 512:  "1073742336", // no unit divides it: plain bytes, which qemu-img accepts
	} {
		if got := FormatConfSize(in); got != want {
			t.Errorf("FormatConfSize(%d) = %q, want %q", in, got, want)
		}
	}
}

// --- against a real qemu-img ------------------------------------------------

func resizeFixture(t *testing.T, size string) (VM, Paths) {
	t.Helper()
	testtool.Require(t, "qemu-img")
	root := socketDir(t) // short enough for the monitor socket the running-VM test adds
	conf := filepath.Join(root, "vm.conf")
	if err := os.WriteFile(conf, []byte("guest_os=\"linux\"\ndisk_size=\"64M\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vm := VM{ConfPath: conf}
	p, err := vm.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.VMDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", p.Disk, size).CombinedOutput(); err != nil {
		t.Fatalf("creating disk: %v: %s", err, out)
	}
	return vm, p
}

func virtualSize(t *testing.T, disk string) int64 {
	t.Helper()
	info, err := GetDiskInfo(disk)
	if err != nil {
		t.Fatal(err)
	}
	return info.VirtualSize
}

func TestResizeGrowsTheDiskAndRecordsItInTheConf(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	plan, err := PlanResize(vm, "128M")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Current != 64*mib || plan.Target != 128*mib || plan.Shrinks() {
		t.Fatalf("plan = %+v", plan)
	}
	if err := ResizeDisk(vm, plan); err != nil {
		t.Fatal(err)
	}
	if got := virtualSize(t, p.Disk); got != 128*mib {
		t.Fatalf("disk is %d bytes, want %d", got, 128*mib)
	}
	conf, _ := vm.Conf()
	if conf["disk_size"] != "128M" {
		t.Fatalf("conf disk_size = %q, want 128M", conf["disk_size"])
	}
}

func TestResizeRelativeGrow(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	plan, err := PlanResize(vm, "+64M")
	if err != nil {
		t.Fatal(err)
	}
	if err := ResizeDisk(vm, plan); err != nil {
		t.Fatal(err)
	}
	if got := virtualSize(t, p.Disk); got != 128*mib {
		t.Fatalf("disk is %d bytes, want %d", got, 128*mib)
	}
}

func TestResizeShrinkIsFlaggedAndWorks(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	plan, err := PlanResize(vm, "32M")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Shrinks() {
		t.Fatalf("a smaller target isn't flagged as a shrink: %+v", plan)
	}
	if err := ResizeDisk(vm, plan); err != nil {
		t.Fatal(err)
	}
	if got := virtualSize(t, p.Disk); got != 32*mib {
		t.Fatalf("disk is %d bytes, want %d", got, 32*mib)
	}
	conf, _ := vm.Conf()
	if conf["disk_size"] != "32M" {
		t.Fatalf("conf disk_size = %q, want 32M", conf["disk_size"])
	}
}

func TestResizeToTheCurrentSizeIsRefused(t *testing.T) {
	vm, _ := resizeFixture(t, "64M")
	if _, err := PlanResize(vm, "64M"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("got %v, want a refusal saying it is already that size", err)
	}
}

func TestPlanResizeCountsSnapshots(t *testing.T) {
	vm, _ := resizeFixture(t, "64M")
	if err := CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanResize(vm, "128M")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Snapshots != 1 {
		t.Fatalf("Snapshots = %d, want 1", plan.Snapshots)
	}
}

// Reverting restores the size the snapshot was taken at, which is why the
// dialog warns about snapshots: pins what qemu-img does.
func TestRevertingToASnapshotRestoresItsDiskSize(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	if err := CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	plan, _ := PlanResize(vm, "128M")
	if err := ResizeDisk(vm, plan); err != nil {
		t.Fatal(err)
	}
	if err := ApplySnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	if got := virtualSize(t, p.Disk); got != 64*mib {
		t.Fatalf("after revert the disk is %d bytes; qemu-img no longer restores the snapshot's size (%d)", got, 64*mib)
	}
}

func TestResizeRefusesARunningVM(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	plan, err := PlanResize(vm, "128M")
	if err != nil {
		t.Fatal(err)
	}
	startFakeMonitor(t, p.MonitorSocket, map[string]string{"info status": "VM status: running"})
	if err := ResizeDisk(vm, plan); !errors.Is(err, ErrRunning) {
		t.Fatalf("got %v, want ErrRunning", err)
	}
	if got := virtualSize(t, p.Disk); got != 64*mib {
		t.Fatalf("the disk was resized to %d under a running VM", got)
	}
}

func TestResizeReportsAMissingDisk(t *testing.T) {
	testtool.Require(t, "qemu-img")
	conf := filepath.Join(t.TempDir(), "vm.conf")
	os.WriteFile(conf, []byte("guest_os=\"linux\"\n"), 0o644)
	if _, err := PlanResize(VM{ConfPath: conf}, "64G"); err == nil {
		t.Fatal("planned a resize of a disk that doesn't exist")
	}
}

func TestRevertToSnapshotReportsTheSizeItBroughtBack(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	if err := CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	plan, _ := PlanResize(vm, "128M")
	if err := ResizeDisk(vm, plan); err != nil {
		t.Fatal(err)
	}
	got, err := RevertToSnapshot(vm, "pristine")
	if err != nil {
		t.Fatal(err)
	}
	if got.Before != 128*mib || got.After != 64*mib || !got.SizeChanged() {
		t.Fatalf("result = %+v, want 128M -> 64M", got)
	}
	if virtualSize(t, p.Disk) != 64*mib {
		t.Fatal("the revert didn't happen")
	}
}

func TestRevertToSnapshotOfTheSameSizeReportsNoChange(t *testing.T) {
	vm, _ := resizeFixture(t, "64M")
	if err := CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	got, err := RevertToSnapshot(vm, "pristine")
	if err != nil {
		t.Fatal(err)
	}
	if got.SizeChanged() || got.Before != 64*mib {
		t.Fatalf("result = %+v, want no change at 64M", got)
	}
}

func TestRevertToSnapshotRefusesARunningVM(t *testing.T) {
	vm, p := resizeFixture(t, "64M")
	if err := CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	startFakeMonitor(t, p.MonitorSocket, map[string]string{"info status": "VM status: running"})
	if _, err := RevertToSnapshot(vm, "pristine"); !errors.Is(err, ErrRunning) {
		t.Fatalf("got %v, want ErrRunning", err)
	}
}

func TestRevertResultWithAnUnknownSizeDoesNotClaimAChange(t *testing.T) {
	if (RevertResult{Before: 0, After: 64 * mib}).SizeChanged() || (RevertResult{Before: 64 * mib}).SizeChanged() {
		t.Fatal("a size that couldn't be read was reported as a change")
	}
}
