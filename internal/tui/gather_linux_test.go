package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// fakeQemu starts a process whose command line looks like qemu's, with args,
// and writes its pid where quickemu would.
func fakeQemu(t *testing.T, vm qemu.VM, args ...string) {
	t.Helper()
	p, err := vm.Paths()
	must(t, err)
	must(t, os.MkdirAll(p.VMDir, 0o755))
	script := filepath.Join(t.TempDir(), "qemu-system-fake")
	must(t, os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755))
	cmd := exec.Command(script, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // so the sleep goes with it
	must(t, cmd.Start())
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	must(t, os.WriteFile(p.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644))
}

func gathered(t *testing.T, vm qemu.VM) vmInfo {
	t.Helper()
	var info vmInfo
	// the child can be part way through exec, with no command line yet
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		info = gatherCmd([]qemu.VM{vm})().(infoMsg)[vm.ConfPath]
		if info.status.IsUp() {
			break
		}
	}
	return info
}

func TestGatherFindsSpiceFromTheRunningQemuNotTheConf(t *testing.T) {
	for name, tc := range map[string]struct {
		conf string
		args []string
		want qemu.SpiceTarget
	}{
		"started with spice, conf since unticked": {
			"guest=\"linux\"\n",
			[]string{"-spice", "disable-ticketing=on,unix=on,addr=alpha/alpha.sock"},
			qemu.SpiceTarget{Socket: "<root>/alpha/alpha.sock"},
		},
		"started without spice, conf since ticked": {
			"display=\"spice\"\n",
			[]string{"-display", "sdl"},
			qemu.SpiceTarget{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMouseModel(t, "alpha")
			vm := m.vms[0]
			must(t, os.WriteFile(vm.ConfPath, []byte(tc.conf), 0o644))
			fakeQemu(t, vm, tc.args...)
			want := tc.want
			if want.Socket != "" {
				want.Socket = filepath.Join(vm.BaseDir(), "alpha", "alpha.sock")
			}
			info := gathered(t, vm)
			if !info.status.IsUp() {
				t.Fatalf("the stand-in qemu isn't seen as running: %+v", info.status)
			}
			if info.spice != want {
				t.Errorf("spice %+v, want %+v", info.spice, want)
			}
		})
	}
}

func TestGatherFindsNoSpiceForAStoppedVM(t *testing.T) {
	m := newMouseModel(t, "alpha")
	vm := m.vms[0]
	must(t, os.WriteFile(vm.ConfPath, []byte("display=\"spice\"\n"), 0o644))
	if info := gatherCmd([]qemu.VM{vm})().(infoMsg)[vm.ConfPath]; !info.spice.IsZero() {
		t.Errorf("spice %+v for a VM that isn't running", info.spice)
	}
}
