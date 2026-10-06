package qemu

import (
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
	// Started is when the qemu process started; zero if unknown.
	Started time.Time
}

// IsUp is true for anything other than Stopped.
func (s Status) IsUp() bool {
	return s.State != Stopped
}

// qemuProcess reports whether the pid in pidFile is a live qemu (guarding
// against PID reuse after quickemu left a stale pid file behind) and when it
// started (zero if that can't be told). See proc_*.go for each OS.
func qemuProcess(pidFile string) (started time.Time, alive bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return time.Time{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return time.Time{}, false
	}
	return processInfo(pid)
}

func started(pidFile string) time.Time {
	t, _ := qemuProcess(pidFile)
	return t
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
				return Status{State: Paused, Detail: out, Started: started(p.PidFile)}
			}
			return Status{State: Running, Detail: out, Started: started(p.PidFile)}
		}
	}
	if started, alive := qemuProcess(p.PidFile); alive {
		return Status{State: Busy, Detail: "qemu is running but its monitor isn't answering (another client attached?)",
			Started: started}
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
