package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// The quick settings dialog has single-select groups for CPUs and memory,
// offering the same choices as those steps of a new install, and for the
// display, then a checkbox for gl.
const (
	qsCPU = iota
	qsRAM
	qsDisplay
	qsGroups
	qsGL      = qsGroups // focus index of the gl checkbox, after the groups
	qsFocuses = qsGL + 1
)

var qsTitles = [qsGroups]string{"CPUs", "Memory", "Display"}

// displaySizes are the window sizes offered; "fullscreen" follows them.
var displaySizes = []string{"800x600", "1024x768", "1920x1080"}

const fullscreen = "fullscreen"

// quickSettings is the dialog's state: each group's choices, which one is
// picked, and what the .conf said when it opened ("" for unset, i.e. auto).
// gl is whether OpenGL is ticked; glWas is what applied when the dialog opened.
type quickSettings struct {
	vm     qemu.VM
	group  int // focused: a group, or qsGL
	items  [qsGroups][]pickItem
	cursor [qsGroups]int
	was    [qsGroups]string
	gl     bool
	glWas  bool
	glHint string
}

func (m *Model) openQuickSettings(vm qemu.VM) {
	conf, err := vm.Conf()
	if err != nil {
		m.setFlash("Can't read "+vm.Name()+"'s .conf: "+firstLine(err.Error()), true)
		return
	}
	m.host = readHostInfo()
	qs := quickSettings{vm: vm}
	all := [qsGroups][]pickItem{m.cpuItems(), m.ramItems(), m.displayItems()}
	for g := range qsGroups {
		qs.was[g] = qsValue(conf, g)
		qs.items[g], qs.cursor[g] = markCurrent(all[g], qs.was[g])
	}
	qs.gl, qs.glHint = m.glSetting(conf["gl"])
	qs.glWas = qs.gl
	m.qs = qs
	m.mode = modeQuickSettings
}

// qsValue is group g's value according to conf: "" when unset (auto). The
// display is "fullscreen" (our own key, which startVM turns into quickemu's
// --fullscreen), else "WxH" when both width and height are set, as quickemu
// ignores either alone.
func qsValue(conf map[string]string, g int) string {
	switch g {
	case qsCPU:
		return conf["cpu_cores"]
	case qsRAM:
		return conf["ram"]
	}
	if conf["fullscreen"] == "on" {
		return fullscreen
	}
	if conf["width"] != "" && conf["height"] != "" {
		return conf["width"] + "x" + conf["height"]
	}
	return ""
}

// qsEdits are the .conf lines to set and keys to unset to make group g's
// value v.
func qsEdits(g int, v string) (set, unset []string) {
	assign := func(k, v string) string { return k + `="` + v + `"` }
	switch g {
	case qsCPU, qsRAM:
		k := map[int]string{qsCPU: "cpu_cores", qsRAM: "ram"}[g]
		if v == "" {
			return nil, []string{k}
		}
		return []string{assign(k, v)}, nil
	}
	if v == fullscreen {
		return []string{assign("fullscreen", "on")}, []string{"width", "height"}
	}
	w, h, ok := strings.Cut(v, "x")
	if !ok {
		return nil, []string{"fullscreen", "width", "height"}
	}
	return []string{assign("width", w), assign("height", h)}, []string{"fullscreen"}
}

func (m Model) displayItems() []pickItem {
	hint := "quickemu's default"
	if d := m.defaultFor("fullscreen"); d != "" {
		hint = "your default: " + d
	} else if w, h := m.defaultFor("width"), m.defaultFor("height"); w != "" && h != "" {
		hint = "your default: " + w + " " + h
	}
	items := []pickItem{{value: "", label: "auto", hint: hint}}
	for _, s := range displaySizes {
		items = append(items, pickItem{value: s, label: s})
	}
	return append(items, pickItem{value: fullscreen, label: fullscreen})
}

// glSetting is whether gl is on for a VM whose .conf sets gl to value, and
// where that comes from. Unset, the user's default applies on start, failing
// which quickemu's own default (on).
func (m Model) glSetting(value string) (bool, string) {
	if value != "" {
		return value != "off", "set in the .conf"
	}
	if d := m.defaultFor("gl"); d != "" {
		_, v, _ := strings.Cut(d, "=")
		return qemu.UnquoteValue(v) != "off", "your default: " + d
	}
	return true, "quickemu's default"
}

// markCurrent flags the item matching the .conf's value and returns its index.
// A value that isn't one of the choices (set by hand, say) is added, so
// keeping it is an option.
func markCurrent(items []pickItem, current string) ([]pickItem, int) {
	for i := range items {
		if items[i].value == current {
			items[i].hint = strings.TrimSuffix("current, "+items[i].hint, ", ")
			return items, i
		}
	}
	return append(items, pickItem{value: current, label: current, hint: "current"}), len(items)
}

