package qemu

import (
	"bytes"
	"time"

	"golang.org/x/sys/unix"
)

// processInfo asks the kernel (sysctl kern.proc.pid) about pid: it's qemu if
// its command name (qemu-system-*, cut to 16 bytes) says so.
func processInfo(pid int) (time.Time, bool) {
	kp, ok := kinfo(pid)
	if !ok || !bytes.Contains(kp.Proc.P_comm[:], []byte("qemu")) {
		return time.Time{}, false
	}
	return time.Unix(kp.Proc.P_starttime.Unix()), true
}

func processStarted(pid int) time.Time {
	if kp, ok := kinfo(pid); ok {
		return time.Unix(kp.Proc.P_starttime.Unix())
	}
	return time.Time{}
}

func kinfo(pid int) (*unix.KinfoProc, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return kp, err == nil && kp.Proc.P_pid == int32(pid)
}
