package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

var t0 = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func TestSpeedIsNotShownUntilThereIsEnoughToAverage(t *testing.T) {
	var s speedMeter
	if _, ok := s.Rate(); ok {
		t.Error("a speed from nothing")
	}
	s.Add(t0, 0)
	s.Add(t0.Add(time.Second), 1<<20)
	if _, ok := s.Rate(); ok {
		t.Error("a speed from one second of data")
	}
	s.Add(t0.Add(2*time.Second), 2<<20)
	if rate, ok := s.Rate(); !ok || rate != 1<<20 {
		t.Errorf("rate %v, %v; want 1 MiB/s", rate, ok)
	}
}

func TestSpeedFollowsTheRecentPastNotTheWholeDownload(t *testing.T) {
	var s speedMeter
	// 10 MiB/s for 20 s, then it slows to 1 MiB/s
	for i := 0; i <= 20; i++ {
		s.Add(t0.Add(time.Duration(i)*time.Second), int64(i)*10<<20)
	}
	for i := 1; i <= 20; i++ {
		s.Add(t0.Add(time.Duration(20+i)*time.Second), 200<<20+int64(i)<<20)
	}
	rate, ok := s.Rate()
	if !ok || rate > 2<<20 {
		t.Errorf("rate %.1f MiB/s, want about 1 once the slowdown has filled the window", rate/(1<<20))
	}
}

func TestSpeedShowsAStallAsZero(t *testing.T) {
	var s speedMeter
	for i := 0; i <= 12; i++ {
		s.Add(t0.Add(time.Duration(i)*time.Second), 5<<20) // the same size, every second
	}
	if rate, ok := s.Rate(); !ok || rate != 0 {
		t.Errorf("rate %v, %v; want a measured zero", rate, ok)
	}
}

func TestSpeedRestartsWhenTheCountGoesBackwards(t *testing.T) {
	var s speedMeter
	s.Add(t0, 100<<20)
	s.Add(t0.Add(3*time.Second), 103<<20)
	s.Add(t0.Add(4*time.Second), 1<<20) // a new file
	if _, ok := s.Rate(); ok {
		t.Error("a speed carried across a restart")
	}
}

