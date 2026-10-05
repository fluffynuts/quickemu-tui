package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/config"
)

func typeLines(m Model, lines ...string) Model {
	for i, l := range lines {
		if i > 0 {
			m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEnter})
		}
		m = typed(m, l)
	}
	return m
}

func TestDefaultsDialogSavesAndKeepsOtherConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cfg", "config.json")
	if err := config.Save(cfgPath, config.Config{VMDir: "/vms"}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: t.TempDir(), ConfigPath: cfgPath})
	m.width, m.height = 100, 40

	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.mode != modeDefaults {
		t.Fatalf("mode = %v, want defaults dialog", m.mode)
	}
	// typing letters (including 'n', 'q', 'd') must go into the text, not trigger hotkeys
	m = typeLines(m, `gl="off"`, `cpu_cores="4"`)
	if m.mode != modeDefaults {
		t.Fatalf("typing left the dialog: mode=%v", m.mode)
	}
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeNormal {
		t.Fatalf("save should close the dialog; err=%q", m.defErr)
	}
	c, err := config.Load(cfgPath)
	if err != nil || c == nil || c.VMDir != "/vms" || len(c.DefaultConf) != 2 || c.DefaultConf[0] != `gl="off"` {
		t.Fatalf("saved config = %+v, %v", c, err)
	}
	if len(m.defaults) != 2 {
		t.Errorf("model defaults not updated: %v", m.defaults)
	}

	// reopening shows what was saved
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if v := m.defInput.Value(); v != "gl=\"off\"\ncpu_cores=\"4\"" {
		t.Errorf("reopened with %q", v)
	}
}

func TestDefaultsDialogRejectsUnsafeLinesAndEscDiscards(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	m := New(Options{Root: t.TempDir(), ConfigPath: cfgPath})
	m.width, m.height = 100, 40
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = typeLines(m, `gl="off"; touch /tmp/pwned`)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeDefaults || m.defErr == "" {
		t.Fatalf("unsafe line accepted: mode=%v err=%q", m.mode, m.defErr)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Error("config written despite validation error")
	}
	if v := m.viewDefaults(); !strings.Contains(v, m.defErr[:10]) {
		t.Errorf("error not shown in dialog:\n%s", v)
	}
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal || len(m.defaults) != 0 {
		t.Errorf("esc should discard: mode=%v defaults=%v", m.mode, m.defaults)
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStartMergesOnlyUnsetDefaults(t *testing.T) {
	root := t.TempDir()
	conf := filepath.Join(root, "vm.conf")
	// the user explicitly turned gl on; ram is unset
	if err := os.WriteFile(conf, []byte("guest_os=\"linux\"\ngl=\"on\"\n"), 0o744); err != nil {
		t.Fatal(err)
	}
	fake := writeScript(t, t.TempDir(), "quickemu", "exit 0\n")
	m := New(Options{Root: root, Quickemu: fake, Defaults: []string{`gl="off"`, `ram="4G"`}})
	m.width, m.height = 100, 40

	m.startVM(m.vms[0])

	data, _ := os.ReadFile(conf)
	got := string(data)
	if !strings.Contains(got, `gl="on"`) || strings.Contains(got, `gl="off"`) {
		t.Errorf("explicit gl=on was overridden:\n%s", got)
	}
	if !strings.Contains(got, `ram="4G"`) {
		t.Errorf("unset default not merged:\n%s", got)
	}
	if !strings.Contains(m.flash, `ram="4G"`) || strings.Contains(m.flash, "gl=") {
		t.Errorf("flash = %q", m.flash)
	}
	if st, _ := os.Stat(conf); st.Mode().Perm() != 0o744 {
		t.Errorf("conf mode changed to %v", st.Mode().Perm())
	}
}

func TestInstallMergesDefaultsIntoNewConf(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "quickget", "printf 'guest_os=\"linux\"\\n' > \"$1-$2.conf\"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := newInstallModel(t)
	m.defaults = []string{`gl="off"`, `ram="4G"`}
	// a pre-existing VM must not be touched by the install
	old := filepath.Join(m.opts.Root, "old.conf")
	if err := os.WriteFile(old, []byte("guest_os=\"linux\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := m.startInstall("Haiku", "haiku", "r1", "", nil)
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	data, err := os.ReadFile(filepath.Join(m.opts.Root, "haiku-r1.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `gl="off"`) || !strings.Contains(string(data), `ram="4G"`) {
		t.Errorf("defaults missing from new conf:\n%s", data)
	}
	if !strings.Contains(m.flash, "installed") || !strings.Contains(m.flash, `gl="off"`) {
		t.Errorf("flash = %q", m.flash)
	}
	if o, _ := os.ReadFile(old); strings.Contains(string(o), "gl=") {
		t.Errorf("existing VM modified by install:\n%s", o)
	}
}
