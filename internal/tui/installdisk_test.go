package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// toDiskStep walks a non-custom install (windows 11, French) to just past memory.
func toDiskStep(t *testing.T) Model {
	t.Helper()
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = typed(m, "win")
	m, _ = keyOf(m, enter) // windows → editions
	m, _ = keyOf(m, enter) // first edition → CPUs
	m, _ = keyOf(m, enter) // auto CPUs → memory
	m, _ = keyOf(m, enter) // auto memory → disk
	if m.instStep != stepDisk {
		t.Fatalf("step = %v, want the disk step", m.instStep)
	}
	return m
}

func TestDiskStepOffersTheSystemsOwnSizeThenTheUsualChoices(t *testing.T) {
	m := toDiskStep(t)
	var labels []string
	for _, it := range m.instItems() {
		labels = append(labels, it.label)
	}
	if got := strings.Join(labels, ","); got != "default,32G,64G,128G,256G,512G" {
		t.Fatalf("disk choices = %s", got)
	}
	v := m.viewInstallPick()
	if !strings.Contains(v, "choose disk size") || !strings.Contains(v, "default") {
		t.Errorf("disk step view:\n%s", v)
	}
}

func TestDiskStepIgnoresTypingLikeTheOtherSizeSteps(t *testing.T) {
	m := typed(toDiskStep(t), "6")
	if len(m.instItems()) != 6 {
		t.Fatal("typing filtered the disk list")
	}
}

func TestDiskStepShowsYourDefault(t *testing.T) {
	m := newInstallModel(t)
	m.defaults = []string{`disk_size="100G"`}
	m.instStep = stepDisk
	if h := m.instItems()[0].hint; h != `your default: disk_size="100G"` {
		t.Fatalf("default row hint = %q", h)
	}
}

func TestPickingADiskSizeGoesIntoTheConfirmationAndTheConfLines(t *testing.T) {
	m := toDiskStep(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = keyOf(m, enter) // 64G
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "disk: 64G") {
		t.Fatalf("mode=%v confirm=%q", m.mode, m.confirmText)
	}
	if got := strings.Join(m.sizeChoices(), " "); got != `disk_size="64G"` {
		t.Fatalf("choices = %s", got)
	}
}

func TestTheDefaultDiskSizeWritesNothingSoQuickgetsStands(t *testing.T) {
	m := toDiskStep(t)
	m, _ = keyOf(m, enter)
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "disk: default") {
		t.Fatalf("mode=%v confirm=%q", m.mode, m.confirmText)
	}
	for _, l := range m.sizeChoices() {
		if strings.HasPrefix(l, "disk_size") {
			t.Fatalf("the default wrote %s", l)
		}
	}
}

func TestEscFromTheDiskStepGoesBackToMemory(t *testing.T) {
	m, _ := keyOf(toDiskStep(t), tea.KeyMsg{Type: tea.KeyEsc})
	if m.instStep != stepRAM {
		t.Fatalf("step = %v, want memory", m.instStep)
	}
}

func TestANewInstallForgetsTheLastDiskChoice(t *testing.T) {
	m := toDiskStep(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = keyOf(m, enter) // 32G
	m.mode = modeNormal
	m.openInstall()
	if m.instDisk != "" {
		t.Fatalf("disk choice survived into a new install: %q", m.instDisk)
	}
}

// A custom ISO already chose its disk size on its own settings screen.
func TestCustomInstallsSkipTheDiskStep(t *testing.T) {
	m := newInstallModel(t)
	m.custom.active = true
	m.instStep = stepRAM
	m.custom.iso.Name = "slothos"
	next, _ := m.pickItem(pickItem{value: ""})
	if next.(Model).instStep == stepDisk {
		t.Fatal("a custom ISO was asked for its disk size twice")
	}
}
