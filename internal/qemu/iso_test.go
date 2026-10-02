package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeISO(t *testing.T, path string, sig string) {
	t.Helper()
	data := make([]byte, 20*isoSectorSize)
	copy(data[16*isoSectorSize:], append([]byte{1}, []byte(sig)...))
	write(t, path, string(data))
}

func TestISOProblemFor(t *testing.T) {
	dir := t.TempDir()
	fakeISO(t, filepath.Join(dir, "iso9660.iso"), "CD001")
	fakeISO(t, filepath.Join(dir, "udf.iso"), "BEA01")
	html := "<!DOCTYPE html>\n<html><head><title>Download moved</title></head><body>" + strings.Repeat("x", 20000) + "</body></html>"
	write(t, filepath.Join(dir, "page.iso"), html)
	write(t, filepath.Join(dir, "tiny.iso"), "hello")
	write(t, filepath.Join(dir, "junk.iso"), strings.Repeat("\x00junk", 20000))
	must := func(p, want string) {
		t.Helper()
		got := ISOProblemFor(filepath.Join(dir, p))
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("%s: got %q, want it to contain %q", p, got, want)
		}
	}
	must("iso9660.iso", "")
	must("udf.iso", "")
	must("page.iso", "HTML web page")
	must("tiny.iso", "too small")
	must("junk.iso", "no ISO 9660 or UDF signature")
	must("absent.iso", "does not exist")
	if got := ISOProblemFor(dir); !strings.Contains(got, "directory") {
		t.Errorf("directory: %q", got)
	}
}

func TestGenuineISOFromGenisoimageIsAccepted(t *testing.T) {
	tool, err := exec.LookPath("genisoimage")
	if err != nil {
		t.Skip("genisoimage not installed")
	}
	src := t.TempDir()
	write(t, filepath.Join(src, "hello.txt"), "hi")
	iso := filepath.Join(t.TempDir(), "real.iso")
	if out, err := exec.Command(tool, "-quiet", "-o", iso, src).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := ISOProblemFor(iso); got != "" {
		t.Errorf("real ISO rejected: %s", got)
	}
}

func TestVerifyISOsFindsReferencedAndStrayFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "win.conf"),
		"disk_img=\"win/disk.qcow2\"\niso=\"win/Win11.iso\"\nfixed_iso=\"win/virtio-win.iso\"\n")
	write(t, filepath.Join(root, "win", "Win11.iso"), "<html>moved</html>")
	fakeISO(t, filepath.Join(root, "win", "virtio-win.iso"), "CD001")
	write(t, filepath.Join(root, "win", "stray.ISO"), "not an iso at all")
	write(t, filepath.Join(root, "unrelated.iso"), "<html>") // outside the VM folder: not this VM's

	problems := VerifyISOs(VM{ConfPath: filepath.Join(root, "win.conf")})
	got := map[string]string{}
	for _, p := range problems {
		got[p.Path] = p.Reason
	}
	if len(got) != 2 {
		t.Fatalf("problems = %+v, want Win11.iso and stray.ISO only", problems)
	}
	if !strings.Contains(got[filepath.Join(root, "win", "Win11.iso")], "HTML") {
		t.Errorf("Win11.iso: %v", got)
	}
	if _, ok := got[filepath.Join(root, "win", "stray.ISO")]; !ok {
		t.Errorf("stray .ISO (upper case extension) not checked: %v", got)
	}
	if !filepath.IsAbs(problems[0].Path) {
		t.Errorf("path not absolute: %q", problems[0].Path)
	}
}

func TestVerifyISOsReportsMissingAndDirectoryReferences(t *testing.T) {
	root := t.TempDir()
	// what quickget leaves behind when the download fails: iso="<folder>/"
	write(t, filepath.Join(root, "dos.conf"), "disk_img=\"dos/disk.qcow2\"\niso=\"dos/\"\n")
	if err := os.MkdirAll(filepath.Join(root, "dos"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "gone.conf"), "disk_img=\"gone/disk.qcow2\"\niso=\"gone/missing.iso\"\n")

	if p := VerifyISOs(VM{ConfPath: filepath.Join(root, "dos.conf")}); len(p) != 1 || !strings.Contains(p[0].Reason, "directory") {
		t.Errorf("dos: %+v", p)
	}
	if p := VerifyISOs(VM{ConfPath: filepath.Join(root, "gone.conf")}); len(p) != 1 || !strings.Contains(p[0].Reason, "does not exist") {
		t.Errorf("gone: %+v", p)
	}
}

func TestVerifyISOsNeverWalksTheWholeVMDirectory(t *testing.T) {
	root := t.TempDir()
	// disk next to the conf: the "VM folder" would be root, which holds other VMs' files
	write(t, filepath.Join(root, "a.conf"), "disk_img=\"disk.qcow2\"\n")
	write(t, filepath.Join(root, "other", "bad.iso"), "<html>")
	if p := VerifyISOs(VM{ConfPath: filepath.Join(root, "a.conf")}); len(p) != 0 {
		t.Errorf("scanned outside the VM's own folder: %+v", p)
	}
}
