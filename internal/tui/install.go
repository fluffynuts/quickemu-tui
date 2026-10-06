package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

type installStep int

const (
	stepOS installStep = iota
	stepRelease
	stepEdition
	stepCPU
	stepRAM
)

// pickItem is one row in the install picker.
type pickItem struct {
	value string
	label string
	hint  string // dim text after the label
}

// installState is a download in flight. The channel and cancel funcs are
// shared by every copy of the Model.
type installState struct {
	os, release, edition string
	title                string
	percent              float64 // -1: quickget isn't reporting one
	line                 string
	started              time.Time
	msgs                 chan tea.Msg
	cancel               context.CancelFunc
	finished             chan struct{} // closed once quickget has exited
}

// catalogMsg delivers the installable-systems list: either the quick read of the
// on-disk cache (cached) or the result of asking quickget.
type catalogMsg struct {
	catalog qemu.Catalog
	err     error
	cached  bool
}

// readCatalogCacheCmd loads the cached list, if there is one.
func readCatalogCacheCmd(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		c, err := qemu.ReadCatalogCache(path)
		return catalogMsg{catalog: c, err: err, cached: true}
	}
}

// fetchCatalogCmd asks quickget for the list (slow) and refreshes the cache.
func fetchCatalogCmd(quickemuOverride, cachePath string) tea.Cmd {
	return func() tea.Msg {
		q, err := qemu.FindQuickget(quickemuOverride)
		if err != nil {
			return catalogMsg{err: errors.New("quickget not found on PATH (it ships with quickemu)")}
		}
		c, raw, err := qemu.FetchCatalog(q)
		if err == nil && cachePath != "" {
			_ = qemu.WriteCatalogCache(cachePath, raw) // best effort
		}
		return catalogMsg{catalog: c, err: err}
	}
}

// onCatalog handles either kind of catalogMsg.
func (m Model) onCatalog(msg catalogMsg) (tea.Model, tea.Cmd) {
	if msg.cached {
		// an old list is better than none; a fresher one replaces it when it arrives
		if msg.err == nil && len(m.catalog) == 0 {
			m.catalog = msg.catalog
		}
		return m, nil
	}
	m.catalogLoading = false
	m.catalogErr = msg.err
	if msg.err == nil {
		m.catalog = msg.catalog
		return m, nil
	}
	// A failed refresh is silent while we have some list to offer. Only
	// someone waiting on the picker with nothing to show needs to hear.
	if len(m.catalog) == 0 && m.mode == modeInstallPick {
		m.mode = modeNormal
		m.showError("Couldn't list installable systems", msg.err.Error())
	}
	return m, nil
}

type installProgressMsg qemu.InstallProgress

type installDoneMsg struct {
	output   string // quickget's own output, minus progress bars
	err      error
	added    []string // default options merged into the new VM's .conf
	mergeErr error

	confs       []string          // the .conf files this install created
	isoProblems []qemu.ISOProblem // installation images that are missing or not real ISOs
}

// confSet is the set of .conf paths in dir.
func confSet(dir string) map[string]bool {
	vms, _ := qemu.Discover(dir)
	set := make(map[string]bool, len(vms))
	for _, vm := range vms {
		set[vm.ConfPath] = true
	}
	return set
}

// --- opening the picker -----------------------------------------------------

func (m *Model) openInstall() tea.Cmd {
	if m.install != nil {
		m.mode = modeInstallProgress
		return nil
	}
	m.mode = modeInstallPick
	m.instStep = stepOS
	m.instFilter, m.instCursor = "", 0
	m.instOS, m.instRelease, m.instEdition = "", "", ""
	m.instCores, m.instRAM = "", ""
	m.host = readHostInfo()
	switch {
	case m.catalogLoading:
		return nil // the startup fetch is still running; the picker shows a wait message until it lands
	case len(m.catalog) > 0 && m.catalogErr == nil:
		return nil
	}
	// nothing usable yet, or the last refresh failed: try again now
	m.catalogLoading = true
	return fetchCatalogCmd(m.opts.Quickemu, m.opts.CachePath)
}

func (m Model) instItems() []pickItem {
	var items []pickItem
	switch m.instStep {
	case stepOS:
		for _, o := range m.catalog.OSes() {
			items = append(items, pickItem{value: o.ID, label: o.DisplayName, hint: o.ID})
		}
	case stepRelease:
		for _, r := range m.catalog.Releases(m.instOS) {
			items = append(items, pickItem{value: r, label: r})
		}
	case stepEdition:
		for _, e := range m.catalog.Editions(m.instOS, m.instRelease) {
			label := e
			if e == "" {
				label = "(default)"
			}
			items = append(items, pickItem{value: e, label: label})
		}
	case stepCPU:
		return m.cpuItems() // short lists: no filtering
	case stepRAM:
		return m.ramItems()
	}
	if m.instFilter == "" {
		return items
	}
	needle := strings.ToLower(m.instFilter)
	var out []pickItem
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.label+" "+it.hint), needle) {
			out = append(out, it)
		}
	}
	return out
}

