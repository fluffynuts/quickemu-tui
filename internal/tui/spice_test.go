package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

func runningSpiceModel(t *testing.T) Model {
	t.Helper()
	m := newMouseModel(t, "alpha")
	vm := m.vms[0]
	must(t, os.MkdirAll(filepath.Join(vm.BaseDir(), vm.Name()), 0o755))
	m.infos[vm.ConfPath] = vmInfo{
		status: qemu.Status{State: qemu.Running},
		conf:   map[string]string{"display": "spice"},
		spice:  qemu.SpiceTarget{Socket: "/q/alpha/alpha.sock"},
	}
	return m
}

func fakeViewer(t *testing.T, name string) (argsFile string) {
	t.Helper()
	bin := t.TempDir()
	argsFile = filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	must(t, os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755))
	t.Setenv("PATH", bin)
	return argsFile
}

func TestStartOnARunningSpiceVMOpensTheViewer(t *testing.T) {
	m := runningSpiceModel(t)
	argsFile := fakeViewer(t, "spicy")

	var label string
	for _, it := range m.menuItems() {
		if it.key == "s" {
			label = it.label
			if it.disabled {
				t.Errorf("%q is disabled", it.label)
			}
		}
	}
	if !strings.Contains(label, "SPICE") {
		t.Errorf("start item is %q, want it to say it opens the SPICE viewer", label)
	}

	m.openMenu()
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatalf("no viewer launched; flash %q", m.flash)
	}
	// waits for the fake viewer to exit
	if exited, _ := cmd().(viewerExitedMsg); exited.err != nil {
		t.Errorf("viewer reported %v", exited.err)
	}
	args, _ := os.ReadFile(argsFile)
	if got := strings.TrimSpace(string(args)); got != "--uri=spice+unix:///q/alpha/alpha.sock" {
		t.Errorf("viewer got %q", got)
	}
}

func TestStartOnARunningVMWithoutSpiceStillSaysItIsRunning(t *testing.T) {
	m := runningSpiceModel(t)
	vm := m.vms[0]
	info := m.infos[vm.ConfPath]
	info.spice = qemu.SpiceTarget{}
	m.infos[vm.ConfPath] = info
	m.openMenu()
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil || !strings.Contains(m.flash, "already running") {
		t.Errorf("cmd %v, flash %q", cmd, m.flash)
	}
}

func TestMissingViewerIsReported(t *testing.T) {
	m := runningSpiceModel(t)
	t.Setenv("PATH", t.TempDir())
	m.openMenu()
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil || !strings.Contains(m.flash, "no SPICE viewer") {
		t.Errorf("cmd %v, flash %q", cmd, m.flash)
	}
}

func TestQuickSettingsSpiceCheckboxWritesAndRemovesDisplay(t *testing.T) {
	for name, tc := range map[string]struct{ conf, want string }{
		"ticking sets it":          {"a=1\n", "a=1\ndisplay=\"spice\"\n"},
		"ticking replaces another": {"display=\"sdl\"\n", "display=\"spice\"\n"},
		"unticking removes it":     {"display=\"spice\"\na=1\n", "a=1\n"},
	} {
		t.Run(name, func(t *testing.T) {
			m, path := newQuickSettingsModel(t, tc.conf)
			m.openMenu()
			m = typed(m, "Q")
			wasSpice := strings.Contains(tc.conf, `"spice"`)
			if m.qs.spice != wasSpice {
				t.Fatalf("opened with spice=%v, want %v", m.qs.spice, wasSpice)
			}
			m.qs.group = qsSpice
			m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeySpace})
			m, _ = keyOf(m, enter)
			if data, _ := os.ReadFile(path); string(data) != tc.want {
				t.Errorf(".conf is %q, want %q", data, tc.want)
			}
		})
	}
}

