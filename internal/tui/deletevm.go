package tui

import (
	"fmt"
	"strings"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// deleteVMPrompt spells out exactly what will be removed.
func deleteVMPrompt(vm qemu.VM, plan qemu.DeletePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Permanently delete %s?\n\nThis will remove:\n", vm.Name())
	fmt.Fprintf(&b, "  • %s\n", tildify(plan.Conf))
	if plan.Dir != "" {
		fmt.Fprintf(&b, "  • %s/ and everything in it (%s, %s)\n", tildify(plan.Dir), plural(plan.Files, "file"), qemu.HumanSize(plan.Bytes))
	}
	if plan.Shortcut != "" {
		fmt.Fprintf(&b, "  • %s\n", tildify(plan.Shortcut))
	}
	if plan.KeepReason != "" {
		fmt.Fprintf(&b, "\nNot removed: %s.\n", plan.KeepReason)
	}
	b.WriteString("\nThis cannot be undone.")
	return b.String()
}
