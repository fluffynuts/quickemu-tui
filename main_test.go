package main

import "testing"

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
