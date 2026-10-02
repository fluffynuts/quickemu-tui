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

	dir := filepath.Clean(p.VMDir)
	base := filepath.Clean(v.BaseDir())
	rel, relErr := filepath.Rel(base, dir)
	switch {
	case relErr == nil && rel == ".":
		plan.KeepReason = fmt.Sprintf("its disk is stored directly in %s, which holds your other VMs, so that directory is left alone", tilde(base))
		return plan, nil
	case relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		plan.KeepReason = fmt.Sprintf("%s isn't inside %s, so it's left alone", tilde(dir), tilde(base))
		return plan, nil
	}
	st, err := os.Lstat(dir)
	switch {
	case os.IsNotExist(err):
		return plan, nil // nothing to remove besides the conf
	case err != nil:
		return plan, err
	case st.Mode()&os.ModeSymlink != 0:
		plan.KeepReason = fmt.Sprintf("%s is a symbolic link, so it's left alone", tilde(dir))
		return plan, nil
	case !st.IsDir():
		plan.KeepReason = fmt.Sprintf("%s isn't a directory", tilde(dir))
		return plan, nil
	}
	others, _ := Discover(base)
	for _, o := range others {
		if o.ConfPath == v.ConfPath {
			continue
		}
		if op, err := o.Paths(); err == nil && filepath.Clean(op.VMDir) == dir {
			plan.KeepReason = fmt.Sprintf("%s is also used by %s, so it's left alone", tilde(dir), o.Name())
			return plan, nil
		}
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
