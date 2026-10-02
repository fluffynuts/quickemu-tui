// quickemu-tui: a terminal UI for managing quickemu VMs and their snapshots.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/config"
	"github.com/fluffynuts/quickemu-tui/internal/tui"
)

// resolveDir returns the VM directory from the config file, asking the user
// (and saving the answer) on first run. It returns "" if the user cancels.
func resolveDir(def string) (string, error) {
	path, err := config.Path()
	if err != nil {
		return def, nil // no usable config location: fall back to the default
	}
	cfg, err := config.Load(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	if cfg != nil && cfg.VMDir != "" {
		return cfg.VMDir, nil
	}
	final, err := tea.NewProgram(tui.NewSetup(def)).Run()
	if err != nil {
		return "", err
	}
	setup := final.(tui.Setup)
	if setup.Cancelled() {
		return "", nil
	}
	dir := config.ExpandHome(setup.Dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := config.Update(path, func(c *config.Config) { c.VMDir = dir }); err != nil {
		return "", fmt.Errorf("saving %s: %w", path, err)
	}
	return dir, nil
}

func main() {
	home, _ := os.UserHomeDir()
	dir := flag.String("dir", filepath.Join(home, "quickemu"), "directory containing quickemu *.conf files (or pass it as the first argument)")
	quickemu := flag.String("quickemu", "", "path to quickemu (default: found on PATH)")
	flag.Parse()
	explicit := flag.NArg() > 0
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "dir" })
	if flag.NArg() > 0 {
		*dir = flag.Arg(0)
	}
	if !explicit {
		chosen, err := resolveDir(*dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "quickemu-tui:", err)
			os.Exit(1)
		}
		if chosen == "" {
			return // cancelled at the first-run dialog
		}
		*dir = chosen
	}
	*dir = config.ExpandHome(*dir)
	root, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "quickemu-tui:", err)
		os.Exit(2)
	}

	opts := tui.Options{Root: root, Quickemu: *quickemu}
	if path, err := config.Path(); err == nil {
		opts.ConfigPath = path
		if cfg, err := config.Load(path); err == nil && cfg != nil {
			opts.Defaults = cfg.DefaultConf
		} else if err != nil {
			fmt.Fprintln(os.Stderr, "quickemu-tui: ignoring unreadable config:", err)
		}
	}

	program := tea.NewProgram(tui.New(opts), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "quickemu-tui:", err)
		os.Exit(1)
	}
}
