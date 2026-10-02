package qemu

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RunState is what we could determine about a VM's qemu process.
type RunState int

const (
	Stopped RunState = iota
	Running
	Paused
	// Busy: the qemu process is alive but the monitor isn't answering.
	Busy
)

func (s RunState) String() string {
	switch s {
	case Running:
		return "running"
	case Paused:
		return "paused"
	case Busy:
		return "running (monitor busy)"
	default:
		return "stopped"
	}
}

// Status is a RunState plus whatever detail explains it.
type Status struct {
	State  RunState
	Detail string
}

// IsUp is true for anything other than Stopped.
func (s Status) IsUp() bool {
	return s.State != Stopped
}

func qemuPidAlive(pidFile string) bool {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	// guards against PID reuse after quickemu left a stale pid file behind
	return bytes.Contains(cmdline, []byte("qemu"))
}

// QueryStatus asks the monitor first ("info status"), then falls back to the pid
// file. A socket file with nobody listening (refused) counts as stopped.
func QueryStatus(v VM, timeout time.Duration) Status {
	p, err := v.Paths()
	if err != nil {
		return Status{State: Stopped, Detail: err.Error()}
	}
	if _, err := os.Stat(p.MonitorSocket); err == nil {
		if out, err := MonitorCommand(p.MonitorSocket, "info status", timeout); err == nil {
			if strings.Contains(out, "paused") {
				return Status{State: Paused, Detail: out}
			}
			return Status{State: Running, Detail: out}
		}
	}
	if qemuPidAlive(p.PidFile) {
		return Status{State: Busy, Detail: "qemu is running but its monitor isn't answering (another client attached?)"}
	}
	return Status{State: Stopped}
}

// QueryAll checks every VM concurrently, keyed by ConfPath.
func QueryAll(vms []VM, timeout time.Duration) map[string]Status {
	result := make(map[string]Status, len(vms))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, vm := range vms {
		wg.Add(1)
		go func(vm VM) {
			defer wg.Done()
			s := QueryStatus(vm, timeout)
			mu.Lock()
			result[vm.ConfPath] = s
			mu.Unlock()
		}(vm)
	}
	wg.Wait()
	return result
}

// ErrRunning is returned (wrapped) by operations that need the VM powered off.
var ErrRunning = errors.New("the VM must be shut down first")

// RequireStopped re-checks state right before a disk operation; the UI's view
// may be a couple of seconds old.
func RequireStopped(v VM) error {
	if s := QueryStatus(v, time.Second); s.IsUp() {
		return fmt.Errorf("%s is %s: %w", v.Name(), s.State, ErrRunning)
	}
	return nil
}
