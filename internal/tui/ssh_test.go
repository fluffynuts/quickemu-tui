package tui

import (
	"path/filepath"
	"testing"

	"github.com/fluffynuts/quickemu-tui/internal/config"
)

func TestSSHUserDefaultsToCurrentUser(t *testing.T) {
	t.Setenv("USER", "someone")
	m := New(Options{Root: t.TempDir()})
	if got := m.sshUserDefault(); got != "someone" {
		t.Errorf("default = %q, want current user", got)
	}
}

func TestRememberedSSHUserOverridesDefaultAndSurvivesRestart(t *testing.T) {
	t.Setenv("USER", "someone")
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfgPath, config.Config{VMDir: "/vms"}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Root: t.TempDir(), ConfigPath: cfgPath})

	m.rememberSSHUser("root")

	if got := m.sshUserDefault(); got != "root" {
		t.Errorf("default after use = %q, want root", got)
	}
	c, err := config.Load(cfgPath)
	if err != nil || c == nil || c.SSHUser != "root" || c.VMDir != "/vms" {
		t.Fatalf("saved config = %+v, %v", c, err)
	}
	restarted := New(Options{Root: t.TempDir(), ConfigPath: cfgPath, SSHUser: c.SSHUser})
	if got := restarted.sshUserDefault(); got != "root" {
		t.Errorf("default after restart = %q, want root", got)
	}
}
