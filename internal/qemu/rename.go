package qemu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var vmNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateVMName rejects names quickemu would trip over: it builds paths from
// the name, and not all of them are quoted.
func ValidateVMName(name string) error {
	if !vmNameRe.MatchString(name) {
		return errors.New("VM names use letters, digits, '.', '_' or '-' (no spaces), starting with a letter or digit")
	}
	return nil
}

// Files in a VM's folder that quickemu names after the VM. Logs and UEFI vars
// are worth keeping, so they're renamed; the rest are recreated on every start.
var (
	renamedSuffixes = []string{".log", ".tui-launch.log", "-vars.fd"}
	staleSuffixes   = []string{".pid", ".ports", ".spice", ".sock", ".sh", "-monitor.socket", "-serial.socket", ".swtpm-sock"}
)

// RenamePlan is what renaming a VM would change, worked out before anything
// is touched.
type RenamePlan struct {
	NewName string
	OldConf string
	NewConf string

	// OldDir is moved to NewDir. Both are empty when the folder stays put;
	// KeepReason then says why.
	OldDir     string
	NewDir     string
	KeepReason string

	ConfText    string   // the new .conf contents
	PathChanges []string // keys whose paths now point into NewDir
	Renames     []string // files in the folder renamed to match (base names, before)
	Stale       []string // runtime files removed (base names)

	Shortcut     string // desktop launcher still naming the old .conf; left as is
	CoresWarning bool   // quickemu would stop raising the CPU count for Windows 11
}

// PlanRename decides what renaming v to newName involves. The VM's folder (the
// directory of its disk image) only moves when it's a real directory strictly
// inside the directory holding the .conf, is named after the VM and no other
// VM uses it; otherwise only the .conf is renamed.
func PlanRename(v VM, newName string) (RenamePlan, error) {
	if err := ValidateVMName(newName); err != nil {
		return RenamePlan{}, err
	}
	oldName := v.Name()
	if newName == oldName {
		return RenamePlan{}, fmt.Errorf("%s is already called that", oldName)
	}
	base := filepath.Clean(v.BaseDir())
	plan := RenamePlan{NewName: newName, OldConf: v.ConfPath, NewConf: filepath.Join(base, newName+".conf")}
	if _, err := os.Lstat(plan.NewConf); err == nil {
		return plan, fmt.Errorf("%s already exists", tilde(plan.NewConf))
	}

	data, err := os.ReadFile(v.ConfPath)
	if err != nil {
		return plan, err
	}
	p, err := v.Paths()
	if err != nil {
		return plan, err
	}
	dir := filepath.Clean(p.VMDir)
	newDir := filepath.Join(base, newName)

	switch rel, relErr := filepath.Rel(base, dir); {
	case relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		plan.KeepReason = fmt.Sprintf("its files are in %s, not in a folder of its own, so they stay where they are", tilde(dir))
	case filepath.Base(dir) != oldName || filepath.Dir(dir) != base:
		plan.KeepReason = fmt.Sprintf("its folder %s isn't named after the VM, so it keeps its name", tilde(dir))
	default:
		st, err := os.Lstat(dir)
		switch {
		case err != nil && !os.IsNotExist(err):
			return plan, err
		case err == nil && st.Mode()&os.ModeSymlink != 0:
			plan.KeepReason = fmt.Sprintf("%s is a symbolic link, so it keeps its name", tilde(dir))
		case err == nil && !st.IsDir():
			plan.KeepReason = fmt.Sprintf("%s isn't a directory", tilde(dir))
		default:
			if other := vmSharingDir(v, dir); other != "" {
				plan.KeepReason = fmt.Sprintf("%s is also used by %s, so it keeps its name", tilde(dir), other)
				break
			}
			if _, err := os.Lstat(newDir); err == nil {
				return plan, fmt.Errorf("%s already exists", tilde(newDir))
			}
			plan.OldDir, plan.NewDir = dir, newDir // may not exist yet: then only the paths change
			if err == nil {
				plan.Renames = existing(dir, oldName, renamedSuffixes)
				plan.Stale = existing(dir, oldName, staleSuffixes)
			}
		}
	}

	plan.ConfText = string(data)
	if plan.NewDir != "" {
		plan.ConfText, plan.PathChanges = repointConf(v, plan.ConfText, plan.OldDir, plan.NewDir)
	}

	if home, err := os.UserHomeDir(); err == nil {
		sc := filepath.Join(home, ".local", "share", "applications", oldName+".desktop")
		if _, err := os.Lstat(sc); err == nil {
			plan.Shortcut = sc
		}
	}

	// quickemu gives Windows 11 at least 2 cores, but only when the name says
	// it's Windows 11 (VMNAME matching *windows-11* or *win11*)
	conf := ParseConf(string(data))
	looksWin11 := func(n string) bool { return strings.Contains(n, "windows-11") || strings.Contains(n, "win11") }
	cores := conf["cpu_cores"]
	plan.CoresWarning = conf["guest_os"] == "windows" && looksWin11(oldName) && !looksWin11(newName) && (cores == "" || cores == "1")
	return plan, nil
}

