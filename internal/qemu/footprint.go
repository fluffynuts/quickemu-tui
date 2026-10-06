package qemu

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Footprint is how much disk space a VM takes up.
type Footprint struct {
	Bytes int64 // allocated on disk, so sparse files count what they really use
	Files int
	// Dir is the VM's own folder, counted with everything in it. It is empty
	// when there's no folder yet, or when the folder isn't the VM's alone
	// (Shared; see ownDir): then only the .conf and the files quickemu names
	// after the VM (disk image, logs, launch script…) are counted.
	Dir    string
	Shared bool
}

// MeasureFootprint adds up the .conf and the files that make up v: the same
// ones deleting it removes.
func MeasureFootprint(v VM) (Footprint, error) {
	var fp Footprint
	add := func(info fs.FileInfo) {
		if info.Mode().IsRegular() {
			fp.Files++
			fp.Bytes += allocatedSize(info)
		}
	}
	if info, err := os.Lstat(v.ConfPath); err == nil {
		add(info)
	}
	p, err := v.Paths()
	if err != nil {
		return fp, err
	}
	dir, reason, err := ownDir(v, p)
	if err != nil {
		return fp, err
	}
	if dir == "" {
		fp.Shared = reason != ""
		for _, f := range []string{p.Disk, p.PidFile, p.PortsFile, p.LaunchScript, p.QuickemuLog, p.LaunchLog} {
			if info, err := os.Lstat(f); err == nil {
				add(info)
			}
		}
		return fp, nil
	}
	fp.Dir = dir
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corners don't stop the count
		}
		if info, err := d.Info(); err == nil {
			add(info)
		}
		return nil
	})
	return fp, err
}
