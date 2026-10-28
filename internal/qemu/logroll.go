package qemu

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	maxLogSize = 1 << 20
	logsToKeep = 3
)

// rollLog moves a log that has outgrown maxBytes to path.1, and that one to
// path.2, and so on, keeping keep files in all (path included). Only the
// size at the moment of the call matters, so a log can overshoot a little.
func rollLog(path string, maxBytes int64, keep int) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxBytes {
		return nil // missing is fine: there's nothing to roll
	}
	if err := os.Remove(fmt.Sprintf("%s.%d", path, keep-1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := keep - 2; i >= 0; i-- {
		from := path
		if i > 0 {
			from = fmt.Sprintf("%s.%d", path, i)
		}
		if err := os.Rename(from, fmt.Sprintf("%s.%d", path, i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ReadFrom returns what was written to the file from offset on, trimmed. An
// offset past the end means the file was rolled over since, so all of it.
func ReadFrom(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() < offset {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, _ := io.ReadAll(f)
	return strings.TrimSpace(string(data))
}
