package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// A custom ISO is for a system quickget doesn't list. It asks for the image
// (a file or a URL), a name and an optional checksum in prompts, then the
// settings that quickget would have known (guest OS, boot, disk size) on one
// screen, then the CPU and memory steps of an ordinary install.

// customOSValue is the value of the "Custom ISO…" row in the OS list; no
// quickget OS id can be it.
const customOSValue = "\x00custom-iso"

func customItem() pickItem {
	return pickItem{value: customOSValue, label: "Custom ISO…", hint: "an image that isn't listed: a file or a URL"}
}

// the settings screen's rows
const (
	customRowOS = iota
	customRowBoot
	customRowDisk
	customRows
)

var customRowTitles = [customRows]string{"System type", "Boot", "Disk size"}

// customDraft is a custom ISO being set up. choice holds the picked index of
// each settings row.
type customDraft struct {
	active bool
	iso    qemu.CustomISO
	row    int
	choice [customRows]int
}

func newCustomDraft() customDraft {
	d := customDraft{active: true}
	d.choice[customRowOS] = indexOfOS(qemu.DefaultGuestOS)
	d.choice[customRowBoot] = indexOf(qemu.BootChoices, qemu.DefaultBoot)
	d.choice[customRowDisk] = indexOf(qemu.DiskSizeChoices, qemu.DefaultDiskSize)
	return d
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return 0
}

func indexOfOS(v string) int {
	for i, c := range qemu.GuestOSChoices {
		if c.Value == v {
			return i
		}
	}
	return 0
}

// rowChoices are the values of a settings row, and a hint for each.
func customRowChoices(row int) (values, hints []string) {
	switch row {
	case customRowOS:
		for _, c := range qemu.GuestOSChoices {
			values, hints = append(values, c.Value), append(hints, c.Hint)
		}
	case customRowBoot:
		return qemu.BootChoices, []string{"UEFI: what current systems expect", "BIOS: for old or unusual images"}
	default:
		hints := make([]string, len(qemu.DiskSizeChoices))
		for i := range hints {
			hints[i] = "the disk image grows as it fills, up to this"
		}
		return qemu.DiskSizeChoices, hints
	}
	return values, hints
}

// settle copies the picks onto the ISO's settings.
func (d *customDraft) settle() {
	os, _ := customRowChoices(customRowOS)
	d.iso.GuestOS = os[d.choice[customRowOS]]
	d.iso.Boot = qemu.BootChoices[d.choice[customRowBoot]]
	d.iso.DiskSize = qemu.DiskSizeChoices[d.choice[customRowDisk]]
}

// startCustom begins asking about a custom ISO.
func (m *Model) startCustom() tea.Cmd {
	m.custom = newCustomDraft()
	return m.askCustomSource("")
}

func (m *Model) askCustomSource(initial string) tea.Cmd {
	return m.askPrompt("Custom ISO: a file on this machine, or a URL to download", "Next", initial, func(m *Model, v string) tea.Cmd {
		if v == "" {
			return nil
		}
		source, problem := checkCustomSource(v)
		if problem != "" {
			m.setFlash(problem, true)
			return m.askCustomSource(v)
		}
		m.custom.iso.Source = source
		return m.askCustomName(qemu.SuggestVMName(source))
	})
}

// checkCustomSource returns the URL or absolute file path typed, or what's wrong with it.
func checkCustomSource(typed string) (source, problem string) {
	if qemu.IsURL(typed) {
		return qemu.NormalizeDownloadURL(typed), qemu.CheckURL(typed)
	}
	p := typed
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err.Error()
	}
	if reason := qemu.ISOProblemFor(p); reason != "" {
		return "", p + ": " + reason
	}
	return p, ""
}

func (m *Model) askCustomName(initial string) tea.Cmd {
	return m.askPrompt("Name for the new VM", "Next", initial, func(m *Model, v string) tea.Cmd {
		if v == "" {
			return nil
		}
		if err := qemu.ValidateVMName(v); err != nil {
			m.setFlash(err.Error(), true)
			return m.askCustomName(v)
		}
		m.custom.iso.Name = v
		if _, err := os.Stat(m.custom.iso.ConfPath(m.opts.Root)); err == nil {
			m.setFlash("A VM called "+v+" already exists", true)
			return m.askCustomName(v)
		}
		return m.askCustomChecksum("")
	})
}

