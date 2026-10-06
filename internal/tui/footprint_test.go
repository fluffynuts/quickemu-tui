package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDetailsShowTheVMsSizeOnDiskBelowTheDisplay(t *testing.T) {
	m := newMouseModel(t, "alpha")
	vm := m.vms[0]
	m.infos[vm.ConfPath] = vmInfo{conf: map[string]string{"guest_os": "linux"}}
	must(t, os.MkdirAll(filepath.Join(vm.BaseDir(), "alpha"), 0o755))
	must(t, os.WriteFile(filepath.Join(vm.BaseDir(), "alpha", "alpha.iso"), []byte("iso"), 0o644))

	next, _ := m.Update(loadDisk(vm)())
	m = next.(Model)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	display := -1
	for i, l := range lines {
		if strings.Contains(l, "display") {
			display = i
		}
	}
	if display < 0 || display+1 >= len(lines) {
		t.Fatalf("no display line:\n%s", strings.Join(lines, "\n"))
	}
	if got := lines[display+1]; !strings.Contains(got, "on disk") || !strings.Contains(got, "in 2 files") {
		t.Errorf("line below display is %q, want the size of the .conf and the ISO", got)
	}
}
