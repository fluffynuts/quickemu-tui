package config

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingIsFirstRun(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope", "config.json"))
	if err != nil || c != nil {
		t.Fatalf("got %v, %v; want nil, nil", c, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	if err := Save(p, Config{VMDir: "/vms"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || c == nil || c.VMDir != "/vms" {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	for in, want := range map[string]string{
		"~": "/home/u", "~/vms": "/home/u/vms", "/abs": "/abs", "~other": "~other",
	} {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}