func TestTimeLeftComesFromTheSpeedAndHowFarItIs(t *testing.T) {
	st := &installState{percent: 25, bytes: 250 << 20}
	st.speed.Add(t0, 0)
	st.speed.Add(t0.Add(10*time.Second), 10<<20) // 1 MiB/s
	// 250 MiB is a quarter, so 750 MiB is left: 12m 30s at 1 MiB/s
	if got := st.speedText(); got != "1.0 MiB/s, about 12m 30s left" {
		t.Errorf("got %q", got)
	}
	st.percent = 0.4 // too little progress to extrapolate from
	if got := st.speedText(); got != "1.0 MiB/s" {
		t.Errorf("got %q", got)
	}
	st.percent = -1 // no percentage reported
	if got := st.speedText(); got != "1.0 MiB/s" {
		t.Errorf("got %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		45 * time.Second:                                       "45s",
		12*time.Minute + 5*time.Second:                         "12m 05s",
		5*time.Hour + 41*time.Minute:                           "5h 41m",
		50*time.Hour + 20*time.Minute:                          "2d 2h",
		12*time.Minute + 29*time.Second + 600*time.Millisecond: "12m 30s",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTheProgressDialogAndHeaderShowTheSpeed(t *testing.T) {
	now := t0
	old := clock
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = old })

	m := newInstallModel(t)
	m.install = &installState{title: "mydistro", percent: 10, started: t0, msgs: make(chan tea.Msg, 1)}
	m.mode = modeInstallProgress
	for i := 0; i <= 4; i++ {
		now = t0.Add(time.Duration(i) * time.Second)
		next, _ := m.onInstallProgress(installProgressMsg(qemu.InstallProgress{Percent: 10, Bytes: int64(i+1) << 20}))
		m = next.(Model)
	}
	for name, view := range map[string]string{"dialog": ansi.Strip(m.viewInstallProgress()), "header": ansi.Strip(m.installHeader())} {
		if !strings.Contains(view, "1.0 MiB/s") {
			t.Errorf("the %s doesn't show the speed:\n%s", name, view)
		}
	}
	if view := ansi.Strip(m.viewInstallProgress()); !strings.Contains(view, "left") {
		t.Errorf("no time left in the dialog:\n%s", view)
	}
}

func TestAnInstallThatReportsNoBytesShowsNoSpeed(t *testing.T) {
	m := newInstallModel(t)
	m.install = &installState{title: "ubuntu", percent: 50, started: t0, msgs: make(chan tea.Msg, 1)}
	next, _ := m.onInstallProgress(installProgressMsg(qemu.InstallProgress{Percent: 60}))
	m = next.(Model)
	if view := ansi.Strip(m.viewInstallProgress()); strings.Contains(view, "/s") {
		t.Errorf("a speed with nothing to base it on:\n%s", view)
	}
}

// progressAt feeds one progress report to the install dialog's model, at the given moment.
func progressAt(t *testing.T, m Model, at time.Time, p qemu.InstallProgress) Model {
	t.Helper()
	old := clock
	clock = func() time.Time { return at }
	defer func() { clock = old }()
	next, _ := m.onInstallProgress(installProgressMsg(p))
	return next.(Model)
}

func installDialogModel(t *testing.T) Model {
	t.Helper()
	m := newInstallModel(t)
	m.install = &installState{title: "ubuntu", percent: -1, started: t0, msgs: make(chan tea.Msg, 1)}
	m.mode = modeInstallProgress
	return m
}

func TestWithOnlyAPercentageTheTimeLeftComesFromHowFastItClimbs(t *testing.T) {
	m := installDialogModel(t)
	for i := 0; i <= 10; i++ { // 1% a second, from 10% to 20%
		m = progressAt(t, m, t0.Add(time.Duration(i)*time.Second), qemu.InstallProgress{Percent: 10 + float64(i)})
	}
	if got := m.install.speedText(); got != "about 1m 20s left" {
		t.Errorf("got %q, want the 80%% left at 1%% a second, and no speed", got)
	}
	if view := ansi.Strip(m.viewInstallProgress()); !strings.Contains(view, "about 1m 20s left") || strings.Contains(view, "/s") {
		t.Errorf("the dialog:\n%s", view)
	}
}

func TestThePercentageTimeLeftWaitsForOnePercentAndForSomethingToAverage(t *testing.T) {
	m := installDialogModel(t)
	m = progressAt(t, m, t0, qemu.InstallProgress{Percent: 0.1})
	m = progressAt(t, m, t0.Add(10*time.Second), qemu.InstallProgress{Percent: 0.6})
	if got := m.install.speedText(); got != "" {
		t.Errorf("got %q under one percent", got)
	}
	m = progressAt(t, m, t0.Add(20*time.Second), qemu.InstallProgress{Percent: 1.1})
	if got := m.install.speedText(); got == "" {
		t.Error("nothing shown at one percent with 20 seconds of data")
	}
	var fresh = installDialogModel(t)
	fresh = progressAt(t, fresh, t0, qemu.InstallProgress{Percent: 40})
	if got := fresh.install.speedText(); got != "" {
		t.Errorf("got %q from a single sample", got)
	}
}

func TestACompletePercentageFromCurlIsIgnoredWhileDownloading(t *testing.T) {
	m := installDialogModel(t)
	m = progressAt(t, m, t0, qemu.InstallProgress{Percent: 40})
	m = progressAt(t, m, t0.Add(time.Second), qemu.InstallProgress{Percent: 100}) // what curl opens a resumed download with
	if m.install.percent != 40 {
		t.Errorf("percent %v, want 40 to stand", m.install.percent)
	}
}

func TestANewDownloadStartsMeasuringAgainAndTheEndOfOneClearsIt(t *testing.T) {
	m := installDialogModel(t)
	// the first file: 100 MiB of 1000 at 1 MiB/s
	for i := 0; i <= 5; i++ {
		m = progressAt(t, m, t0.Add(time.Duration(i)*time.Second), qemu.InstallProgress{Percent: -1, File: "/vm/a.iso", Bytes: int64(95+i) << 20, Total: 1000 << 20})
	}
	if got := m.install.speedText(); !strings.HasPrefix(got, "1.0 MiB/s") {
		t.Fatalf("got %q", got)
	}
	// the next is a different file: nothing about the last one carries over
	m = progressAt(t, m, t0.Add(6*time.Second), qemu.InstallProgress{Percent: -1, File: "/vm/b.msi", Bytes: 1 << 20})
	if got := m.install.speedText(); got != "" {
		t.Errorf("got %q for a download only just begun", got)
	}
	if m.install.total != 0 || m.install.file != "/vm/b.msi" {
		t.Errorf("total %d file %q: the last file's total was kept", m.install.total, m.install.file)
	}
	// and when it finishes there's no download to speak of
	for i := 7; i <= 12; i++ {
		m = progressAt(t, m, t0.Add(time.Duration(i)*time.Second), qemu.InstallProgress{Percent: -1, File: "/vm/b.msi", Bytes: int64(i) << 20, Total: 20 << 20})
	}
	if got := m.install.speedText(); got == "" {
		t.Fatal("no speed while it downloads")
	}
	m = progressAt(t, m, t0.Add(13*time.Second), qemu.InstallProgress{Percent: -1, Idle: true})
	if got := m.install.speedText(); got != "" || m.install.percent != -1 {
		t.Errorf("speed %q percent %v after the download ended, want neither", got, m.install.percent)
	}
}
