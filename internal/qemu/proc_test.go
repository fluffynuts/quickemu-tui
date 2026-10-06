//go:build linux || darwin

package qemu

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestHelperSleep is a stand-in qemu: run as a child of the test binary (named
// qemu.test, so it looks like qemu), it just waits to be killed.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("QEMU_TUI_HELPER_SLEEP") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// startChild runs cmd until the test ends, returning its pid.
func startChild(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd.Process.Pid
}

func TestProcessInfoFindsQemuAndWhenItStarted(t *testing.T) {
	before := time.Now()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "QEMU_TUI_HELPER_SLEEP=1")
	pid := startChild(t, cmd)
	// Start returns while the child can still be part way through exec, with
	// no command line yet; qemu writes its pid file long after that
	var started time.Time
	alive := false
	for deadline := time.Now().Add(2 * time.Second); !alive && time.Now().Before(deadline); {
		started, alive = processInfo(pid)
		time.Sleep(10 * time.Millisecond)
	}
	if !alive {
		t.Fatal("the stand-in qemu isn't alive")
	}
	// /proc's btime is whole seconds, so allow a little either side
	if started.Before(before.Add(-2*time.Second)) || started.After(time.Now().Add(2*time.Second)) {
		t.Errorf("started %v, want around %v", started, before)
	}
}

func TestProcessInfoIgnoresNonQemu(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	if _, alive := processInfo(startChild(t, exec.Command(sleep, "30"))); alive {
		t.Error("sleep counts as qemu")
	}
}

func TestProcessInfoUnknownPid(t *testing.T) {
	if _, alive := processInfo(1 << 30); alive {
		t.Error("a pid that can't exist is alive")
	}
}
