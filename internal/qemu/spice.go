package qemu

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// SpiceTarget is where a running VM's SPICE server listens: a unix socket, or
// else a TCP port on localhost. The zero value means there is none.
type SpiceTarget struct {
	Socket string
	Port   int
}

func (t SpiceTarget) IsZero() bool {
	return t == SpiceTarget{}
}

// RunningSpice is where the running qemu of v serves SPICE, read from the
// qemu process's own -spice option: the .conf can have changed since it
// started, and quickemu's .spice file can be stale.
func RunningSpice(v VM) (SpiceTarget, bool) {
	p, err := v.Paths()
	if err != nil {
		return SpiceTarget{}, false
	}
	args, ok := qemuArgs(p.PidFile)
	if !ok {
		return SpiceTarget{}, false
	}
	return spiceFromArgs(v, args)
}

// spiceFromArgs finds the endpoint in qemu's arguments: "-spice
// unix=on,addr=<socket>,..." or "-spice port=<n>,...". A socket path is
// relative to where qemu ran, which is the .conf's directory.
func spiceFromArgs(v VM, args []string) (SpiceTarget, bool) {
	for i, arg := range args {
		if arg != "-spice" || i+1 >= len(args) {
			continue
		}
		var unix bool
		var addr string
		var port int
		for _, opt := range strings.Split(args[i+1], ",") {
			key, value, _ := strings.Cut(opt, "=")
			switch key {
			case "unix":
				unix = value == "on"
			case "addr":
				addr = value
			case "port":
				port, _ = strconv.Atoi(value)
			}
		}
		switch {
		case unix && addr != "":
			return SpiceTarget{Socket: v.Resolve(addr)}, true
		case port > 0:
			return SpiceTarget{Port: port}, true
		}
	}
	return SpiceTarget{}, false
}

// spiceViewers are the viewers we know how to drive, in order of preference
// (the first is quickemu's own default).
var spiceViewers = []string{"spicy", "remote-viewer"}

// FindSpiceViewer returns the first of spiceViewers on PATH.
func FindSpiceViewer() (string, error) {
	for _, name := range spiceViewers {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no SPICE viewer found: install %s", strings.Join(spiceViewers, " or "))
}

// ViewerArgs are the arguments that make viewer (spicy or remote-viewer, by
// its base name) connect to t.
func (t SpiceTarget) ViewerArgs(viewer string) []string {
	remote := filepath.Base(viewer) == "remote-viewer"
	switch {
	case t.Socket != "" && remote:
		return []string{"spice+unix://" + t.Socket}
	case t.Socket != "":
		return []string{"--uri=spice+unix://" + t.Socket}
	case remote:
		return []string{"spice://localhost:" + strconv.Itoa(t.Port)}
	default:
		return []string{"-h", "localhost", "-p", strconv.Itoa(t.Port)}
	}
}

// StartViewer opens viewer on t in its own session, so it outlives the TUI.
// Its output is appended to Paths.ViewerLog (rolled over first if it has grown
// large); logFrom is where this viewer's own output will start in it. The
// caller should Wait() on the returned command to reap it.
func StartViewer(v VM, viewer string, t SpiceTarget) (cmd *exec.Cmd, logFrom int64, err error) {
	p, err := v.Paths()
	if err != nil {
		return nil, 0, err
	}
	if err := rollLog(p.ViewerLog, maxLogSize, logsToKeep); err != nil {
		return nil, 0, err
	}
	logFile, err := os.OpenFile(p.ViewerLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, 0, err
	}
	defer logFile.Close() // the child keeps its own copy of the descriptor
	if info, err := logFile.Stat(); err == nil {
		logFrom = info.Size()
	}
	cmd = exec.Command(viewer, t.ViewerArgs(viewer)...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}
	return cmd, logFrom, nil
}
