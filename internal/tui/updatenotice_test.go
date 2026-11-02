package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func headerWith(t *testing.T, opts Options, width int, msgs ...tea.Msg) string {
	t.Helper()
	opts.Root = t.TempDir()
	m := New(opts)
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	m.width, m.height = width, 30
	return ansi.Strip(strings.Split(m.View(), "\n")[0])
}

func wideEnough(t *testing.T) int {
	t.Helper()
	m := New(Options{Root: t.TempDir()})
	return lipgloss.Width(m.viewHeader()) + 80 // the temp dir's length varies by OS
}

func TestHeaderAnnouncesANewerRelease(t *testing.T) {
	first := headerWith(t, Options{Version: "0.1.29"}, wideEnough(t), updateMsg{latest: "0.1.40"})
	if !strings.Contains(first, "v0.1.40 available") || !strings.Contains(first, "quickemu-tui --upgrade") {
		t.Fatalf("no upgrade notice in %q", first)
	}
	if !strings.HasSuffix(strings.TrimRight(first, " "), "v0.1.29") {
		t.Fatalf("running version no longer at the right edge: %q", first)
	}
}

func TestHeaderSaysNothingWhenThereIsNoNewerRelease(t *testing.T) {
	first := headerWith(t, Options{Version: "0.1.29"}, wideEnough(t), updateMsg{})
	if strings.Contains(first, "available") || strings.Contains(first, "--upgrade") {
		t.Fatalf("unexpected notice in %q", first)
	}
}

func TestUpgradeNoticeShortensThenYieldsToTheVersionInANarrowHeader(t *testing.T) {
	m := New(Options{Root: t.TempDir(), Version: "0.1.29"})
	base := lipgloss.Width(m.viewHeader())
	msg := updateMsg{latest: "0.1.40"}

	short := headerWith(t, Options{Version: "0.1.29"}, base+len("v0.1.40 available  v0.1.29")+2, msg)
	if !strings.Contains(short, "v0.1.40 available") || strings.Contains(short, "--upgrade") {
		t.Fatalf("expected the short notice only: %q", short)
	}
	narrow := headerWith(t, Options{Version: "0.1.29"}, base+len("v0.1.29")+2, msg)
	if strings.Contains(narrow, "available") || !strings.Contains(narrow, "v0.1.29") {
		t.Fatalf("expected only the version: %q", narrow)
	}
}

func TestUpdateCheckCommandDeliversTheNewerRelease(t *testing.T) {
	m := New(Options{Root: t.TempDir(), Version: "0.1.29", CheckUpdate: func(context.Context) (string, error) {
		return "0.1.40", nil
	}})
	cmd := m.checkUpdateCmd()
	if cmd == nil {
		t.Fatal("no command")
	}
	if got, ok := cmd().(updateMsg); !ok || got.latest != "0.1.40" {
		t.Fatalf("got %+v", got)
	}
}

func TestNoUpdateCheckWhenNoneIsConfigured(t *testing.T) {
	if cmd := New(Options{Root: t.TempDir()}).checkUpdateCmd(); cmd != nil {
		t.Fatal("a check was started with nothing to run")
	}
}

func TestAFailedUpdateCheckIsSilent(t *testing.T) {
	m := New(Options{Root: t.TempDir(), Version: "0.1.29", CheckUpdate: func(context.Context) (string, error) {
		return "", errors.New("offline")
	}})
	msg := m.checkUpdateCmd()()
	if u, ok := msg.(updateMsg); !ok || u.latest != "" {
		t.Fatalf("a failure produced %+v", msg)
	}
	next, _ := m.Update(msg)
	m = next.(Model)
	m.width, m.height = wideEnough(t), 30
	if m.flash != "" || strings.Contains(ansi.Strip(m.View()), "offline") {
		t.Fatalf("a failed check surfaced: flash=%q", m.flash)
	}
}

func TestAnUpdateIsAnnouncedEvenWhenOnlyTheCacheCouldNotBeSaved(t *testing.T) {
	m := New(Options{Root: t.TempDir(), Version: "0.1.29", CheckUpdate: func(context.Context) (string, error) {
		return "0.1.40", errors.New("saving the update check: read-only file system")
	}})
	if got, ok := m.checkUpdateCmd()().(updateMsg); !ok || got.latest != "0.1.40" {
		t.Fatalf("the release that was found was dropped: %+v", got)
	}
}

// Runs what Init starts, and waits for the update check's answer among them.
// Everything else Init starts is harmless here: there are no VMs, and the
// catalog cache path points at a file that doesn't exist.
func TestInitStartsTheUpdateCheck(t *testing.T) {
	m := New(Options{
		Root:      t.TempDir(),
		Version:   "0.1.29",
		CachePath: filepath.Join(t.TempDir(), "catalog.json"),
		CheckUpdate: func(context.Context) (string, error) {
			return "0.1.40", nil
		},
	})
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init didn't start a batch of commands")
	}
	answers := make(chan updateMsg, len(batch))
	for _, cmd := range batch {
		go func() {
			if u, ok := cmd().(updateMsg); ok {
				answers <- u
			}
		}()
	}
	select {
	case u := <-answers:
		if u.latest != "0.1.40" {
			t.Fatalf("got %+v", u)
		}
	case <-time.After(10 * time.Second): // only reached when the check was never started
		t.Fatal("Init never started the update check")
	}
}
