package tui

import (
	"fmt"
	"time"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// clock is how progress is timed; tests replace it.
var clock = time.Now

const (
	speedWindow  = 8 * time.Second // the speed is the average over this much recent time
	speedMinSpan = 2 * time.Second // and isn't shown until there is this much to average over
)

type speedSample struct {
	at    time.Time
	bytes int64
}

// speedMeter turns a series of "this many bytes so far" into a recent speed.
type speedMeter struct {
	samples []speedSample
}

// Add records that bytes had arrived by at. A count that goes backwards means
// a new file or a restart, so the earlier samples no longer apply.
func (s *speedMeter) Add(at time.Time, bytes int64) {
	if n := len(s.samples); n > 0 && bytes < s.samples[n-1].bytes {
		s.samples = nil
	}
	s.samples = append(s.samples, speedSample{at, bytes})
	for len(s.samples) > 2 && at.Sub(s.samples[1].at) >= speedWindow {
		s.samples = s.samples[1:] // the second sample is old enough to start from
	}
}

// Rate is the bytes per second over the window, and false while there isn't
// enough to say.
func (s speedMeter) Rate() (float64, bool) {
	if len(s.samples) < 2 {
		return 0, false
	}
	first, last := s.samples[0], s.samples[len(s.samples)-1]
	span := last.at.Sub(first.at)
	if span < speedMinSpan {
		return 0, false
	}
	return float64(last.bytes-first.bytes) / span.Seconds(), true
}

// startTransfer is a download of file (or, with "", none) beginning, so that
// nothing measured about the one before applies to it.
func (st *installState) startTransfer(file string) {
	st.file = file
	st.bytes, st.total = 0, 0
	st.speed, st.pace = speedMeter{}, speedMeter{}
}

// speedText is "1.4 MiB/s, about 38m left", or "" while nothing is known.
// The time left needs to know how much there is in all: the server's own
// figure when it gave one, else worked out from the percentage (and then not
// until there is one whole percent, which is too little to go on). With
// neither, it's left out.
func (st *installState) speedText() string {
	rate, ok := st.speed.Rate()
	if !ok {
		return st.paceText()
	}
	text := qemu.HumanSize(int64(rate)) + "/s"
	total := float64(st.total)
	if total == 0 && st.percent >= 1 {
		total = float64(st.bytes) * 100 / st.percent
	}
	if remaining := total - float64(st.bytes); rate > 0 && remaining > 0 {
		text += ", about " + formatDuration(time.Duration(remaining/rate*float64(time.Second))) + " left"
	}
	return text
}

// paceText is the time left, when all there is to go on is how fast curl's
// percentage climbs: no speed, and nothing until there is one whole percent.
// What it says is about the download in progress, which for an install of
// several (quickget fetches more than the image for some) restarts with each.
func (st *installState) paceText() string {
	rate, ok := st.pace.Rate() // thousandths of a percent a second
	if !ok || rate <= 0 || st.percent < 1 || st.percent >= 100 {
		return ""
	}
	left := time.Duration((100 - st.percent) * 1000 / rate * float64(time.Second))
	return "about " + formatDuration(left) + " left"
}

// formatDuration is d as the two largest units it needs: 2d 4h, 5h 41m, 12m 05s, 45s.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch days, hours, mins, secs := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60, int(d.Seconds())%60; {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %02ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}
