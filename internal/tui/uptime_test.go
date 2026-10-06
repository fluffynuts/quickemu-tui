package tui

import (
	"testing"
	"time"
)

func TestUptime(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "<1m"},
		{5*time.Minute + 59*time.Second, "5m"},
		{2*time.Hour + 13*time.Minute, "2h 13m"},
		{3*24*time.Hour + 4*time.Hour + 59*time.Minute, "3d 4h"},
	} {
		if got := uptime(tc.d); got != tc.want {
			t.Errorf("uptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
