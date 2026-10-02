package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercises the snapshot functions against a real qcow2 disk. Skipped when
// qemu-img isn't installed.
func TestSnapshotLifecycleWithRealQemuImg(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not installed")
	}
	root := t.TempDir()
	conf := filepath.Join(root, "vm.conf")
	if err := os.WriteFile(conf, []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
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
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", p.Disk, "64M").CombinedOutput(); err != nil {
		t.Fatalf("creating disk: %v: %s", err, out)
	}

	refs := func() []string {
		t.Helper()
		info, err := GetDiskInfo(p.Disk)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range info.Snapshots {
			out = append(out, s.Ref())
		}
		return out
	}

	for _, tag := range []string{"alpha", "beta", "gamma"} {
		if err := CreateSnapshot(vm, tag); err != nil {
			t.Fatalf("create %s: %v", tag, err)
		}
	}
	if got, want := refs(), []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after create: %v, want %v", got, want)
	}

	// qemu-img only accepts the tag here, so the numeric ID must be rejected
	// (guards against anyone "fixing" deletes to use IDs again).
	if err := DeleteSnapshot(vm, "1"); err == nil {
		t.Fatal("deleting by numeric ID unexpectedly succeeded")
	}

	for _, tag := range []string{"alpha", "gamma"} {
		if err := DeleteSnapshot(vm, tag); err != nil {
			t.Fatalf("delete %s: %v", tag, err)
		}
	}
	if got, want := refs(), []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after delete: %v, want %v", got, want)
	}

	if err := ApplySnapshot(vm, "beta"); err != nil {
		t.Fatalf("revert: %v", err)
	}
}