func vmSharingDir(v VM, dir string) string {
	others, _ := Discover(v.BaseDir())
	for _, o := range others {
		if o.ConfPath == v.ConfPath {
			continue
		}
		if op, err := o.Paths(); err == nil && filepath.Clean(op.VMDir) == dir {
			return o.Name()
		}
	}
	return ""
}

func existing(dir, name string, suffixes []string) []string {
	var out []string
	for _, s := range suffixes {
		if _, err := os.Lstat(filepath.Join(dir, name+s)); err == nil {
			out = append(out, name+s)
		}
	}
	return out
}

// repointConf rewrites assignments whose value is a path inside oldDir to
// point into newDir, keeping each one's style (relative, ~/ or absolute) and
// anything after the value, such as a comment. Other lines are untouched.
func repointConf(v VM, text, oldDir, newDir string) (string, []string) {
	lines := strings.Split(text, "\n")
	var changed []string
	for i, line := range lines {
		m := assignRe.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		key := line[m[2]:m[3]]
		rhs := line[m[4]:m[5]]
		lead := len(rhs) - len(strings.TrimLeft(rhs, " \t"))
		word := firstWord(rhs[lead:])
		value := UnquoteValue(word)
		if value == "" || strings.HasPrefix(value, "(") {
			continue
		}
		rel, err := filepath.Rel(oldDir, v.Resolve(value))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		var target string
		switch {
		case value == "~" || strings.HasPrefix(value, "~/"):
			target = tilde(newDir)
		case filepath.IsAbs(value):
			target = newDir
		default:
			target, err = filepath.Rel(v.BaseDir(), newDir)
			if err != nil {
				continue
			}
		}
		if rel != "." {
			target = filepath.Join(target, rel)
		}
		lines[i] = line[:m[4]] + rhs[:lead] + quoteValue(target) + rhs[lead+len(word):]
		changed = append(changed, key)
	}
	return strings.Join(lines, "\n"), changed
}

// firstWord returns the leading shell word of s: up to the first whitespace
// outside quotes.
func firstWord(s string) string {
	var quote rune
	escaped := false
	for i, c := range s {
		switch {
		case escaped:
			escaped = false
		case c == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case unicode.IsSpace(c):
			return s[:i]
		}
	}
	return s
}

// quoteValue double-quotes s for bash, escaping what double quotes don't protect.
func quoteValue(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		if strings.ContainsRune(dqEscapable, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	b.WriteByte('"')
	return b.String()
}

// RenameVM carries out plan for v, which must be stopped. The folder moves
// first and the old .conf goes last; if a step fails, the earlier ones are
// undone so the VM is never left pointing at a folder that isn't there.
// Renaming logs and removing runtime files is best-effort afterwards.
func RenameVM(v VM, plan RenamePlan) error {
	if err := RequireStopped(v); err != nil {
		return err
	}
	if _, err := os.Lstat(plan.NewConf); err == nil {
		return fmt.Errorf("%s already exists", plan.NewConf)
	}
	st, err := os.Stat(plan.OldConf)
	if err != nil {
		return err
	}

	moved := false
	if plan.NewDir != "" {
		if _, err := os.Lstat(plan.NewDir); err == nil {
			return fmt.Errorf("%s already exists", plan.NewDir)
		}
		if _, err := os.Lstat(plan.OldDir); err == nil {
			if err := os.Rename(plan.OldDir, plan.NewDir); err != nil {
				return fmt.Errorf("renaming %s: %w", plan.OldDir, err)
			}
			moved = true
		}
	}
	undoMove := func() {
		if moved {
			_ = os.Rename(plan.NewDir, plan.OldDir)
		}
	}

	f, err := os.OpenFile(plan.NewConf, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		undoMove()
		return fmt.Errorf("creating %s: %w", plan.NewConf, err)
	}
	_, werr := f.WriteString(plan.ConfText)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(plan.NewConf, st.Mode().Perm()) // OpenFile's mode is subject to the umask
	}
	if werr != nil {
		_ = os.Remove(plan.NewConf)
		undoMove()
		return fmt.Errorf("writing %s: %w", plan.NewConf, werr)
	}
	if err := os.Remove(plan.OldConf); err != nil {
		_ = os.Remove(plan.NewConf)
		undoMove()
		return fmt.Errorf("removing %s: %w", plan.OldConf, err)
	}

	if moved {
		oldName := strings.TrimSuffix(filepath.Base(plan.OldConf), ".conf")
		for _, f := range plan.Renames {
			_ = os.Rename(filepath.Join(plan.NewDir, f), filepath.Join(plan.NewDir, plan.NewName+strings.TrimPrefix(f, oldName)))
		}
		for _, f := range plan.Stale {
			_ = os.Remove(filepath.Join(plan.NewDir, f))
		}
	}
	return nil
}