func TestAViewerThatFailsShowsItsOwnOutputNotEarlierRuns(t *testing.T) {
	m := runningSpiceModel(t)
	vm := m.vms[0]
	p, err := vm.Paths()
	must(t, err)
	must(t, os.WriteFile(p.ViewerLog, []byte("an earlier run's complaint\n"), 0o644))
	bin := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bin, "spicy"), []byte("#!/bin/sh\necho 'cannot connect to the socket' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", bin)

	m.openMenu()
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatalf("no viewer launched; flash %q", m.flash)
	}
	exited, ok := cmd().(viewerExitedMsg)
	if !ok || exited.err == nil {
		t.Fatalf("got %+v, want a failed viewer", exited)
	}
	next, _ := m.Update(exited)
	m = next.(Model)
	if !strings.Contains(m.flash, "cannot connect to the socket") || !m.flashErr {
		t.Errorf("flash %q (error: %v)", m.flash, m.flashErr)
	}
	if m.mode != modeError {
		t.Errorf("mode %v, want the error dialog", m.mode)
	}
	if got := ansi.Strip(m.View()); strings.Contains(got, "earlier run") {
		t.Errorf("the dialog shows an earlier run's output:\n%s", got)
	}
	if log, _ := os.ReadFile(p.ViewerLog); !strings.Contains(string(log), "earlier run") {
		t.Errorf("the log was truncated: %q", log)
	}
}

func TestAViewerClosedCleanlyShowsNothing(t *testing.T) {
	m := runningSpiceModel(t)
	next, _ := m.Update(viewerExitedMsg{vm: m.vms[0], viewer: "spicy"})
	m = next.(Model)
	if m.flash != "" || m.mode != modeNormal {
		t.Errorf("flash %q, mode %v", m.flash, m.mode)
	}
}

func TestQuickSettingsSpiceCheckboxIsReachedByTabAndByClick(t *testing.T) {
	m, _ := newQuickSettingsModel(t, "a=1\n")
	m.openMenu()
	m = typed(m, "Q")
	for range qsSpice {
		m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyTab})
	}
	if m.qs.group != qsSpice {
		t.Fatalf("group %d after tabbing, want the SPICE checkbox", m.qs.group)
	}
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown}) // no list to move in
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.qs.spice {
		t.Fatal("space didn't tick it")
	}

	m = newMouseModel(t, "alpha")
	m.openQuickSettings(m.vms[0])
	m = click(t, m, "SPICE display", anywhere, tea.MouseButtonLeft)
	if m.qs.group != qsSpice || !m.qs.spice {
		t.Fatalf("group %d, spice %v; want the checkbox ticked", m.qs.group, m.qs.spice)
	}
	m = click(t, m, "OpenGL", anywhere, tea.MouseButtonLeft)
	if m.qs.group != qsGL || !m.qs.spice {
		t.Fatalf("clicking OpenGL: group %d, spice %v", m.qs.group, m.qs.spice)
	}
	m = click(t, m, buttonText(qsButton), anywhere, tea.MouseButtonLeft)
	if m.mode != modeNormal {
		t.Fatalf("mode %v after clicking %s, want closed", m.mode, qsButton)
	}
	if data, _ := os.ReadFile(m.vms[0].ConfPath); !strings.Contains(string(data), `display="spice"`) {
		t.Errorf(".conf lacks display=spice:\n%s", data)
	}
	if !strings.Contains(m.flash, "spice on") {
		t.Errorf("flash %q doesn't report spice", m.flash)
	}
}

func TestSpiceHintOnlyWhereItApplies(t *testing.T) {
	for name, tc := range map[string]struct {
		conf     string
		press    bool
		wantHint string
	}{
		"unticked":        {"a=1\n", false, ""},
		"ticked in .conf": {"display=\"spice\"\n", false, "reopen the viewer"},
		"just ticked":     {"a=1\n", true, "changed"},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := newQuickSettingsModel(t, tc.conf)
			m.openMenu()
			m = typed(m, "Q")
			if tc.press {
				m.qs.group = qsSpice
				m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeySpace})
			}
			got := ansi.Strip(m.qs.viewSpice())
			if tc.wantHint == "" && strings.Contains(got, "  ") && strings.TrimSpace(strings.SplitN(got, ")", 2)[1]) != "" {
				t.Errorf("hint shown: %q", got)
			}
			if tc.wantHint != "" && !strings.Contains(got, tc.wantHint) {
				t.Errorf("view %q lacks %q", got, tc.wantHint)
			}
		})
	}
}