func (m Model) handleQuickSettingsKey(key string) (tea.Model, tea.Cmd) {
	qs := &m.qs
	g := qs.group
	switch key {
	case "ctrl+c":
		return m.requestQuit()
	case "esc", "enter", "q":
		return m.saveQuickSettings()
	case "tab", "right", "l":
		qs.group = (g + 1) % qsFocuses
	case "shift+tab", "left", "h":
		qs.group = (g + qsFocuses - 1) % qsFocuses
	case " ", "x":
		if g == qsGL {
			qs.gl = !qs.gl
		}
	}
	if g == qsGL {
		return m, nil
	}
	switch key {
	case "up", "k":
		qs.cursor[g] = clamp(qs.cursor[g]-1, 0, len(qs.items[g])-1)
	case "down", "j":
		qs.cursor[g] = clamp(qs.cursor[g]+1, 0, len(qs.items[g])-1)
	case "pgup":
		qs.cursor[g] = clamp(qs.cursor[g]-10, 0, len(qs.items[g])-1)
	case "pgdown":
		qs.cursor[g] = clamp(qs.cursor[g]+10, 0, len(qs.items[g])-1)
	}
	return m, nil
}

// picked is the value chosen in group g ("" for auto).
func (qs quickSettings) picked(g int) string {
	return qs.items[g][qs.cursor[g]].value
}

// saveQuickSettings closes the dialog, writing whatever changed to the .conf:
// a picked value is set, and auto removes the key so quickemu sizes the VM.
func (m Model) saveQuickSettings() (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	qs := m.qs
	var set, unset []string
	for g := range qsGroups {
		v := qs.picked(g)
		switch {
		case v == qs.was[g]:
		default:
			s, u := qsEdits(g, v)
			set, unset = append(set, s...), append(unset, u...)
		}
	}
	if qs.gl != qs.glWas {
		// written out even when it matches a default, which the user could change
		set = append(set, `gl="`+onOff(qs.gl)+`"`)
	}
	if len(set)+len(unset) == 0 {
		return m, nil
	}
	if err := qemu.EditConf(qs.vm.ConfPath, set, unset); err != nil {
		m.setFlash("Saving "+qs.vm.Name()+"'s settings failed: "+firstLine(err.Error()), true)
		m.showError("Saving "+qs.vm.Name()+"'s settings failed", err.Error())
		return m, nil
	}
	m.setFlash(fmt.Sprintf("%s: CPUs %s, memory %s, display %s, gl %s (applies on next start)",
		qs.vm.Name(), or(qs.picked(qsCPU), "auto"), or(qs.picked(qsRAM), "auto"),
		or(qs.picked(qsDisplay), "auto"), onOff(qs.gl)), false)
	return m, m.pollNow()
}

func (m Model) viewQuickSettings() string {
	qs := m.qs
	listRows := max(3, min(12, m.height-14))
	cols := make([]string, qsGroups)
	for g := range qsGroups {
		cols[g] = strings.Join(qs.viewGroup(g, listRows), "\n")
	}
	// side by side if they fit, else the first two side by side above the
	// display, else all stacked
	gap := "    "
	groups := lipgloss.JoinHorizontal(lipgloss.Top, cols[0], gap, cols[1], gap, cols[2])
	if lipgloss.Width(groups) > m.width-8 {
		pair := lipgloss.JoinHorizontal(lipgloss.Top, cols[0], gap, cols[1])
		if lipgloss.Width(pair) > m.width-8 {
			pair = lipgloss.JoinVertical(lipgloss.Left, cols[0], "", cols[1])
		}
		groups = lipgloss.JoinVertical(lipgloss.Left, pair, "", cols[2])
	}
	return strings.Join([]string{
		titleStyle.Render("Quick settings: " + qs.vm.Name()),
		"",
		dimStyle.Render("Saved to the .conf when you close this; applies on next start."),
		"",
		groups,
		"",
		qs.viewGL(),
		"",
		dimStyle.Render("↑↓ choose • space tick • tab/←→ switch • enter/esc save and close"),
	}, "\n")
}

func (qs quickSettings) viewGL() string {
	box := "[ ]"
	if qs.gl {
		box = "[x]"
	}
	row := " " + box + " OpenGL acceleration (gl) "
	hint := qs.glHint
	if qs.gl != qs.glWas {
		hint = "changed"
	}
	if qs.group == qsGL {
		return selStyle.Render(row) + "  " + dimStyle.Render(hint)
	}
	return row + "  " + dimStyle.Render(hint)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (qs quickSettings) viewGroup(g, listRows int) []string {
	title := qsTitles[g]
	if g == qs.group {
		title = underStyle.Render(title)
	} else {
		title = dimStyle.Render(title)
	}
	width := 0
	for _, it := range qs.items[g] {
		width = max(width, lipgloss.Width(it.label))
	}
	rows := make([]string, len(qs.items[g]))
	for i, it := range qs.items[g] {
		mark := "( )"
		if i == qs.cursor[g] {
			mark = "(•)"
		}
		row := " " + mark + " " + it.label + strings.Repeat(" ", width-lipgloss.Width(it.label))
		switch {
		case i == qs.cursor[g] && g == qs.group:
			if it.hint != "" {
				row += "  " + it.hint
			}
			row = selStyle.Render(row + " ")
		case i == qs.cursor[g]:
			row = okStyle.Render(row)
			if it.hint != "" {
				row += "  " + dimStyle.Render(it.hint)
			}
		default:
			if it.hint != "" {
				row += "  " + dimStyle.Render(it.hint)
			}
		}
		rows[i] = row
	}
	return append([]string{title}, windowAround(rows, qs.cursor[g], listRows)...)
}