func (m Model) handleInstallPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.instItems()
	switch msg.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "esc":
		switch {
		case m.instFilter != "":
			m.instFilter = ""
		case m.instStep == stepOS:
			m.mode = modeNormal
		default:
			m.instStep = m.stepBack()
		}
		m.instCursor = 0
		return m, nil
	case "up":
		m.instCursor = clamp(m.instCursor-1, 0, len(items)-1)
	case "down":
		m.instCursor = clamp(m.instCursor+1, 0, len(items)-1)
	case "pgup":
		m.instCursor = clamp(m.instCursor-10, 0, len(items)-1)
	case "pgdown":
		m.instCursor = clamp(m.instCursor+10, 0, len(items)-1)
	case "backspace":
		if r := []rune(m.instFilter); len(r) > 0 {
			m.instFilter = string(r[:len(r)-1])
			m.instCursor = 0
		}
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		return m.pickItem(items[clamp(m.instCursor, 0, len(items)-1)])
	default:
		if m.instStep == stepCPU || m.instStep == stepRAM {
			return m, nil // nothing to filter
		}
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			for _, r := range msg.Runes {
				if unicode.IsPrint(r) {
					m.instFilter += string(r)
				}
			}
			if msg.Type == tea.KeySpace {
				m.instFilter += " "
			}
			m.instCursor = 0
		}
	}
	return m, nil
}

// stepBack is the step before the current one, skipping steps that had
// nothing to choose.
func (m Model) stepBack() installStep {
	switch m.instStep {
	case stepRAM:
		return stepCPU
	case stepCPU:
		if len(m.catalog.Editions(m.instOS, m.instRelease)) > 0 {
			return stepEdition
		}
	}
	if m.instStep != stepRelease && len(m.catalog.Releases(m.instOS)) > 1 {
		return stepRelease
	}
	return stepOS
}

func (m Model) pickItem(it pickItem) (tea.Model, tea.Cmd) {
	m.instFilter, m.instCursor = "", 0
	switch m.instStep {
	case stepOS:
		m.instOS = it.value
		m.instStep = stepRelease
		if len(m.catalog.Releases(it.value)) == 1 { // nothing to choose
			m.instRelease = m.catalog.Releases(it.value)[0]
			return m.afterRelease()
		}
	case stepRelease:
		m.instRelease = it.value
		return m.afterRelease()
	case stepEdition:
		m.instEdition = it.value
		m.instStep = stepCPU
	case stepCPU:
		m.instCores = it.value
		m.instStep = stepRAM
	case stepRAM:
		m.instRAM = it.value
		return m.confirmInstall()
	}
	return m, nil
}

func (m Model) afterRelease() (tea.Model, tea.Cmd) {
	if len(m.catalog.Editions(m.instOS, m.instRelease)) > 0 {
		m.instStep = stepEdition
		return m, nil
	}
	m.instEdition = ""
	m.instStep = stepCPU
	return m, nil
}

func (m Model) confirmInstall() (tea.Model, tea.Cmd) {
	edition := m.instEdition
	name := m.catalog.DisplayName(m.instOS)
	what := strings.TrimSpace(name + " " + m.instRelease + " " + edition)
	target := tildify(m.opts.Root)
	extra := ""
	if n := countOptions(m.defaults); n > 0 {
		extra = fmt.Sprintf("\n\nYour %d default option(s) will be added to its .conf.", n)
	}
	// back out of the picker only if confirmed; "no" returns to the picker
	choices := m.sizeChoices()
	m.askConfirm(fmt.Sprintf("Download %s and create a VM in %s?\n\n%s\n\nImages can be several GB. Progress is shown while it downloads,\nand a cancelled download can be resumed by installing it again.%s", what, target, m.sizeSummary(), extra), defaultYes, func(m *Model) tea.Cmd {
		return m.startInstall(name, m.instOS, m.instRelease, edition, choices)
	})
	return m, nil
}

// --- running the install ----------------------------------------------------

