package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// askRenameVM prompts for vm's new name, then confirms what will change.
func (m *Model) askRenameVM(vm qemu.VM) tea.Cmd {
	return m.askPrompt("Rename "+vm.Name()+" to", vm.Name(), func(m *Model, name string) tea.Cmd {
		if name == "" || name == vm.Name() {
			return nil
		}
		plan, err := qemu.PlanRename(vm, name)
		if err != nil {
			m.setFlash("Can't rename "+vm.Name()+": "+firstLine(err.Error()), true)
			return nil
		}
		m.askConfirm(renameVMPrompt(vm, plan), defaultYes, func(m *Model) tea.Cmd {
			return m.startOp("Rename "+vm.Name()+" to "+name, vm, opDoneMsg{selectConf: plan.NewConf}, func() error {
				return qemu.RenameVM(vm, plan)
			})
		})
		return nil
	})
}

// renameVMPrompt spells out what renaming changes.
func renameVMPrompt(vm qemu.VM, plan qemu.RenamePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rename %s to %s?\n\n", vm.Name(), plan.NewName)
	// both live in the VM directory shown in the header, so names are enough
	fmt.Fprintf(&b, "  • %s → %s\n", filepath.Base(plan.OldConf), filepath.Base(plan.NewConf))
	if plan.NewDir != "" {
		fmt.Fprintf(&b, "  • %s/ → %s/\n", filepath.Base(plan.OldDir), filepath.Base(plan.NewDir))
	}
	if len(plan.PathChanges) > 0 {
		fmt.Fprintf(&b, "  • %s in the .conf updated to the new folder\n", strings.Join(plan.PathChanges, ", "))
	}
	if len(plan.Renames) > 0 {
		fmt.Fprintf(&b, "  • %s renamed to match\n", strings.Join(plan.Renames, ", "))
	}
	if len(plan.Stale) > 0 {
		fmt.Fprintf(&b, "  • %s removed (recreated on the next start)\n", plural(len(plan.Stale), "runtime file"))
	}
	if plan.KeepReason != "" {
		fmt.Fprintf(&b, "\nNot moved: %s.\n", plan.KeepReason)
	}
	if plan.CoresWarning {
		fmt.Fprintf(&b, "\nNote: quickemu only gives Windows 11 the 2 CPU cores it needs when the VM's name contains \"windows-11\" or \"win11\". Add cpu_cores=\"2\" (or more) to the .conf if it won't install.\n")
	}
	if plan.Shortcut != "" {
		fmt.Fprintf(&b, "\nThe desktop shortcut %s still opens the old name; recreate it with quickemu --shortcut.\n", tildify(plan.Shortcut))
	}
	return strings.TrimRight(b.String(), "\n")
}
