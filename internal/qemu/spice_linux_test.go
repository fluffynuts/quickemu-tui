package qemu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRunningSpiceReadsTheQemuProcessNotTheConf(t *testing.T) {
	root := t.TempDir()
	v := VM{ConfPath: filepath.Join(root, "alpha.conf")}
	// the .conf doesn't say spice: what the process was started with is what counts
	if err := os.WriteFile(v.ConfPath, []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := v.Paths()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$", "--", "-spice", "disable-ticketing=on,unix=on,addr=alpha/alpha.sock")
	cmd.Env = append(os.Environ(), "QEMU_TUI_HELPER_SLEEP=1")
	pid := startChild(t, cmd)
	if err := os.MkdirAll(p.VMDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.PidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got SpiceTarget
	var ok bool
	// the child can be part way through exec, with no command line yet
	for deadline := time.Now().Add(2 * time.Second); !ok && time.Now().Before(deadline); {
		got, ok = RunningSpice(v)
		time.Sleep(10 * time.Millisecond)
	}
	if want := (SpiceTarget{Socket: filepath.Join(root, "alpha", "alpha.sock")}); !ok || got != want {
		t.Errorf("got %+v, %v; want %+v", got, ok, want)
	}
}

func TestRunningSpiceIsNothingWithoutALiveQemu(t *testing.T) {
	root := t.TempDir()
	v := VM{ConfPath: filepath.Join(root, "alpha.conf")}
	if err := os.WriteFile(v.ConfPath, []byte("display=\"spice\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := RunningSpice(v); ok || !got.IsZero() {
		t.Errorf("no pid file: got %+v, %v", got, ok)
	}
}
