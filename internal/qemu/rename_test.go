package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A .conf as quickget writes it, plus the files quickemu leaves in the folder.
func TestRenameMovesConfFolderAndPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "shared.iso")
	conf := filepath.Join(root, "windows-11.conf")
	write(t, conf, `#!/usr/bin/quickemu --vm
guest_os="windows"
disk_img="windows-11/disk.qcow2"
iso="windows-11/windows-11.iso"   # from quickget
fixed_iso="`+filepath.Join(root, "windows-11", "virtio-win.iso")+`"
floppy="`+outside+`"
# iso="windows-11/old.iso"
gl="off"
`)
	if err := os.Chmod(conf, 0o744); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "windows-11")
	for _, f := range []string{"disk.qcow2", "windows-11.iso", "virtio-win.iso", "OVMF_VARS.fd", "windows-11.log", "windows-11.tui-launch.log", "windows-11.pid", "windows-11.sh", "windows-11.ports"} {
		write(t, filepath.Join(dir, f), f)
	}

	vm := VM{ConfPath: conf}
	plan, err := PlanRename(vm, "work-pc")
	if err != nil {
		t.Fatal(err)
	}
	if plan.NewDir != filepath.Join(root, "work-pc") || plan.KeepReason != "" {
		t.Fatalf("folder not planned to move: %+v", plan)
	}
	if got := strings.Join(plan.PathChanges, ","); got != "disk_img,iso,fixed_iso" {
		t.Errorf("path changes = %s", got)
	}
	if !plan.CoresWarning {
		t.Error("no warning about losing quickemu's Windows 11 core bump")
	}
	if err := RenameVM(vm, plan); err != nil {
		t.Fatal(err)
	}

	newConf := filepath.Join(root, "work-pc.conf")
	if exists(conf) || exists(dir) {
		t.Fatal("old .conf or folder left behind")
	}
	text := read(t, newConf)
	for _, want := range []string{
		`disk_img="work-pc/disk.qcow2"`,
		`iso="work-pc/windows-11.iso"   # from quickget`, // the ISO's own name isn't the VM's
		`fixed_iso="` + filepath.Join(root, "work-pc", "virtio-win.iso") + `"`,
		`floppy="` + outside + `"`, // outside the folder: untouched
		`# iso="windows-11/old.iso"`,
		"#!/usr/bin/quickemu --vm\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf(".conf lacks %q:\n%s", want, text)
		}
	}
	if st, _ := os.Stat(newConf); st.Mode().Perm() != 0o744 {
		t.Errorf(".conf mode = %v, want 0744", st.Mode().Perm())
	}
	nd := filepath.Join(root, "work-pc")
	for _, f := range []string{"disk.qcow2", "windows-11.iso", "virtio-win.iso", "OVMF_VARS.fd", "work-pc.log", "work-pc.tui-launch.log"} {
		if !exists(filepath.Join(nd, f)) {
			t.Errorf("%s missing after rename", f)
		}
	}
	for _, f := range []string{"windows-11.pid", "windows-11.sh", "windows-11.ports", "windows-11.log"} {
		if exists(filepath.Join(nd, f)) {
			t.Errorf("%s left behind", f)
		}
	}
	if read(t, filepath.Join(nd, "work-pc.log")) != "windows-11.log" {
		t.Error("log contents lost")
	}
	p, err := VM{ConfPath: newConf}.Paths()
	if err != nil || p.Disk != filepath.Join(nd, "disk.qcow2") {
		t.Errorf("renamed VM's disk = %q (%v)", p.Disk, err)
	}
}

func TestRenameRefusesBadNamesAndClashes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.conf"), "disk_img=\"a/disk.qcow2\"\n")
	write(t, filepath.Join(root, "b.conf"), "")
	write(t, filepath.Join(root, "c", "x"), "")
	vm := VM{ConfPath: filepath.Join(root, "a.conf")}
	for name, want := range map[string]string{
		"a":         "already called",
		"b":         "already exists",
		"c":         "already exists",
		"has space": "letters, digits",
		"../up":     "letters, digits",
		"-dash":     "letters, digits",
		"":          "letters, digits",
	} {
		if _, err := PlanRename(vm, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", name, err, want)
		}
	}
}

func TestRenameLeavesForeignFoldersAlone(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.conf"), "disk_img=\"shared/disk.qcow2\"\n")
	write(t, filepath.Join(root, "shared", "disk.qcow2"), "")
	vm := VM{ConfPath: filepath.Join(root, "a.conf")}
	plan, err := PlanRename(vm, "b")
	if err != nil {
		t.Fatal(err)
	}
	if plan.NewDir != "" || !strings.Contains(plan.KeepReason, "isn't named after the VM") {
		t.Fatalf("plan = %+v", plan)
	}
	if err := RenameVM(vm, plan); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(root, "shared", "disk.qcow2")) || read(t, filepath.Join(root, "b.conf")) != "disk_img=\"shared/disk.qcow2\"\n" {
		t.Fatal("foreign folder moved or .conf changed")
	}
}

func TestRenameWithoutFolderYetRepointsConf(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.conf"), "disk_img=\"a/disk.qcow2\"\n")
	vm := VM{ConfPath: filepath.Join(root, "a.conf")}
	plan, err := PlanRename(vm, "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameVM(vm, plan); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, "b.conf")); got != "disk_img=\"b/disk.qcow2\"\n" {
		t.Fatalf(".conf = %q", got)
	}
}

func TestRenameUndoesFolderMoveWhenConfCantBeWritten(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.conf"), "disk_img=\"a/disk.qcow2\"\n")
	write(t, filepath.Join(root, "a", "disk.qcow2"), "")
	vm := VM{ConfPath: filepath.Join(root, "a.conf")}
	plan, err := PlanRename(vm, "b")
	if err != nil {
		t.Fatal(err)
	}
	plan.NewConf = filepath.Join(root, "missing-dir", "b.conf") // can't be created
	if err := RenameVM(vm, plan); err == nil {
		t.Fatal("expected an error")
	}
	if !exists(filepath.Join(root, "a", "disk.qcow2")) || exists(filepath.Join(root, "b")) || !exists(filepath.Join(root, "a.conf")) {
		t.Fatal("failed rename wasn't undone")
	}
}

func TestRenameKeepsSnapshotsWithRealQemuImg(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not installed")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "a.conf"), "guest_os=\"linux\"\ndisk_img=\"a/disk.qcow2\"\n")
	vm := VM{ConfPath: filepath.Join(root, "a.conf")}
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", filepath.Join(root, "a", "disk.qcow2"), "64M").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if err := CreateSnapshot(vm, "pristine"); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRename(vm, "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameVM(vm, plan); err != nil {
		t.Fatal(err)
	}
	info, err := GetDiskInfo(filepath.Join(root, "b", "disk.qcow2"))
	if err != nil || len(info.Snapshots) != 1 || info.Snapshots[0].Name != "pristine" {
		t.Fatalf("snapshots after rename = %+v (%v)", info.Snapshots, err)
	}
}
