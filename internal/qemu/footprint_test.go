package qemu

import (
	"os"
	"path/filepath"
	"testing"
)

// writeIn is writeFile, making path's directory first.
func writeIn(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, text)
}

func TestFootprintCountsTheConfAndEverythingInTheFolder(t *testing.T) {
	root := t.TempDir()
	conf := filepath.Join(root, "alpine.conf")
	writeIn(t, conf, "guest_os=\"linux\"\n")
	writeIn(t, filepath.Join(root, "alpine", "disk.qcow2"), "disk")
	writeIn(t, filepath.Join(root, "alpine", "alpine.iso"), "iso")
	writeIn(t, filepath.Join(root, "alpine", "nested", "OVMF_VARS.fd"), "vars")
	writeIn(t, filepath.Join(root, "other", "disk.qcow2"), "not ours")

	fp, err := MeasureFootprint(VM{ConfPath: conf})
	if err != nil {
		t.Fatal(err)
	}
	if fp.Files != 4 || fp.Dir != filepath.Join(root, "alpine") || fp.Shared {
		t.Errorf("got %+v, want 4 files in alpine/", fp)
	}
	if fp.Bytes <= 0 {
		t.Errorf("%d bytes", fp.Bytes)
	}
}

func TestFootprintCountsAllocatedSpaceNotLength(t *testing.T) {
	root := t.TempDir()
	conf := filepath.Join(root, "vm.conf")
	writeIn(t, conf, "a=1\n")
	disk := filepath.Join(root, "vm", "disk.qcow2")
	writeIn(t, disk, "")
	if err := os.Truncate(disk, 1<<30); err != nil { // sparse: 1 GiB long, nothing written
		t.Fatal(err)
	}
	fp, err := MeasureFootprint(VM{ConfPath: conf})
	if err != nil {
		t.Fatal(err)
	}
	if fp.Bytes >= 1<<30 {
		t.Errorf("a sparse 1 GiB file counts as %d bytes", fp.Bytes)
	}
}

func TestFootprintWithoutAFolderOfItsOwn(t *testing.T) {
	root := t.TempDir()
	conf := filepath.Join(root, "vm.conf")
	writeIn(t, conf, "disk_img=\"disk.qcow2\"\n") // next to the conf, among other VMs
	writeIn(t, filepath.Join(root, "disk.qcow2"), "disk")
	writeIn(t, filepath.Join(root, "vm.log"), "log")
	writeIn(t, filepath.Join(root, "other.conf"), "a=1\n")
	writeIn(t, filepath.Join(root, "other", "disk.qcow2"), "not ours")

	fp, err := MeasureFootprint(VM{ConfPath: conf})
	if err != nil {
		t.Fatal(err)
	}
	if fp.Files != 3 || fp.Dir != "" || !fp.Shared { // conf, disk, log
		t.Errorf("got %+v, want the conf, disk and log only", fp)
	}
}

func TestFootprintBeforeTheFolderExists(t *testing.T) {
	root := t.TempDir()
	conf := filepath.Join(root, "vm.conf")
	writeIn(t, conf, "a=1\n")
	fp, err := MeasureFootprint(VM{ConfPath: conf})
	if err != nil {
		t.Fatal(err)
	}
	if fp.Files != 1 || fp.Shared {
		t.Errorf("got %+v, want just the conf", fp)
	}
}
