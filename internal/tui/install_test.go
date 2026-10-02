package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

const testCSV = `Display Name,OS,Release,Option
Ubuntu,ubuntu,22.04,
Ubuntu,ubuntu,24.04,
Windows,windows,11,English
Windows,windows,11,French
Haiku,haiku,r1,
`

func newInstallModel(t *testing.T) Model {
	t.Helper()
	cat, err := qemu.ParseCatalog([]byte(testCSV))
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: t.TempDir()})
	m.width, m.height = 100, 40
	m.catalog = cat
	return m
}

func keyOf(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	n, c := m.Update(k)
	return n.(Model), c
}

func typed(m Model, s string) Model {
	for _, r := range s {
		m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

var enter = tea.KeyMsg{Type: tea.KeyEnter}

func TestInstallPickerWalksOSReleaseEdition(t *testing.T) {
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.mode != modeInstallPick || m.instStep != stepOS {
		t.Fatalf("mode=%v step=%v", m.mode, m.instStep)
	}
	// filter narrows the list; typing letters must not trigger hotkeys
	m = typed(m, "win")
	if items := m.instItems(); len(items) != 1 || items[0].value != "windows" {
		t.Fatalf("filter 'win' -> %+v", items)
	}
	m, _ = keyOf(m, enter)
	// windows has a single release, so that step is skipped straight to editions
	if m.instStep != stepEdition || m.instOS != "windows" || m.instRelease != "11" {
		t.Fatalf("step=%v os=%q release=%q", m.instStep, m.instOS, m.instRelease)
	}
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = keyOf(m, enter) // French
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "Windows 11 French") {
		t.Fatalf("mode=%v confirm=%q", m.mode, m.confirmText)
	}
}

func TestInstallPickerEscGoesBackThenCloses(t *testing.T) {
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = typed(m, "ubuntu")
	m, _ = keyOf(m, enter)
	if m.instStep != stepRelease {
		t.Fatalf("step=%v", m.instStep)
	}
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.instStep != stepOS || m.mode != modeInstallPick {
		t.Fatalf("esc should go back to OS: step=%v mode=%v", m.instStep, m.mode)
	}
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Fatalf("esc at the first step should close: mode=%v", m.mode)
	}
}

func TestInstallShowsProgressAndFinishes(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'Downloading Haiku r1'\nprintf '## 25.0%%\\r#### 75.0%%\\r###### 100.0%%\\n' >&2\necho ok > \"$1-$2.conf\"\n"
	if err := os.WriteFile(filepath.Join(dir, "quickget"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = typed(m, "haiku")
	m, _ = keyOf(m, enter) // single release -> straight to the confirm
	if m.mode != modeConfirm {
		t.Fatalf("mode=%v, want confirm", m.mode)
	}
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.install == nil || m.mode != modeInstallProgress {
		t.Fatalf("install=%v mode=%v", m.install, m.mode)
	}

	var seen []float64
	for i := 0; i < 50 && m.install != nil; i++ {
		msg := cmd()
		if p, ok := msg.(installProgressMsg); ok && p.Percent >= 0 {
			seen = append(seen, p.Percent)
		}
		next, c := m.Update(msg)
		m, cmd = next.(Model), c
		if m.install != nil && m.install.percent == 75 {
			if v := m.viewInstallProgress(); !strings.Contains(v, " 75.0%") || !strings.Contains(v, "█") {
				t.Errorf("progress view missing bar/percent:\n%s", v)
			}
			if h := m.installHeader(); !strings.Contains(h, "75%") {
				t.Errorf("header = %q", h)
			}
		}
	}
	if m.install != nil {
		t.Fatal("install never finished")
	}
	if len(seen) == 0 || seen[len(seen)-1] != 100 {
		t.Errorf("progress seen = %v, want to end at 100", seen)
	}
	if m.mode != modeNormal || !strings.Contains(m.flash, "installed") || m.flashErr {
		t.Errorf("mode=%v flash=%q", m.mode, m.flash)
	}
	if len(m.vms) != 1 || m.vms[0].Name() != "haiku-r1" {
		t.Errorf("new VM not discovered: %+v", m.vms)
	}
}

func TestInstallFailureShowsErrorDialog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "quickget"), []byte("#!/bin/sh\necho 'ERROR! mirror is down'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newInstallModel(t)
	cmd := m.startInstall("Haiku", "haiku", "r1", "")
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	if m.mode != modeError || !strings.Contains(m.errBody, "mirror is down") {
		t.Fatalf("mode=%v err=%q", m.mode, m.errBody)
	}
}

func TestInstallCancel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "quickget"), []byte("#!/bin/sh\necho hi\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newInstallModel(t)
	cmd := m.startInstall("Haiku", "haiku", "r1", "")
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	if m.install != nil || !strings.Contains(m.flash, "cancelled") || m.mode == modeError {
		t.Fatalf("install=%v mode=%v flash=%q", m.install, m.mode, m.flash)
	}
}

func TestInstallThatExitsZeroButReportsProblemsIsFlagged(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'unzip:  cannot find zipfile directory'\necho \"Making $1-$2.conf\"\n"
	if err := os.WriteFile(filepath.Join(dir, "quickget"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newInstallModel(t)
	cmd := m.startInstall("Haiku", "haiku", "r1", "")
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	if m.mode != modeError || !m.flashErr || strings.Contains(m.flash, "installed ✓") {
		t.Fatalf("mode=%v flash=%q", m.mode, m.flash)
	}
}
