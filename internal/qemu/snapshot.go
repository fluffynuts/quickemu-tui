package qemu

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Snapshot is one qcow2 internal snapshot.
type Snapshot struct {
	ID          string
	Name        string
	Date        time.Time
	VMStateSize int64 // 0 for disk-only snapshots (what qemu-img creates)
}

// Ref is what to pass to qemu-img: the tag, or the ID for untagged snapshots.
func (s Snapshot) Ref() string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

// DiskInfo is the subset of `qemu-img info --output=json` we use.
type DiskInfo struct {
	VirtualSize int64
	ActualSize  int64
	Snapshots   []Snapshot
}

// ParseDiskInfo decodes `qemu-img info --output=json` output.
func ParseDiskInfo(data []byte) (DiskInfo, error) {
	var raw struct {
		VirtualSize int64 `json:"virtual-size"`
		ActualSize  int64 `json:"actual-size"`
		Snapshots   []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DateSec     int64  `json:"date-sec"`
			VMStateSize int64  `json:"vm-state-size"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return DiskInfo{}, fmt.Errorf("parsing qemu-img output: %w", err)
	}
	info := DiskInfo{VirtualSize: raw.VirtualSize, ActualSize: raw.ActualSize}
	for _, s := range raw.Snapshots {
		info.Snapshots = append(info.Snapshots, Snapshot{
			ID:          s.ID,
			Name:        s.Name,
			Date:        time.Unix(s.DateSec, 0),
			VMStateSize: s.VMStateSize,
		})
	}
	return info, nil
}

// GetDiskInfo runs qemu-img info. -U (force-share) allows reading a disk that a
// running qemu holds locked, so this also works while the VM is up.
func GetDiskInfo(disk string) (DiskInfo, error) {
	out, err := runCommand("", 30*time.Second, "qemu-img", "info", "-U", "--output=json", disk)
	if err != nil {
		return DiskInfo{}, err
	}
	return ParseDiskInfo([]byte(out))
}

var (
	tagRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	digitsRe = regexp.MustCompile(`^[0-9]+$`)
)

// ValidateTag rejects tags that are awkward or ambiguous for qemu-img.
func ValidateTag(tag string) error {
	if !tagRe.MatchString(tag) {
		return errors.New("snapshot tags use letters, digits, '.', '_' or '-' (no spaces), starting with a letter or digit")
	}
	if digitsRe.MatchString(tag) {
		return errors.New("purely numeric tags are ambiguous with snapshot IDs in qemu-img")
	}
	return nil
}

func snapshotOp(v VM, flag, ref string) error {
	if err := RequireStopped(v); err != nil {
		return err
	}
	p, err := v.Paths()
	if err != nil {
		return err
	}
	_, err = runCommand("", 0, "qemu-img", "snapshot", flag, ref, p.Disk)
	return err
}

// CreateSnapshot makes a disk-only internal snapshot (VM must be off).
func CreateSnapshot(v VM, tag string) error {
	if err := ValidateTag(tag); err != nil {
		return err
	}
	return snapshotOp(v, "-c", tag)
}

// ApplySnapshot reverts the disk to ref, discarding the current state.
func ApplySnapshot(v VM, ref string) error {
	return snapshotOp(v, "-a", ref)
}

// DeleteSnapshot removes ref. Space is reused inside the qcow2; the file doesn't shrink.
func DeleteSnapshot(v VM, ref string) error {
	return snapshotOp(v, "-d", ref)
}
