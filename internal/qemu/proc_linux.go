package qemu

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// userHZ is the kernel's USER_HZ, the unit of /proc/<pid>/stat's times; it is
// 100 on every Linux architecture qemu runs on.
const userHZ = 100

// processInfo reads /proc: pid is qemu if its command line says so, and it
// started at boot time (btime in /proc/stat) plus its start time in clock
// ticks since boot (/proc/<pid>/stat field 22).
func processInfo(pid int) (time.Time, bool) {
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !bytes.Contains(cmdline, []byte("qemu")) {
		return time.Time{}, false
	}
	return processStarted(pid), true
}

func processStarted(pid int) time.Time {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}
	}
	// the command (field 2) is in parentheses and may hold spaces, so count
	// fields from after its closing parenthesis, where field 3 starts
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return time.Time{}
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return time.Time{}
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}
	}
	boot := bootTime()
	if boot.IsZero() {
		return time.Time{}
	}
	return boot.Add(time.Duration(ticks) * time.Second / userHZ)
}

func bootTime() time.Time {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(secs, 0)
			}
		}
	}
	return time.Time{}
}
