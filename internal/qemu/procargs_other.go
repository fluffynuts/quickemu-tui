//go:build !linux

package qemu

// qemuArgs can't read another process's command line here, so a running VM's
// SPICE endpoint isn't known (the viewer can't be reopened).
func qemuArgs(pidFile string) ([]string, bool) {
	return nil, false
}
