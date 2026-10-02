package main

import (
	"os"
	"path/filepath"
	"testing"
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