// startInstall runs quickget, then writes choices (the CPU and memory picked,
// which override anything quickget set) and the user's defaults (which don't)
// into the new VM's .conf.
func (m *Model) startInstall(displayName, os, release, edition string, choices []string) tea.Cmd {
	q, err := qemu.FindQuickget(m.opts.Quickemu)
	if err != nil {
		m.mode = modeNormal
		m.setFlash("quickget not found on PATH (it ships with quickemu)", true)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &installState{
		os: os, release: release, edition: edition,
		title:    strings.TrimSpace(displayName + " " + release + " " + edition),
		percent:  -1,
		started:  time.Now(),
		msgs:     make(chan tea.Msg, 64),
		cancel:   cancel,
		finished: make(chan struct{}),
	}
	m.install = st
	m.mode = modeInstallProgress
	root := m.opts.Root
	defaults := append([]string(nil), m.defaults...)
	go func() {
		defer close(st.finished)
		before := confSet(root)
		output, err := qemu.Install(ctx, q, root, os, release, edition, func(p qemu.InstallProgress) {
			select {
			case st.msgs <- installProgressMsg(p):
			default: // the UI is behind; a later update will catch it up
			}
		})
		done := installDoneMsg{output: output, err: err}
		if err == nil {
			// the .conf quickget just wrote gets the user's default options
			for path := range confSet(root) {
				if before[path] {
					continue
				}
				done.confs = append(done.confs, path)
				if err := qemu.SetConfValues(path, choices); err != nil {
					done.mergeErr = err
				} else {
					done.added = append(done.added, choices...)
				}
				added, mergeErr := qemu.ApplyDefaults(path, defaults)
				done.added = append(done.added, added...)
				if mergeErr != nil && done.mergeErr == nil {
					done.mergeErr = mergeErr
				}
				// quickget can "succeed" having saved a web page where the ISO
				// should be (e.g. a vendor moved the download)
				done.isoProblems = append(done.isoProblems, qemu.VerifyISOs(qemu.VM{ConfPath: path})...)
			}
		}
		st.msgs <- done
	}()
	return waitInstall(st)
}

func waitInstall(st *installState) tea.Cmd {
	return func() tea.Msg { return <-st.msgs }
}

// stopInstall cancels a running install and waits briefly for quickget to exit.
func (m *Model) stopInstall() {
	if m.install == nil {
		return
	}
	m.install.cancel()
	select {
	case <-m.install.finished:
	case <-time.After(6 * time.Second):
	}
}

func (m Model) handleInstallProgressKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		return m.requestQuit()
	case "esc", "enter", "b":
		m.mode = modeNormal // keeps running; n brings this back
	case "c":
		if m.install != nil {
			m.askConfirm("Cancel the install of "+m.install.title+"?\nPartly downloaded files are kept so it can resume.", defaultNo, func(m *Model) tea.Cmd {
				if m.install != nil {
					m.install.cancel()
				}
				return nil
			})
		}
	}
	return m, nil
}

func (m Model) onInstallProgress(p installProgressMsg) (tea.Model, tea.Cmd) {
	if m.install == nil {
		return m, nil
	}
	if p.Line != "" {
		m.install.line = p.Line
	}
	if p.Percent >= 0 {
		m.install.percent = p.Percent
	}
	return m, waitInstall(m.install)
}

func (m Model) onInstallDone(msg installDoneMsg) (tea.Model, tea.Cmd) {
	st := m.install
	m.install = nil
	if m.mode == modeInstallProgress || m.mode == modeInstallPick {
		m.mode = modeNormal
	}
	switch {
	case st == nil:
	case errors.Is(msg.err, context.Canceled):
		m.setFlash("Install of "+st.title+" cancelled; install it again to resume", true)
	case msg.err != nil:
		m.setFlash(st.title+" install failed: "+firstLine(msg.err.Error()), true)
		m.showError(st.title+" install failed", msg.err.Error())
	default:
		m.reportInstalled(st, msg)
	}
	m.rediscover()
	return m, batch(m.pollNow(), m.loadSelectedDisk())
}

// reportInstalled tells the user how a finished install went: a plain success,
// or a dialog if the VM was created but can't be installed from as it stands.
func (m *Model) reportInstalled(st *installState, msg installDoneMsg) {
	isoBad := len(msg.isoProblems) > 0
	outputBad := qemu.OutputLooksFailed(msg.output) // quickget exits 0 even after e.g. a failed unzip
	switch {
	case isoBad:
		m.setFlash(st.title+": the installation ISO is not usable: "+msg.isoProblems[0].Path, true)
		report := isoReport(st.title, msg.isoProblems, msg.confs)
		if outputBad {
			report += "\n\nquickget also reported:\n" + msg.output
		}
		m.showError(st.title+" needs an installation ISO", report)
	case outputBad:
		m.setFlash(st.title+": quickget reported problems, the VM probably won't boot", true)
		m.showError(st.title+" finished with problems", msg.output)
	default:
		m.setFlash(st.title+" installed ✓"+defaultsNote(msg.added, msg.mergeErr), msg.mergeErr != nil)
	}
}