func (m *Model) askCustomChecksum(initial string) tea.Cmd {
	return m.askPrompt("SHA-256 checksum: the digest, or the address of the checksum file (recommended; blank skips)", "Next", initial, func(m *Model, v string) tea.Cmd {
		m.custom.iso.SHA256, m.custom.iso.SHA256URL = "", ""
		switch {
		case v == "":
		case qemu.IsURL(v):
			if problem := qemu.CheckURL(v); problem != "" {
				m.setFlash(problem, true)
				return m.askCustomChecksum(v)
			}
			m.custom.iso.SHA256URL = v
		default:
			sum, err := qemu.NormalizeSHA256(v)
			if err != nil {
				m.setFlash(err.Error(), true)
				return m.askCustomChecksum(v)
			}
			m.custom.iso.SHA256 = sum
		}
		m.custom.row = 0
		m.instStep = stepCustomOptions
		return nil
	})
}

func (m Model) handleCustomOptionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := &m.custom
	switch msg.String() {
	case "ctrl+c":
		return m.interruptQuit()
	case "esc":
		m.instStep = stepOS
		m.custom = customDraft{}
	case "tab":
		d.row = (d.row + 1) % customRows
	case "shift+tab":
		d.row = (d.row + customRows - 1) % customRows
	case "up", "k":
		values, _ := customRowChoices(d.row)
		d.choice[d.row] = clamp(d.choice[d.row]-1, 0, len(values)-1)
	case "down", "j":
		values, _ := customRowChoices(d.row)
		d.choice[d.row] = clamp(d.choice[d.row]+1, 0, len(values)-1)
	case "enter":
		d.settle()
		m.instStep = stepCPU
		m.instFilter, m.instCursor = "", 0
	}
	return m, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// customOptionsTop is the line of the settings screen on which its rows start.
const customOptionsTop = 4

const customNextButton = "Next"

// customArrowCols are the columns, within a settings row, of its ‹ and › around value.
func customArrowCols(value string) (left, right int) {
	const labelWidth = 12 // the row is " " + a label padded to this + " ‹ "
	left = 1 + labelWidth + 1
	return left, left + 2 + lipgloss.Width(value) + 1
}

func (m Model) viewCustomOptions() string {
	d := m.custom
	lines := []string{
		titleStyle.Render("New VM: " + d.iso.Name + " – settings"),
		"",
		dimStyle.Render("What suits a typical Linux distribution is already chosen."),
		"", // customOptionsTop lines so far
	}
	for row := range customRows {
		values, hints := customRowChoices(row)
		line := fmt.Sprintf(" %-12s ‹ %s ›", customRowTitles[row], values[d.choice[row]])
		if row == d.row {
			line = selStyle.Render(line + " ")
		}
		lines = append(lines, line+"  "+dimStyle.Render(hints[d.choice[row]]))
	}
	return strings.Join(append(lines, "", button(customNextButton), "", dimStyle.Render("tab/shift-tab setting • ↑↓ change • enter next • esc back")), "\n")
}

func (m Model) confirmCustom() (tea.Model, tea.Cmd) {
	c := m.custom.iso
	source := "Copy:      " + c.Source + " (the original stays where it is)"
	if qemu.IsURL(c.Source) {
		source = "Download:  " + c.Source
	}
	checksum := "Checksum:  will be checked once it's here"
	if c.SHA256URL != "" {
		checksum = "Checksum:  will be fetched from " + c.SHA256URL + "\n           and checked once the image is here"
	}
	if c.SHA256 == "" && c.SHA256URL == "" {
		checksum = "Checksum:  none given, so the image can't be checked against what the\n           publisher released. It's worth pasting one next time."
	}
	extra := ""
	if n := countOptions(m.defaults); n > 0 {
		extra = fmt.Sprintf("\n\nYour %d default option(s) will be added to its .conf.", n)
	}
	choices := m.newVMChoices()
	if len(m.spiceChoices()) > 0 {
		extra += "\n\nIt will use a SPICE display, as spicy is installed (quick settings can change that)."
	}
	// The folder can be left over from a VM whose .conf is gone. quickemu uses a
	// disk that's there, so the installer would write over whatever it holds: the
	// user may mean that, so they're told rather than stopped, and enter says no.
	def := defaultYes
	if disk := filepath.Join(m.opts.Root, c.Name, "disk.qcow2"); fileExists(disk) {
		extra += "\n\nWARNING: " + tildify(disk) + " already exists. The new VM will use it,\nso installing will overwrite what's on it. Go back and pick another name\nunless that's what you want."
		def = defaultNo
	}
	m.askConfirm(fmt.Sprintf("Create the VM %s in %s?\n\n%s\n%s\nSettings:  %s, %s boot, %s disk; %s%s",
		c.Name, tildify(m.opts.Root), source, checksum, c.GuestOS, c.Boot, c.DiskSize, m.sizeSummary(), extra), def, func(m *Model) tea.Cmd {
		return m.startCustomInstall(c, choices)
	})
	return m, nil
}

