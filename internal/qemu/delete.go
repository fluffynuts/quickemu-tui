package qemu

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DeletePlan is what removing a VM would delete, worked out (and sanity
// checked) before anything is touched.
type DeletePlan struct {
	Conf string // the .conf file

	// Dir is the VM's folder, to be removed with everything in it. It is empty
	// when there is nothing safe to remove; KeepReason then says why.
	Dir        string
	KeepReason string
	Files      int   // files under Dir
	Bytes      int64 // their total size

	Shortcut string // desktop launcher quickemu may have created; empty if none
}

// PlanDelete decides what deleting v removes. The VM's folder (the directory
// of its disk image, like quickemu's own --delete-vm) is only included when it
// is a real directory strictly inside the directory holding the .conf and no
// other VM uses it. Otherwise (say disk_img="disk.qcow2" next to the conf,
// which would make the folder the whole VM directory) only the .conf goes.
func PlanDelete(v VM) (DeletePlan, error) {
	plan := DeletePlan{Conf: v.ConfPath}
	p, err := v.Paths()
	if err != nil {
		return plan, err
	}
	if home, err := os.UserHomeDir(); err == nil {
		sc := filepath.Join(home, ".local", "share", "applications", v.Name()+".desktop")
		if _, err := os.Lstat(sc); err == nil {
			plan.Shortcut = sc
		}
	}

	dir, reason, err := ownDir(v, p)
	if err != nil || dir == "" {
		plan.KeepReason = reason
		return plan, err
	}

	plan.Dir = dir
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			plan.Files++
			plan.Bytes += info.Size()
		}
		return nil
	})
	return plan, nil
}

// ownDir is v's folder (the directory of its disk image, p.VMDir) if it
// belongs to v alone: a real directory strictly inside the directory holding
// the .conf that no other VM uses. Otherwise it is empty, with the reason
// (empty too when the folder doesn't exist).
func ownDir(v VM, p Paths) (dir, reason string, err error) {
	dir = filepath.Clean(p.VMDir)
	base := filepath.Clean(v.BaseDir())
	rel, relErr := filepath.Rel(base, dir)
	switch {
	case relErr == nil && rel == ".":
		return "", fmt.Sprintf("its disk is stored directly in %s, which holds your other VMs, so that directory is left alone", tilde(base)), nil
	case relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return "", fmt.Sprintf("%s isn't inside %s, so it's left alone", tilde(dir), tilde(base)), nil
	}
	st, err := os.Lstat(dir)
	switch {
	case os.IsNotExist(err):
		return "", "", nil // nothing besides the conf
	case err != nil:
		return "", "", err
	case st.Mode()&os.ModeSymlink != 0:
		return "", fmt.Sprintf("%s is a symbolic link, so it's left alone", tilde(dir)), nil
	case !st.IsDir():
		return "", fmt.Sprintf("%s isn't a directory", tilde(dir)), nil
	}
	others, _ := Discover(base)
	for _, o := range others {
		if o.ConfPath == v.ConfPath {
			continue
		}
		if op, err := o.Paths(); err == nil && filepath.Clean(op.VMDir) == dir {
			return "", fmt.Sprintf("%s is also used by %s, so it's left alone", tilde(dir), o.Name()), nil
		}
	}

	return dir, "", nil
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// DeleteVM carries out plan for v. The VM must be stopped. The folder goes
// first, so if that fails the .conf stays and the VM remains listed.
func DeleteVM(v VM, plan DeletePlan) error {
	if err := RequireStopped(v); err != nil {
		return err
	}
	if plan.Dir != "" {
		if err := os.RemoveAll(plan.Dir); err != nil {
			return fmt.Errorf("removing %s: %w", plan.Dir, err)
		}
	}
	if err := os.Remove(plan.Conf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", plan.Conf, err)
	}
	if plan.Shortcut != "" {
		_ = os.Remove(plan.Shortcut)
	}
	return nil
}
