package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// What the CPU and memory steps read about this machine; tests replace them.
var (
	hostCPUs = qemu.HostCPUs
	hostRAM  = qemu.HostRAM
)

// hostInfo is the machine the new VM's CPU and memory are chosen against.
type hostInfo struct {
	cpus     int
	ramBytes int64 // 0 if it couldn't be read
}

func readHostInfo() hostInfo {
	h := hostInfo{cpus: hostCPUs()}
	if b, err := hostRAM(); err == nil {
		h.ramBytes = b
	}
	return h
}

// defaultFor is the user's default .conf line setting key, if any.
func (m Model) defaultFor(key string) string {
	for _, d := range m.defaults {
		if k, ok := qemu.DefaultKey(d); ok && k == key {
			return strings.TrimSpace(d)
		}
	}
	return ""
}

func (m Model) cpuItems() []pickItem {
	auto := qemu.AutoCoreTiers[qemu.AutoTierFor(qemu.AutoCoreTiers, m.host.cpus)].Value
	hint := fmt.Sprintf("quickemu picks %s on this host", plural(auto, "core"))
	if d := m.defaultFor("cpu_cores"); d != "" {
		hint = "your default: " + d
	}
	items := []pickItem{{value: "", label: "auto", hint: hint}}
	for n := 1; n < m.host.cpus; n++ {
		items = append(items, pickItem{value: strconv.Itoa(n), label: plural(n, "core")})
	}
	return items
}

func (m Model) ramItems() []pickItem {
	hint := "quickemu picks by host memory"
	if m.host.ramBytes > 0 {
		gb := qemu.HostRAMGB(m.host.ramBytes)
		hint = fmt.Sprintf("quickemu picks %dG on this host", qemu.AutoRAMTiers[qemu.AutoTierFor(qemu.AutoRAMTiers, gb)].Value)
	}
	if d := m.defaultFor("ram"); d != "" {
		hint = "your default: " + d
	}
	items := []pickItem{{value: "", label: "auto", hint: hint}}
	for _, g := range qemu.RAMChoices(m.host.ramBytes) {
		items = append(items, pickItem{value: strconv.Itoa(g) + "G", label: strconv.Itoa(g) + "G"})
	}
	return items
}

// sizeChoices are the .conf lines for the CPU and memory picked; auto adds none.
func (m Model) sizeChoices() []string {
	var lines []string
	if m.instCores != "" {
		lines = append(lines, `cpu_cores="`+m.instCores+`"`)
	}
	if m.instRAM != "" {
		lines = append(lines, `ram="`+m.instRAM+`"`)
	}
	return lines
}

// sizeSummary describes the picks for the confirmation, e.g. "CPUs: 4, memory: auto".
func (m Model) sizeSummary() string {
	return "CPUs: " + or(m.instCores, "auto") + ", memory: " + or(m.instRAM, "auto")
}

// viewAutoTable shows quickemu's sizing table with this host's row marked.
func viewAutoTable(what string, tiers []qemu.AutoTier, have int, known bool, unit string) []string {
	lines := []string{dimStyle.Render(fmt.Sprintf("auto: quickemu sizes the VM from the host's %s when it starts", what))}
	mark := -1
	if known {
		mark = qemu.AutoTierFor(tiers, have)
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  %-14s  %s", "host "+what, "VM gets")))
	for i, t := range tiers {
		var span string
		switch {
		case i == 0:
			span = fmt.Sprintf("%d+", t.Min)
		case t.Min == 0:
			span = fmt.Sprintf("under %d", tiers[i-1].Min)
		default:
			span = fmt.Sprintf("%d–%d", t.Min, tiers[i-1].Min-1)
		}
		row := fmt.Sprintf("  %-14s  %d%s", span, t.Value, unit)
		if i == mark {
			lines = append(lines, okStyle.Render(row+"   ← this host"))
		} else {
			lines = append(lines, dimStyle.Render(row))
		}
	}
	return lines
}

func (m Model) viewSizeTable() []string {
	if m.instStep == stepCPU {
		lines := viewAutoTable("CPUs", qemu.AutoCoreTiers, m.host.cpus, true, "")
		return append(lines, "", fmt.Sprintf("This host has %s.", plural(m.host.cpus, "logical CPU")))
	}
	gb := qemu.HostRAMGB(m.host.ramBytes)
	lines := viewAutoTable("RAM (GB)", qemu.AutoRAMTiers, gb, m.host.ramBytes > 0, "G")
	if m.host.ramBytes > 0 {
		return append(lines, "", fmt.Sprintf("This host has %s RAM; choices go up to about half of it.", qemu.HumanSize(m.host.ramBytes)))
	}
	return append(lines, "", errStyle.Render("Couldn't read this host's memory, so only auto is offered."))
}
