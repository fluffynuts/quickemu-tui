package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFormatVersion(t *testing.T) {
	for _, c := range []struct{ version, commit, built, want string }{
		{"0.1.57", "c84a1bb6abd5", "2026-10-02T13:23:26Z", "quickemu-tui 0.1.57 (c84a1bb6abd5, built 2026-10-02T13:23:26Z)"},
		{"0.1.57", "c84a1bb6abd5-dirty", "2026-10-02T13:23:26Z", "quickemu-tui 0.1.57 (c84a1bb6abd5-dirty, built 2026-10-02T13:23:26Z)"},
		{"0.1.57", "c84a1bb6abd5", "", "quickemu-tui 0.1.57 (c84a1bb6abd5)"},
		{"0.1.57", "", "2026-10-02T13:23:26Z", "quickemu-tui 0.1.57 (built 2026-10-02T13:23:26Z)"},
		{"dev", "", "", "quickemu-tui dev"},
	} {
		if got := formatVersion(c.version, c.commit, c.built); got != c.want {
			t.Errorf("formatVersion(%q, %q, %q) = %q, want %q", c.version, c.commit, c.built, got, c.want)
		}
	}
}

func TestEnsureVMDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vms", "nested")
	for range 2 { // creates it, then is happy that it already exists
		if err := ensureVMDir(dir); err != nil {
			t.Fatalf("ensureVMDir(%q): %v", dir, err)
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			t.Fatalf("%q isn't a directory after ensureVMDir: %v", dir, err)
		}
	}
}

func TestEnsureVMDirReportsAFileInTheWay(t *testing.T) {
	file := filepath.Join(t.TempDir(), "vms")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureVMDir(file); err == nil {
		t.Errorf("ensureVMDir(%q) succeeded on a plain file", file)
	}
}

func TestUpdateCheckerIsWiredToTheRunningVersion(t *testing.T) {
	// a dev build has nothing to compare, so it answers without asking GitHub
	// (the unreachable API base would make any attempt fail)
	check := updateChecker("dev", "http://127.0.0.1:1", "")
	if latest, err := check(context.Background()); latest != "" || err != nil {
		t.Fatalf("a dev build checked anyway: %q, %v", latest, err)
	}
	// a release build does ask, and an unreachable API is reported
	check = updateChecker("0.1.29", "http://127.0.0.1:1", "")
	if _, err := check(context.Background()); err == nil {
		t.Fatal("a release build never asked")
	}
}

type quitModel struct{}

func (quitModel) Init() tea.Cmd                       { return tea.Quit }
func (quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return quitModel{}, nil }
func (quitModel) View() string                        { return "x" }

// terminalWrites runs a program that quits at once with the options and
// returns what it wrote to the terminal.
func terminalWrites(t *testing.T, opts []tea.ProgramOption) string {
	t.Helper()
	var out bytes.Buffer
	p := tea.NewProgram(quitModel{}, append(opts, tea.WithInput(nil), tea.WithOutput(&out))...)
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestProgramOptionsTurnTheMouseAndTheAlternateScreenOnAndOff(t *testing.T) {
	const mouseOn, altScreenOn = "\x1b[?1002h", "\x1b[?1049h" // the "l" forms are written on every exit
	for _, c := range []struct{ altScreen, mouse bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		got := terminalWrites(t, programOptions(c.altScreen, c.mouse))
		if has := strings.Contains(got, mouseOn); has != c.mouse {
			t.Errorf("altScreen=%v mouse=%v: mouse enabled=%v", c.altScreen, c.mouse, has)
		}
		if has := strings.Contains(got, altScreenOn); has != c.altScreen {
			t.Errorf("altScreen=%v mouse=%v: alternate screen=%v", c.altScreen, c.mouse, has)
		}
	}
}