// isoReport explains missing/invalid ISOs, with full paths.
func isoReport(title string, problems []qemu.ISOProblem, confs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The VM for %s was created, but its installation ISO is not usable, so it can't be installed from.\n", title)
	for _, p := range problems {
		fmt.Fprintf(&b, "\n  %s\n    %s\n", p.Path, p.Reason)
	}
	b.WriteString("\nDownload a genuine ISO yourself from the vendor's site")
	if len(problems) == 1 {
		fmt.Fprintf(&b, " and save it as the path above (replacing that file)")
	} else {
		b.WriteString(" and save it over the file(s) above")
	}
	if len(confs) > 0 {
		fmt.Fprintf(&b, ", or point iso= at it in %s", confs[0])
	}
	b.WriteString(".")
	return b.String()
}

// --- views ------------------------------------------------------------------

var installStepTitles = map[installStep]string{
	stepOS: "operating system", stepRelease: "release", stepEdition: "edition",
	stepCPU: "number of CPUs", stepRAM: "memory",
}

func (m Model) viewInstallPick() string {
	title := "New VM: choose " + installStepTitles[m.instStep]
	if m.instStep != stepOS {
		title = "New VM: " + m.catalog.DisplayName(m.instOS)
		if m.instStep >= stepEdition {
			title += " " + m.instRelease
		}
		if m.instStep >= stepCPU && m.instEdition != "" {
			title += " " + m.instEdition
		}
		title += " – choose " + installStepTitles[m.instStep]
	}
	lines := []string{titleStyle.Render(title), ""}
	if len(m.catalog) == 0 {
		return strings.Join(append(lines, dimStyle.Render("asking quickget what it can install (this can take a while)…")), "\n")
	}
	sizing := m.instStep == stepCPU || m.instStep == stepRAM
	if sizing {
		lines = append(lines, m.viewSizeTable()...)
		lines = append(lines, "")
	} else {
		lines = append(lines, "filter: "+m.instFilter+dimStyle.Render("▏"), "")
	}
	items := m.instItems()
	if len(items) == 0 {
		lines = append(lines, dimStyle.Render("nothing matches"))
	}
	cursor := clamp(m.instCursor, 0, len(items)-1)
	rows := make([]string, len(items))
	width := 0
	for _, it := range items {
		width = max(width, lipgloss.Width(it.label))
	}
	width = min(width, 40)
	for i, it := range items {
		row := " " + trunc(it.label, 40) + strings.Repeat(" ", width-min(lipgloss.Width(it.label), 40))
		if it.hint != "" {
			row += "  " + it.hint
		}
		row += " "
		if i == cursor {
			row = selStyle.Render(row)
		}
		rows[i] = row
	}
	listRows := max(3, min(14, m.height-16))
	help := fmt.Sprintf("%d shown • type to filter • enter select • esc back", len(items))
	if sizing {
		listRows = max(3, min(10, m.height-26)) // the table takes room
		help = "↑↓ select • enter next • esc back"
	}
	lines = append(lines, windowAround(rows, cursor, listRows)...)
	lines = append(lines, "", dimStyle.Render(help))
	return strings.Join(lines, "\n")
}

const progressBarWidth = 40

func progressBar(pct float64) string {
	filled := int(pct/100*progressBarWidth + 0.5)
	filled = clamp(filled, 0, progressBarWidth)
	return okStyle.Render(strings.Repeat("█", filled)) + dimStyle.Render(strings.Repeat("░", progressBarWidth-filled))
}

func (m Model) viewInstallProgress() string {
	st := m.install
	if st == nil {
		return ""
	}
	lines := []string{titleStyle.Render("Installing " + st.title), ""}
	if st.percent >= 0 {
		lines = append(lines, fmt.Sprintf("%s  %5.1f%%", progressBar(st.percent), st.percent))
	} else {
		lines = append(lines, "working…")
	}
	line := st.line
	if line == "" {
		line = "starting quickget…"
	}
	lines = append(lines, "", dimStyle.Render(trunc(line, progressBarWidth+8)),
		dimStyle.Render("elapsed "+time.Since(st.started).Truncate(time.Second).String()),
		"", dimStyle.Render("esc/b keep running in background • c cancel • n reopens this"))
	return strings.Join(lines, "\n")
}

// installHeader is the short status shown in the header while installing.
func (m Model) installHeader() string {
	if m.install == nil {
		return ""
	}
	s := "  ⬇ " + m.install.title
	if m.install.percent >= 0 {
		s += fmt.Sprintf(" %.0f%%", m.install.percent)
	}
	return warnStyle.Render(s)
}
