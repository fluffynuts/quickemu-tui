package qemu

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// maxDiskSize is far beyond any real disk; it only catches typos like 99999T.
const maxDiskSize = int64(1) << 50 // 1 PiB

// sectorSize is what qemu-img rounds sizes to.
const sectorSize = 512

// DiskSize is a size the user typed: absolute ("40G"), or relative to the
// current size ("+20G", "-5G").
type DiskSize struct {
	Bytes    int64 // negative only when Relative
	Relative bool
}

var (
	numberRe = regexp.MustCompile(`^\d+(?:\.\d+)?`)
	unitRe   = regexp.MustCompile(`^([kmgt])(?:i?b)?$`)
)

var unitBytes = map[string]float64{"k": 1 << 10, "m": 1 << 20, "g": 1 << 30, "t": 1 << 40}

// ParseDiskSize reads sizes like "40G", "512M", "1.5T", "+20G" and "-5G". Units
// are binary (G is GiB, as in qemu-img) and the unit is required, since a bare
// "40" would mean 40 bytes.
func ParseDiskSize(s string) (DiskSize, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return DiskSize{}, errors.New("enter a size, such as 64G, or +10G to add to the current size")
	}
	var d DiskSize
	negative := false
	switch s[0] {
	case '+':
		d.Relative, s = true, s[1:]
	case '-':
		d.Relative, negative, s = true, true, s[1:]
	}
	num := numberRe.FindString(s)
	if num == "" {
		return DiskSize{}, errors.New("a size starts with a number, such as 64G")
	}
	rest := strings.TrimSpace(s[len(num):])
	if rest == "" {
		return DiskSize{}, fmt.Errorf("add a unit to %s: K, M, G or T (for example %sG)", num, num)
	}
	m := unitRe.FindStringSubmatch(rest)
	if m == nil {
		return DiskSize{}, fmt.Errorf("unknown unit %q: use K, M, G or T", rest)
	}
	value, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return DiskSize{}, fmt.Errorf("a size starts with a number: %w", err)
	}
	bytes := math.Round(value * unitBytes[m[1]])
	if bytes < 1 {
		return DiskSize{}, errors.New("a size must be more than 0")
	}
	if bytes > float64(maxDiskSize) {
		return DiskSize{}, fmt.Errorf("%s is too large (the limit is %s)", strings.ToUpper(num+m[1]), HumanSize(maxDiskSize))
	}
	d.Bytes = int64(bytes)
	if negative {
		d.Bytes = -d.Bytes
	}
	return d, nil
}

// resolveTarget is the absolute size spec asks for, given the current size,
// rounded up to a whole sector.
func resolveTarget(current int64, spec DiskSize) (int64, error) {
	target := spec.Bytes
	if spec.Relative {
		target = current + spec.Bytes
	}
	if target <= 0 {
		return 0, errors.New("that would leave no disk: the new size must be more than 0")
	}
	if rem := target % sectorSize; rem != 0 {
		target += sectorSize - rem
	}
	return target, nil
}

// FormatConfSize writes bytes the way a .conf's disk_size does: the largest
// of T, G, M and K that divides it exactly, else plain bytes (which qemu-img
// also accepts).
func FormatConfSize(bytes int64) string {
	for _, u := range []struct {
		suffix string
		size   int64
	}{{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}} {
		if bytes%u.size == 0 {
			return strconv.FormatInt(bytes/u.size, 10) + u.suffix
		}
	}
	return strconv.FormatInt(bytes, 10)
}

// ResizePlan is what resizing a VM's disk would do, worked out before anything
// is touched so the user can be shown it.
type ResizePlan struct {
	Disk      string
	Current   int64
	Target    int64
	Snapshots int // reverting to any of these brings back the size it was taken at
}

// Shrinks is whether the plan makes the disk smaller, which destroys whatever
// the guest keeps beyond the new end.
func (p ResizePlan) Shrinks() bool { return p.Target < p.Current }

// PlanResize works out the resize spec asks for ("64G", "+20G"...). It only
// reads, so it works while the VM runs; ResizeDisk insists it doesn't.
func PlanResize(v VM, spec string) (ResizePlan, error) {
	size, err := ParseDiskSize(spec)
	if err != nil {
		return ResizePlan{}, err
	}
	p, err := v.Paths()
	if err != nil {
		return ResizePlan{}, err
	}
	info, err := GetDiskInfo(p.Disk)
	if err != nil {
		return ResizePlan{}, fmt.Errorf("reading %s: %w", p.Disk, err)
	}
	target, err := resolveTarget(info.VirtualSize, size)
	if err != nil {
		return ResizePlan{}, err
	}
	if target == info.VirtualSize {
		return ResizePlan{}, fmt.Errorf("the disk is already %s", HumanSize(target))
	}
	return ResizePlan{Disk: p.Disk, Current: info.VirtualSize, Target: target, Snapshots: len(info.Snapshots)}, nil
}

// ResizeDisk carries out plan (VM must be off), then records the new size as
// the .conf's disk_size so the two agree. A shrink is passed to qemu-img with
// --shrink, which it insists on.
func ResizeDisk(v VM, plan ResizePlan) error {
	if err := RequireStopped(v); err != nil {
		return err
	}
	p, err := v.Paths()
	if err != nil {
		return err
	}
	if p.Disk != plan.Disk {
		return fmt.Errorf("the VM's disk is now %s, not the %s that was planned: plan again", p.Disk, plan.Disk)
	}
	info, err := GetDiskInfo(p.Disk)
	if err != nil {
		return fmt.Errorf("reading %s: %w", p.Disk, err)
	}
	if info.VirtualSize != plan.Current {
		return fmt.Errorf("the disk changed from %s to %s since this was planned: plan again", HumanSize(plan.Current), HumanSize(info.VirtualSize))
	}
	args := []string{"resize"}
	if plan.Shrinks() {
		args = append(args, "--shrink")
	}
	args = append(args, p.Disk, strconv.FormatInt(plan.Target, 10))
	if _, err := runCommand("", 0, "qemu-img", args...); err != nil {
		return err
	}
	line := `disk_size="` + FormatConfSize(plan.Target) + `"`
	if err := EditConf(v.ConfPath, []string{line}, nil); err != nil {
		return fmt.Errorf("the disk is now %s, but setting %s in %s failed: %w", HumanSize(plan.Target), line, v.ConfPath, err)
	}
	return nil
}
