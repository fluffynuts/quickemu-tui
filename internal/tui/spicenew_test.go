package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

func TestNewVMsGetSpiceOnlyWhereSpicyIsInstalledAndTheUserHasNotChosenADisplay(t *testing.T) {
	for name, tc := range map[string]struct {
		spicy    bool
		defaults []string
		want     []string
	}{
		"spicy installed":        {true, nil, []string{`display="spice"`}},
		"no spicy":               {false, nil, nil},
		"the user's own display": {true, []string{`display="sdl"`}, nil},
		"an unrelated default":   {true, []string{`ram="8G"`}, []string{`display="spice"`}},
	} {
		t.Run(name, func(t *testing.T) {
			m := newInstallModel(t)
			withSpicy(t, tc.spicy)
			m.defaults = tc.defaults
			got := m.spiceChoices()
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCustomISOGetsASpiceDisplayWhenSpicyIsInstalled(t *testing.T) {
	for _, spicy := range []bool{true, false} {
		m := newInstallModel(t)
		withSpicy(t, spicy)
		src := filepath.Join(t.TempDir(), "d.iso")
		writeISO(t, src)
		m = customAtSettings(t, m, src, "")
		m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
		if got := strings.Contains(m.confirmText, "SPICE display"); got != spicy {
			t.Errorf("spicy=%v: the confirmation mentions SPICE: %v\n%s", spicy, got, m.confirmText)
		}
		m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		m = finishInstall(t, m, cmd)

		conf, err := qemu.VM{ConfPath: filepath.Join(m.opts.Root, "d.conf")}.Conf()
		if err != nil {
			t.Fatal(err)
		}
		if got := conf["display"] == "spice"; got != spicy {
			t.Errorf("spicy=%v: display = %q", spicy, conf["display"])
		}
	}
}

// installHaiku runs a quickget install, with a quickget that just writes a .conf.
func installHaiku(t *testing.T, m Model) Model {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'guest_os=\"haiku\"' > \"$1-$2.conf\"\n"
	must(t, os.WriteFile(filepath.Join(dir, "quickget"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = typed(m, "haiku")
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter) // the system, then auto CPUs and memory
	if m.mode != modeConfirm {
		t.Fatalf("mode %v, want the confirmation", m.mode)
	}
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	return finishInstall(t, m, cmd)
}

func TestAQuickgetInstallGetsASpiceDisplayWhenSpicyIsInstalled(t *testing.T) {
	m := newInstallModel(t)
	withSpicy(t, true)
	m = installHaiku(t, m)
	conf, err := qemu.VM{ConfPath: filepath.Join(m.opts.Root, "haiku-r1.conf")}.Conf()
	if err != nil {
		t.Fatal(err)
	}
	if conf["display"] != "spice" {
		t.Errorf("display = %q, want spice", conf["display"])
	}
	if !strings.Contains(m.flash, `display="spice"`) {
		t.Errorf("the finished message doesn't say it was added: %q", m.flash)
	}
}

func TestAQuickgetInstallKeepsTheUsersOwnDisplayDefault(t *testing.T) {
	m := newInstallModel(t)
	withSpicy(t, true)
	m.defaults = []string{`display="sdl"`}
	m = installHaiku(t, m)
	conf, _ := qemu.VM{ConfPath: filepath.Join(m.opts.Root, "haiku-r1.conf")}.Conf()
	if conf["display"] != "sdl" {
		t.Errorf("display = %q, want the user's sdl", conf["display"])
	}
}
