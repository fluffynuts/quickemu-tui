// Package config persists quickemu-tui's user settings.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is the on-disk settings file.
type Config struct {
	// VMDir is the directory holding quickemu *.conf files.
	VMDir string `json:"vm_dir,omitempty"`

	// DefaultConf are `key="value"` lines merged into new VMs' .conf files, and
	// into existing ones (for keys they don't set) when the VM is started.
	DefaultConf []string `json:"default_conf,omitempty"`
}

// Path returns the config file location (<user config dir>/quickemu-tui/config.json).
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "quickemu-tui", "config.json"), nil
}

// Load reads the config at path. A missing file yields (nil, nil), meaning
// this is a first run.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config to path, creating parent directories.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Update loads the config (an empty one on first run), applies fn and saves,
// so changing one setting never drops the others.
func Update(path string, fn func(*Config)) error {
	c, err := Load(path)
	if err != nil {
		return err
	}
	if c == nil {
		c = &Config{}
	}
	fn(c)
	return Save(path, *c)
}

// ExpandHome expands a leading "~" or "~/" to the user's home directory.
func ExpandHome(p string) string {
	if p != "~" && len(p) > 1 && p[:2] != "~/" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}
