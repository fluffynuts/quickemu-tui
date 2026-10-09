// quickemu-tui: a terminal UI for managing quickemu VMs and their snapshots.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fluffynuts/quickemu-tui/internal/config"
	"github.com/fluffynuts/quickemu-tui/internal/seed"
	"github.com/fluffynuts/quickemu-tui/internal/tui"
	"github.com/fluffynuts/quickemu-tui/internal/upgrade"
)

// Set at build time by the Makefile (-ldflags "-X main.version=...").
var (
	version   = "dev"
	commit    = "" // short git SHA, with "-dirty" if built from uncommitted changes
	buildDate = "" // UTC, RFC 3339
)

// versionString is what -version prints, e.g.
// "quickemu-tui 0.1.57 (c84a1bb6abd5, built 2026-10-02T13:23:26Z)".
func versionString() string {
	c := commit
	if c == "" {
		c = vcsRevision() // a plain `go build` still records the commit
	}
	return formatVersion(version, c, buildDate)
}

func formatVersion(version, commit, built string) string {
	var meta []string
	if commit != "" {
		meta = append(meta, commit)
	}
	if built != "" {
		meta = append(meta, "built "+built)
	}
	s := "quickemu-tui " + version
	if len(meta) > 0 {
		s += " (" + strings.Join(meta, ", ") + ")"
	}
	return s
}

// vcsRevision reads the commit Go embedded at build time, if any.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev != "" && dirty {
		rev += "-dirty"
	}
	return rev
}

// runUpgrade replaces this binary with the latest GitHub release and returns
// the process exit code.
func runUpgrade() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "quickemu-tui: can't tell where this binary is:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	_, err = upgrade.Run(ctx, upgrade.Options{
		Current: version,
		ExePath: exe,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		// lets the upgrade be tried against a stand-in for GitHub
		APIBase: os.Getenv("QUICKEMU_TUI_API_BASE"),
		Log:     func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "quickemu-tui: upgrade failed:", err)
		return 1
	}
	return 0
}

// ensureVMDir creates the VM directory if it's missing (first run, or removed
// since): quickget runs inside it, and fails with a misleading
// "fork/exec .../quickget: no such file or directory" when it doesn't exist.
func ensureVMDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating VM directory %s: %w", dir, err)
	}
	return nil
}

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
	showVersion := flag.Bool("version", false, "print the version and exit")
	doUpgrade := flag.Bool("upgrade", false, "download the latest release for this machine from GitHub and replace this binary")
	noMouse := flag.Bool("no-mouse", false, "don't use the mouse, leaving it to the terminal (e.g. for selecting text without holding shift)")
	flag.Parse()
	if *showVersion {
		fmt.Println(versionString())
		return
	}
	if *doUpgrade {
		os.Exit(runUpgrade())
	}
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
	if err := ensureVMDir(root); err != nil {
		fmt.Fprintln(os.Stderr, "quickemu-tui:", err)
		os.Exit(2)
	}

	opts := tui.Options{Root: root, Quickemu: *quickemu, Version: version}
	if path, err := config.Path(); err == nil {
		opts.ConfigPath = path
		if cfg, err := config.Load(path); err == nil && cfg != nil {
			opts.Defaults = cfg.DefaultConf
		} else if err != nil {
			fmt.Fprintln(os.Stderr, "quickemu-tui: ignoring unreadable config:", err)
		}
	}

	if dir, err := os.UserCacheDir(); err == nil {
		cacheDir := filepath.Join(dir, "quickemu-tui")
		opts.CachePath = filepath.Join(cacheDir, seed.CatalogFile)
		// a first run starts from the lists this build carries, rather than
		// waiting minutes for quickget
		if err := seed.Unpack(cacheDir); err != nil {
			fmt.Fprintln(os.Stderr, "quickemu-tui: couldn't seed the cache:", err)
		}
	}

	programOpts := []tea.ProgramOption{tea.WithAltScreen()}
	if !*noMouse {
		programOpts = append(programOpts, tea.WithMouseCellMotion())
	}
	program := tea.NewProgram(tui.New(opts), programOpts...)
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "quickemu-tui:", err)
		os.Exit(1)
	}
}
