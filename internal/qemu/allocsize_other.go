//go:build !unix

package qemu

import "io/fs"

// allocatedSize falls back to the file's length where blocks aren't reported.
func allocatedSize(info fs.FileInfo) int64 {
	return info.Size()
}