// startCustomInstall fetches the image and writes the VM in the background,
// reporting through the same progress dialog as a quickget install.
func (m *Model) startCustomInstall(c qemu.CustomISO, choices []string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	note := "start it again to copy the image (a half-made copy isn't kept)"
	if qemu.IsURL(c.Source) {
		note = "start it again with the same address to resume the download"
	}
	st := &installState{
		cancelNote: note,
		title:      c.Name,
		percent:    -1,
		started:    time.Now(),
		msgs:       make(chan tea.Msg, 64),
		cancel:     cancel,
		finished:   make(chan struct{}),
	}
	m.install = st
	m.mode = modeInstallProgress
	root := m.opts.Root
	defaults := append([]string(nil), m.defaults...)
	go func() {
		defer close(st.finished)
		st.msgs <- runCustomInstall(ctx, root, c, choices, defaults, func(p qemu.InstallProgress) {
			select {
			case st.msgs <- installProgressMsg(p):
			default: // the UI is behind; a later update will catch it up
			}
		})
	}()
	return waitInstall(st)
}

// runCustomInstall brings the image into root/<name>/, checks it, and writes
// the .conf, with choices (the CPU and memory picked, which win) and defaults
// (which don't) added to it.
func runCustomInstall(ctx context.Context, root string, c qemu.CustomISO, choices, defaults []string, progress func(qemu.InstallProgress)) installDoneMsg {
	// the checksum file first: a wrong address is better found before a long download
	checksumFile := ""
	if c.SHA256URL != "" {
		text, err := qemu.FetchText(c.SHA256URL, 1<<20)
		if err != nil {
			return installDoneMsg{err: fmt.Errorf("couldn't fetch the checksum from %s: %v", c.SHA256URL, err)}
		}
		checksumFile = text
	}
	res, err := qemu.FetchISO(ctx, c.Source, filepath.Join(root, c.Name), progress)
	if err != nil {
		return installDoneMsg{err: err}
	}
	if checksumFile != "" {
		// the listing names the image, which a redirect only reveals once it has downloaded
		if c.SHA256, err = qemu.PickSHA256(checksumFile, res.File); err != nil {
			return installDoneMsg{err: fmt.Errorf("%v (%s)", err, c.SHA256URL)}
		}
	}
	if c.SHA256 != "" {
		if err := qemu.VerifySHA256(ctx, res.Path, c.SHA256, progress); err != nil {
			if ctx.Err() == nil {
				// kept, a bad image would be used as it is by the next attempt
				_ = os.Remove(res.Path)
				err = fmt.Errorf("%v\n\n%s was removed. Check the checksum, and the address, and try again.", err, res.File)
			}
			return installDoneMsg{err: err}
		}
	}
	confPath, err := qemu.CreateCustomVM(root, c, res.File)
	if err != nil {
		return installDoneMsg{err: err}
	}
	qemu.ForgetFetch(filepath.Join(root, c.Name)) // the VM exists now, so a retry has nothing to resume
	done := installDoneMsg{confs: []string{confPath}}
	if err := qemu.SetConfValues(confPath, choices); err != nil {
		done.mergeErr = err
	} else {
		done.added = append(done.added, choices...)
	}
	added, mergeErr := qemu.ApplyDefaults(confPath, defaults)
	done.added = append(done.added, added...)
	if mergeErr != nil && done.mergeErr == nil {
		done.mergeErr = mergeErr
	}
	if strings.EqualFold(filepath.Ext(res.File), ".iso") {
		done.isoProblems = qemu.VerifyISOs(qemu.VM{ConfPath: confPath})
	}
	if res.Copied {
		done.notes = append(done.notes, "The ISO was copied into the VM folder. You may delete it from "+res.From+" if you like.")
	}
	if c.SHA256 == "" {
		done.notes = append(done.notes, "No checksum was given, so the image wasn't checked against what the publisher released.")
	}
	return done
}
