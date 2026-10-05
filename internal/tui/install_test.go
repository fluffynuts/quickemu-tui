package tui

import (
	"errors"
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
	fakeHost(t, 16, 32<<30)
	return m
}

// fakeHost makes the CPU and memory steps see a machine with cpus logical
// CPUs and ram bytes of memory.
func fakeHost(t *testing.T, cpus int, ram int64) {
	t.Helper()
	oldCPUs, oldRAM := hostCPUs, hostRAM
	hostCPUs = func() int { return cpus }
	hostRAM = func() (int64, error) { return ram, nil }
	t.Cleanup(func() { hostCPUs, hostRAM = oldCPUs, oldRAM })
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
	if m.instStep != stepCPU || m.instEdition != "French" {
		t.Fatalf("after the edition: step=%v edition=%q, want the CPU step", m.instStep, m.instEdition)
	}
	m, _ = keyOf(m, enter) // auto CPUs
	m, _ = keyOf(m, enter) // auto memory
	if m.mode != modeConfirm || m.confirmDefault != defaultYes || !strings.Contains(m.confirmText, "Windows 11 French") {
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
	m, _ = keyOf(m, enter) // single release, no editions -> straight to CPUs
	if m.instStep != stepCPU {
		t.Fatalf("step=%v, want CPUs", m.instStep)
	}
	m, _ = keyOf(m, enter) // auto CPUs
	m, _ = keyOf(m, enter) // auto memory
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
	cmd := m.startInstall("Haiku", "haiku", "r1", "", nil)
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
	cmd := m.startInstall("Haiku", "haiku", "r1", "", nil)
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
	cmd := m.startInstall("Haiku", "haiku", "r1", "", nil)
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	if m.mode != modeError || !m.flashErr || strings.Contains(m.flash, "installed ✓") {
		t.Fatalf("mode=%v flash=%q", m.mode, m.flash)
	}
}

func TestCatalogUsesCacheThenFreshAndSurvivesFailure(t *testing.T) {
	cached, _ := qemu.ParseCatalog([]byte("Display Name,OS,Release,Option\nOld,old,1,\n"))
	fresh, _ := qemu.ParseCatalog([]byte(testCSV))

	m := New(Options{Root: t.TempDir()})
	m.width, m.height = 100, 40
	if !m.catalogLoading || len(m.catalog) != 0 {
		t.Fatal("expected to start loading with no catalog")
	}

	// 1. the cache lands first: usable immediately, still refreshing
	next, _ := m.Update(catalogMsg{catalog: cached, cached: true})
	m = next.(Model)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.mode != modeInstallPick || len(m.instItems()) != 1 || m.instItems()[0].value != "old" {
		t.Fatalf("cached list not offered: mode=%v items=%+v", m.mode, m.instItems())
	}

	// 2. the fresh list replaces it while the picker is open
	next, _ = m.Update(catalogMsg{catalog: fresh})
	m = next.(Model)
	if m.catalogLoading || len(m.instItems()) != 3 {
		t.Fatalf("fresh list not applied: loading=%v items=%d", m.catalogLoading, len(m.instItems()))
	}

	// 3. a later failed refresh is silent when there is a list to use
	m.catalogLoading = true
	next, _ = m.Update(catalogMsg{err: errors.New("network down")})
	m = next.(Model)
	if m.mode != modeInstallPick || len(m.catalog) == 0 || m.catalogErr == nil {
		t.Fatalf("stale list should survive a failed refresh: mode=%v err=%v", m.mode, m.catalogErr)
	}
}

func TestPickerWaitsForCatalogAndReportsFailureOnlyIfNothingToShow(t *testing.T) {
	m := New(Options{Root: t.TempDir()}) // still loading, nothing cached
	m.width, m.height = 100, 40
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.mode != modeInstallPick || cmd != nil {
		t.Fatalf("should wait on the startup fetch, not start another: mode=%v cmd=%v", m.mode, cmd != nil)
	}
	if v := m.viewInstallPick(); !strings.Contains(v, "asking quickget") {
		t.Errorf("no waiting message:\n%s", v)
	}
	// enter while waiting must not crash or advance
	m, _ = keyOf(m, enter)
	if m.instStep != stepOS {
		t.Errorf("advanced with no catalog: %v", m.instStep)
	}
	next, _ := m.Update(catalogMsg{err: errors.New("boom")})
	m = next.(Model)
	if m.mode != modeError || !strings.Contains(m.errBody, "boom") {
		t.Fatalf("waiting user not told: mode=%v", m.mode)
	}

	// pressing n again retries, since the last attempt failed
	m.mode = modeNormal
	m, cmd = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil || !m.catalogLoading {
		t.Error("expected a retry after a failure")
	}
}

func TestStartupFetchPopulatesCacheAndModel(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "quickget", "cat <<'EOF'\n"+testCSV+"EOF\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cache := filepath.Join(t.TempDir(), "catalog.csv")

	m := New(Options{Root: t.TempDir(), CachePath: cache})
	msg := fetchCatalogCmd("", cache)()
	next, _ := m.Update(msg)
	m = next.(Model)
	if m.catalogLoading || len(m.catalog.OSes()) != 3 {
		t.Fatalf("loading=%v OSes=%d", m.catalogLoading, len(m.catalog.OSes()))
	}
	if c, err := qemu.ReadCatalogCache(cache); err != nil || len(c.OSes()) != 3 {
		t.Fatalf("cache not written: %v", err)
	}

	// next launch: the cache alone makes the list available before quickget answers
	m2 := New(Options{Root: t.TempDir(), CachePath: cache})
	next, _ = m2.Update(readCatalogCacheCmd(cache)())
	if got := len(next.(Model).catalog.OSes()); got != 3 {
		t.Fatalf("cache not used: %d OSes", got)
	}
}

// quickgetMaking returns a fake quickget that creates a VM whose ISO is made by isoCmd.
func quickgetMaking(t *testing.T, isoCmd string) {
	t.Helper()
	dir := t.TempDir()
	writeScript(t, dir, "quickget", `mkdir -p "$1-$2"
printf 'guest_os="windows"\ndisk_img="%s/disk.qcow2"\niso="%s/Win.iso"\n' "$1-$2" "$1-$2" > "$1-$2.conf"
`+isoCmd+"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runInstall(t *testing.T) Model {
	t.Helper()
	m := newInstallModel(t)
	cmd := m.startInstall("Windows", "windows", "11", "", nil)
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	return m
}

func TestInstallWarnsWhenTheISOIsReallyAWebPage(t *testing.T) {
	quickgetMaking(t, `echo '<!DOCTYPE html><html><head><title>Not found</title></head></html>' > "$1-$2/Win.iso"`)
	m := runInstall(t)

	win := filepath.Join(m.opts.Root, "windows-11", "Win.iso")
	if m.mode != modeError || !m.flashErr || strings.Contains(m.flash, "installed ✓") {
		t.Fatalf("expected a warning: mode=%v flash=%q", m.mode, m.flash)
	}
	for _, want := range []string{
		"Windows 11 needs an installation ISO",
		win, // the full path
		"HTML web page",
		"Download a genuine ISO",
		filepath.Join(m.opts.Root, "windows-11.conf"),
	} {
		if !strings.Contains(m.errTitle+"\n"+m.errBody, want) {
			t.Errorf("warning lacks %q:\n%s", want, m.errBody)
		}
	}
	if !strings.Contains(m.flash, win) {
		t.Errorf("flash should name the file too: %q", m.flash)
	}
	// the VM itself is still created and listed
	if len(m.vms) != 1 {
		t.Errorf("vms = %d", len(m.vms))
	}
}

func TestInstallWithRealISOIsAPlainSuccess(t *testing.T) {
	quickgetMaking(t, `{ head -c 32768 /dev/zero; printf '\001CD001'; head -c 4000 /dev/zero; } > "$1-$2/Win.iso"`)
	m := runInstall(t)
	if m.mode == modeError || m.flashErr || !strings.Contains(m.flash, "installed ✓") {
		t.Fatalf("a valid ISO was flagged: mode=%v flash=%q err=%q", m.mode, m.flash, m.errBody)
	}
}

func TestInstallWarnsWhenTheISOIsMissing(t *testing.T) {
	quickgetMaking(t, `true`) // conf says iso=…/Win.iso but nothing was downloaded
	m := runInstall(t)
	if m.mode != modeError || !strings.Contains(m.errBody, "does not exist") ||
		!strings.Contains(m.errBody, filepath.Join(m.opts.Root, "windows-11", "Win.iso")) {
		t.Fatalf("mode=%v body=%q", m.mode, m.errBody)
	}
}

func TestInstallCPUAndMemorySteps(t *testing.T) {
	m := newInstallModel(t) // 16 CPUs, 32 GiB
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = typed(m, "haiku")
	m, _ = keyOf(m, enter)

	cpus := m.instItems()
	if len(cpus) != 16 || cpus[0].label != "auto" || !strings.Contains(cpus[0].hint, "8 cores") || cpus[1].label != "1 core" || cpus[15].label != "15 cores" {
		t.Fatalf("CPU choices = %+v, want auto (8 cores) then 1..15", cpus)
	}
	v := m.viewInstallPick()
	for _, want := range []string{"16–31", "8   ← this host", "This host has 16 logical CPUs"} {
		if !strings.Contains(v, want) {
			t.Errorf("CPU step lacks %q:\n%s", want, v)
		}
	}
	m = typed(m, "4") // no filter here: typing does nothing
	if len(m.instItems()) != 16 {
		t.Fatal("typing filtered the CPU list")
	}
	for i := 0; i < 4; i++ {
		m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = keyOf(m, enter)
	if m.instCores != "4" || m.instStep != stepRAM {
		t.Fatalf("cores=%q step=%v", m.instCores, m.instStep)
	}

	ram := m.instItems()
	var labels []string
	for _, it := range ram {
		labels = append(labels, it.label)
	}
	if got := strings.Join(labels, ","); got != "auto,4G,8G,12G,16G" {
		t.Fatalf("memory choices = %s", got)
	}
	if !strings.Contains(ram[0].hint, "8G") || !strings.Contains(m.viewInstallPick(), "← this host") {
		t.Errorf("auto hint/table wrong: %q\n%s", ram[0].hint, m.viewInstallPick())
	}
	// esc goes back to CPUs, keeping nothing half-chosen
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.instStep != stepCPU {
		t.Fatalf("esc from memory: step=%v", m.instStep)
	}
	m, _ = keyOf(m, enter) // auto CPUs this time
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = keyOf(m, enter) // 8G
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "CPUs: auto, memory: 8G") {
		t.Fatalf("mode=%v confirm=%q", m.mode, m.confirmText)
	}
	if got := strings.Join(m.sizeChoices(), " "); got != `ram="8G"` {
		t.Fatalf("choices = %s", got)
	}
}

func TestInstallSizeStepsShowYourDefaults(t *testing.T) {
	m := newInstallModel(t)
	m.defaults = []string{`cpu_cores="6"`}
	m.instStep = stepCPU
	if h := m.cpuItems()[0].hint; h != `your default: cpu_cores="6"` {
		t.Fatalf("auto hint = %q", h)
	}
	fakeHost(t, 1, 6<<30) // tiny host: nothing but auto
	m.host = readHostInfo()
	if len(m.cpuItems()) != 1 || len(m.ramItems()) != 1 {
		t.Fatalf("cpu=%+v ram=%+v, want only auto", m.cpuItems(), m.ramItems())
	}
}

func TestInstallWritesPickedSizeOverQuickgetsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	// like quickget for ubuntu-server, which writes its own ram=
	script := "#!/bin/sh\nprintf 'guest_os=\"linux\"\nram=\"4G\"\n' > \"$1-$2.conf\"\n"
	if err := os.WriteFile(filepath.Join(dir, "quickget"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	m := newInstallModel(t)
	m.defaults = []string{`ram="2G"`, `cpu_cores="2"`, `gl="off"`}

	cmd := m.startInstall("Haiku", "haiku", "r1", "", []string{`cpu_cores="6"`, `ram="12G"`})
	for i := 0; i < 20 && m.install != nil; i++ {
		next, c := m.Update(cmd())
		m, cmd = next.(Model), c
	}
	data, err := os.ReadFile(filepath.Join(m.opts.Root, "haiku-r1.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "guest_os=\"linux\"\nram=\"12G\"\ncpu_cores=\"6\"\ngl=\"off\"\n"; got != want {
		t.Fatalf(".conf =\n%s\nwant\n%s", got, want)
	}
}
