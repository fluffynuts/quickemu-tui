package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newQuickSettingsModel(t *testing.T, conf string) (Model, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "vm.conf")
	must(t, os.WriteFile(path, []byte(conf), 0o755))
	fakeHost(t, 8, 32<<30)
	m := New(Options{Root: root})
	m.width, m.height = 100, 40
	return m, path
}

func TestQuickSettingsIsInTheMenuAfterRemovableMedia(t *testing.T) {
	m, _ := newQuickSettingsModel(t, `guest="linux"`+"\n")
	var keys []string
	for _, it := range m.menuItems() {
		keys = append(keys, it.key)
	}
	if got := strings.Join(keys, ""); !strings.Contains(got, "mQe") {
		t.Fatalf("menu keys %q: want Q between m and e", got)
	}
}

func TestQuickSettingsWritesPicksOnClose(t *testing.T) {
	m, path := newQuickSettingsModel(t, "guest=\"linux\"\nram=\"4G\"\n")
	m.openMenu()
	m = typed(m, "Q")
	if m.mode != modeQuickSettings {
		t.Fatalf("mode %v, want quick settings", m.mode)
	}
	// opens on what the .conf says: cpu unset (auto), ram 4G
	if m.qs.picked(qsCPU) != "" || m.qs.picked(qsRAM) != "4G" {
		t.Fatalf("opened on cpu=%q ram=%q", m.qs.picked(qsCPU), m.qs.picked(qsRAM))
	}
	// same limits as a new install: 1..host-1 cores, 4G steps to about half the RAM
	if n := len(m.qs.items[qsCPU]); n != 8 { // auto + 1..7
		t.Errorf("%d CPU choices, want 8", n)
	}
	if last := m.qs.items[qsRAM][len(m.qs.items[qsRAM])-1].value; last != "16G" {
		t.Errorf("largest RAM choice %q, want 16G", last)
	}
	view := m.viewQuickSettings()
	for _, want := range []string{"CPUs", "Memory", "4 cores", "current"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}

	down := tea.KeyMsg{Type: tea.KeyDown}
	up := tea.KeyMsg{Type: tea.KeyUp}
	m, _ = keyOf(m, down)
	m, _ = keyOf(m, down) // 2 cores
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = keyOf(m, up) // 4G -> auto
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Fatalf("esc left mode %v", m.mode)
	}
	data, _ := os.ReadFile(path)
	if got, want := string(data), "guest=\"linux\"\ncpu_cores=\"2\"\n"; got != want {
		t.Errorf(".conf is %q, want %q", got, want)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o755 {
		t.Errorf("mode changed to %v", st.Mode().Perm())
	}
}

func TestQuickSettingsKeepsHandSetValuesAndLeavesUnchangedConfAlone(t *testing.T) {
	conf := "guest=\"linux\"\ncpu_cores=\"12\"\nram=\"6G\"   # tuned\n"
	m, path := newQuickSettingsModel(t, conf)
	m.openMenu()
	m = typed(m, "Q")
	if m.qs.picked(qsCPU) != "12" || m.qs.picked(qsRAM) != "6G" {
		t.Fatalf("hand-set values not offered: cpu=%q ram=%q", m.qs.picked(qsCPU), m.qs.picked(qsRAM))
	}
	m, _ = keyOf(m, enter)
	if data, _ := os.ReadFile(path); string(data) != conf {
		t.Errorf(".conf rewritten without changes: %q", data)
	}
}

func TestQuickSettingsGLCheckbox(t *testing.T) {
	tab := tea.KeyMsg{Type: tea.KeyTab}
	space := tea.KeyMsg{Type: tea.KeySpace}
	for name, tc := range map[string]struct {
		conf, defaults string
		ticked         bool
		want           string // the .conf after unticking/ticking
	}{
		"unset is quickemu's on":     {"a=1\n", "", true, "a=1\ngl=\"off\"\n"},
		"set off":                    {"gl=\"off\"\n", "", false, "gl=\"on\"\n"},
		"unset follows your default": {"a=1\n", `gl="off"`, false, "a=1\ngl=\"on\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			m, path := newQuickSettingsModel(t, tc.conf)
			if tc.defaults != "" {
				m.defaults = []string{tc.defaults}
			}
			m.openMenu()
			m = typed(m, "Q")
			if m.qs.gl != tc.ticked {
				t.Fatalf("gl ticked=%v, want %v", m.qs.gl, tc.ticked)
			}
			m, _ = keyOf(m, tab)
			m, _ = keyOf(m, tab)                           // past CPUs and memory to the checkbox
			m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown}) // no list to move in
			m, _ = keyOf(m, space)
			if !strings.Contains(m.viewQuickSettings(), "OpenGL") {
				t.Error("view lacks the gl checkbox")
			}
			m, _ = keyOf(m, enter)
			if data, _ := os.ReadFile(path); string(data) != tc.want {
				t.Errorf(".conf is %q, want %q", data, tc.want)
			}
		})
	}
}

func TestQuickSettingsGLTickedTwiceIsUnchanged(t *testing.T) {
	m, path := newQuickSettingsModel(t, "a=1\n")
	m.openMenu()
	m = typed(m, "Q")
	m.qs.group = qsGL
	m = typed(m, "  ")
	m, _ = keyOf(m, enter)
	if data, _ := os.ReadFile(path); string(data) != "a=1\n" {
		t.Errorf(".conf rewritten: %q", data)
	}
}
