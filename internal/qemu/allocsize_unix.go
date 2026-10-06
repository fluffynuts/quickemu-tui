//go:build unix

package qemu

import (
	"io/fs"
	"syscall"
)

// allocatedSize is the space info's file takes on disk (its 512-byte blocks,
// as du counts them), which for a sparse disk image is less than its length.
func allocatedSize(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}
