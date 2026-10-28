package qemu

import (
	"bytes"
	"fmt"
	"os"
)

// qemuArgs is the command line of the qemu whose pid is in pidFile. It's what
// qemu was really started with, which a .conf (edited since, or only applying
// on the next start) can't tell us.
func qemuArgs(pidFile string) ([]string, bool) {
	pid, ok := readPid(pidFile)
	if !ok {
		return nil, false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !bytes.Contains(cmdline, []byte("qemu")) {
		return nil, false
	}
	args := bytes.Split(bytes.TrimSuffix(cmdline, []byte{0}), []byte{0})
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = string(a)
	}
	return out, true
}
