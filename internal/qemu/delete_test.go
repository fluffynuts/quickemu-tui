package qemu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestDeleteRemovesConfFolderAndShortcutOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	write(t, filepath.Join(root, "alpine.conf"), "disk_img=\"alpine/disk.qcow2\"\n")
	write(t, filepath.Join(root, "alpine", "disk.qcow2"), "0123456789")
	write(t, filepath.Join(root, "alpine", "nested", "OVMF_VARS.fd"), "xx")
	write(t, filepath.Join(root, "other.conf"), "disk_img=\"other/disk.qcow2\"\n")
	write(t, filepath.Join(root, "other", "disk.qcow2"), "keep")
	write(t, filepath.Join(root, "notes.txt"), "keep")
	shortcut := filepath.Join(home, ".local", "share", "applications", "alpine.desktop")
	write(t, shortcut, "[Desktop Entry]")

	vm := VM{ConfPath: filepath.Join(root, "alpine.conf")}
	plan, err := PlanDelete(vm)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Dir != filepath.Join(root, "alpine") || plan.Files != 2 || plan.Bytes != 12 || plan.Shortcut != shortcut {
		t.Fatalf("plan = %+v", plan)
	}
	if err := DeleteVM(vm, plan); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"alpine.conf", "alpine", shortcut} {
		p := gone
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, gone)
		}
		if exists(p) {
			t.Errorf("%s still exists", gone)
		}
	}
	for _, kept := range []string{"other.conf", "other/disk.qcow2", "notes.txt"} {
		if !exists(filepath.Join(root, kept)) {
			t.Errorf("%s was deleted", kept)
		}
	}
}

func TestDeleteNeverRemovesTheVMDirectoryItself(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	// disk sits next to the conf, so "the VM's folder" would be the whole root
	write(t, filepath.Join(root, "vm.conf"), "disk_img=\"disk.qcow2\"\n")
	write(t, filepath.Join(root, "disk.qcow2"), "x")
	write(t, filepath.Join(root, "precious", "data"), "keep")
	vm := VM{ConfPath: filepath.Join(root, "vm.conf")}

	plan, err := PlanDelete(vm)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Dir != "" || !strings.Contains(plan.KeepReason, "stored directly in") {
		t.Fatalf("plan = %+v, want the folder kept with a reason", plan)
	}
	if err := DeleteVM(vm, plan); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "vm.conf")) {
		t.Error("conf should still be removed")
	}
	for _, kept := range []string{"disk.qcow2", "precious/data"} {
		if !exists(filepath.Join(root, kept)) {
			t.Errorf("%s was deleted", kept)
		}
	}
}

func TestDeleteLeavesFoldersOutsideSharedOrSymlinked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "disk.qcow2"), "x")
	write(t, filepath.Join(root, "ext.conf"), "disk_img=\""+outside+"/disk.qcow2\"\n")

	write(t, filepath.Join(root, "a.conf"), "disk_img=\"shared/disk.qcow2\"\n")
	write(t, filepath.Join(root, "b.conf"), "disk_img=\"shared/disk.qcow2\"\n")
	write(t, filepath.Join(root, "shared", "disk.qcow2"), "x")

	target := t.TempDir()
	write(t, filepath.Join(target, "disk.qcow2"), "x")
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "l.conf"), "disk_img=\"linked/disk.qcow2\"\n")

	for name, wantReason := range map[string]string{
		"ext.conf": "isn't inside", "a.conf": "also used by b", "l.conf": "symbolic link",
	} {
		plan, err := PlanDelete(VM{ConfPath: filepath.Join(root, name)})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Dir != "" || !strings.Contains(plan.KeepReason, wantReason) {
			t.Errorf("%s: plan = %+v, want reason containing %q", name, plan, wantReason)
		}
	}
}

func TestDeleteWithMissingFolderJustRemovesConf(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	write(t, filepath.Join(root, "x.conf"), "guest_os=\"linux\"\n") // never started: no folder yet
	vm := VM{ConfPath: filepath.Join(root, "x.conf")}
	plan, err := PlanDelete(vm)
	if err != nil || plan.Dir != "" || plan.KeepReason != "" {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
	if err := DeleteVM(vm, plan); err != nil || exists(vm.ConfPath) {
		t.Fatalf("err = %v, conf exists = %v", err, exists(vm.ConfPath))
	}
}
