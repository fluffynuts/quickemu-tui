//go:build !linux && !darwin

package qemu

import "time"

// processInfo can't look processes up here, so a qemu whose monitor doesn't
// answer counts as stopped.
func processInfo(pid int) (time.Time, bool) {
	return time.Time{}, false
}
