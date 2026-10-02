package qemu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// CommandError is a failed external command, with its stderr (or stdout).
type CommandError struct {
	Args   []string
	Code   int
	Output string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("%s failed (exit %d): %s", strings.Join(e.Args, " "), e.Code, e.Output)
}

// runCommand runs name+args in dir (empty: current dir). timeout <= 0: no limit.
func runCommand(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stderr.String())
		if out == "" {
			out = strings.TrimSpace(stdout.String())
		}
		if out == "" {
			out = err.Error()
		}
		code := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return "", &CommandError{Args: append([]string{name}, args...), Code: code, Output: out}
	}
	return stdout.String(), nil
}

// FindQuickemu returns override if set, else quickemu from PATH.
func FindQuickemu(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return exec.LookPath("quickemu")
}

// Start launches quickemu for v in its own session, so the VM outlives the TUI.
//
// quickemu's output goes to Paths.LaunchLog rather than a pipe: qemu inherits
// quickemu's stdout/stderr, and a pipe would SIGPIPE it once the TUI exits.
// The caller should Wait() on the returned command to reap it.
func Start(v VM, quickemu string, extraArgs ...string) (*exec.Cmd, error) {
	p, err := v.Paths()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p.LaunchLog), 0o755); err != nil {
		return nil, err
	}
	logFile, err := os.Create(p.LaunchLog)
	if err != nil {
		return nil, err
	}
	defer logFile.Close() // the child keeps its own copy of the descriptor

	args := append([]string{"--vm", filepath.Base(v.ConfPath)}, extraArgs...)
	cmd := exec.Command(quickemu, args...)
	cmd.Dir = v.BaseDir()
	cmd.Stdin = nil // /dev/null; the terminal belongs to the TUI
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// Shutdown presses the virtual ACPI power button; the guest decides what to do.
func Shutdown(v VM) error {
	p, err := v.Paths()
	if err != nil {
		return err
	}
	_, err = MonitorCommand(p.MonitorSocket, "system_powerdown", 3*time.Second)
	return err
}

// ForceStop uses quickemu --kill, falling back to the monitor's "quit".
func ForceStop(v VM, quickemu string) error {
	_, killErr := runCommand(v.BaseDir(), 30*time.Second, quickemu, "--vm", filepath.Base(v.ConfPath), "--kill")
	if killErr == nil {
		return nil
	}
	p, err := v.Paths()
	if err != nil {
		return killErr
	}
	_, quitErr := MonitorCommand(p.MonitorSocket, "quit", 3*time.Second)
	if quitErr == nil || errors.Is(quitErr, ErrMonitorClosed) {
		return nil // qemu hanging up is what "quit" looks like when it works
	}
	return fmt.Errorf("%v; monitor quit also failed: %v", killErr, quitErr)
}
