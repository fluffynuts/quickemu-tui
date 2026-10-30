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
