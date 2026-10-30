package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

// writeISO makes a file that passes for an ISO 9660 image.
func writeISO(t *testing.T, path string) []byte {
	t.Helper()
	data := make([]byte, 40*2048)
	copy(data[16*2048:], "\x01CD001")
	copy(data[20*2048:], "the rest of the image")
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, data, 0o644))
	return data
}

func press(m Model, keys ...tea.KeyType) Model {
	for _, k := range keys {
		m, _ = keyOf(m, tea.KeyMsg{Type: k})
	}
	return m
}

// answer types text into the open prompt and presses enter.
func answer(t *testing.T, m Model, text string) Model {
	t.Helper()
	if m.mode != modePrompt {
		t.Fatalf("mode %v, want a prompt (flash %q)", m.mode, m.flash)
	}
	m = typed(m, text)
	return press(m, tea.KeyEnter)
}

// customAtSettings opens the picker, picks "Custom ISO…" and answers the prompts.
func customAtSettings(t *testing.T, m Model, source, checksum string) Model {
	t.Helper()
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if items := m.pickerItems(); len(items) == 0 || items[0].value != customOSValue {
		t.Fatalf("the first row is not the custom ISO: %+v", items)
	}
	m = press(m, tea.KeyEnter)
	m = answer(t, m, source)
	m = press(m, tea.KeyEnter) // the suggested name
	m = answer(t, m, checksum)
	if m.mode != modeInstallPick || m.instStep != stepCustomOptions {
		t.Fatalf("mode %v step %v (flash %q), want the settings screen", m.mode, m.instStep, m.flash)
	}
	return m
}

// finishInstall runs the background install to its end.
func finishInstall(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil && i < 1000; i++ {
		msg := cmd()
		next, c := m.Update(msg)
		m, cmd = next.(Model), c
		if _, done := msg.(installDoneMsg); done {
			return m
		}
	}
	t.Fatal("the install never finished")
	return m
}

func TestCustomISORowComesFirstAndFiltersLikeTheRest(t *testing.T) {
	m := newInstallModel(t)
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if items := m.pickerItems(); len(items) != 4 || items[0].value != customOSValue {
		t.Errorf("rows %+v, want Custom ISO then the three systems", items)
	}
	if v := ansi.Strip(m.viewInstallPick()); !strings.Contains(v, "Custom ISO…") {
		t.Errorf("the row isn't shown:\n%s", v)
	}
	m = typed(m, "win")
	if items := m.pickerItems(); len(items) != 1 || items[0].value != "windows" {
		t.Errorf("filter 'win' -> %+v", items)
	}
}

func TestCustomISORowWaitsForTheListOfSystems(t *testing.T) {
	m := New(Options{Root: t.TempDir()})
	m.width, m.height = 100, 40
	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if items := m.pickerItems(); len(items) != 0 {
		t.Errorf("rows %+v while nothing is loaded", items)
	}
}

func TestCustomISOFromAFileIsCopiedWithTheChosenSettings(t *testing.T) {
	m := newInstallModel(t)
	root := m.opts.Root
	src := filepath.Join(t.TempDir(), "Downloads", "mydistro-1.0-amd64.iso")
	want := writeISO(t, src)

	m = customAtSettings(t, m, src, "")
	view := ansi.Strip(m.viewInstallPick())
	for _, s := range []string{"mydistro-1.0-amd64", "linux", "efi", "32G"} {
		if !strings.Contains(view, s) {
			t.Errorf("settings screen lacks %q:\n%s", s, view)
		}
	}
	m = press(m, tea.KeyTab, tea.KeyDown) // the boot row, then its next choice: legacy
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "Copy:") || !strings.Contains(m.confirmText, "legacy boot") {
		t.Fatalf("mode %v, confirm %q", m.mode, m.confirmText)
	}
	if !strings.Contains(m.confirmText, "none given") {
		t.Errorf("no nudge towards a checksum:\n%s", m.confirmText)
	}

	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = finishInstall(t, m, cmd)

	conf, err := qemu.VM{ConfPath: filepath.Join(root, "mydistro-1.0-amd64.conf")}.Conf()
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"guest_os": "linux", "boot": "legacy", "disk_size": "32G",
		"disk_img": "mydistro-1.0-amd64/disk.qcow2", "iso": "mydistro-1.0-amd64/mydistro-1.0-amd64.iso",
	} {
		if conf[k] != v {
			t.Errorf("%s = %q, want %q", k, conf[k], v)
		}
	}
	if have, _ := os.ReadFile(filepath.Join(root, "mydistro-1.0-amd64", "mydistro-1.0-amd64.iso")); !bytes.Equal(have, want) {
		t.Error("the image wasn't copied into the VM folder")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("the original is gone: %v", err)
	}
	if m.mode != modeError || !m.errNotice {
		t.Fatalf("mode %v notice %v, want a notice", m.mode, m.errNotice)
	}
	for _, s := range []string{"copied into the VM folder", "delete it from " + src, "No checksum was given"} {
		if !strings.Contains(m.errBody, s) {
			t.Errorf("the notice lacks %q:\n%s", s, m.errBody)
		}
	}
	if len(m.vms) != 1 {
		t.Errorf("%d VMs listed, want the new one", len(m.vms))
	}
}

func TestCustomISOFromAURLIsDownloadedAndItsChecksumChecked(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	m := newInstallModel(t)
	root := m.opts.Root
	image := writeISO(t, filepath.Join(t.TempDir(), "served.iso"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "d.iso", time.Unix(0, 0), bytes.NewReader(image))
	}))
	defer srv.Close()
	sum := sha256.Sum256(image)

	m = customAtSettings(t, m, srv.URL+"/pub/newdistro.iso", "SHA256: "+hex.EncodeToString(sum[:]))
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if !strings.Contains(m.confirmText, "Download:") || !strings.Contains(m.confirmText, "will be checked") {
		t.Fatalf("confirm %q", m.confirmText)
	}
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = finishInstall(t, m, cmd)

	if have, _ := os.ReadFile(filepath.Join(root, "newdistro", "newdistro.iso")); !bytes.Equal(have, image) {
		t.Error("the image wasn't downloaded")
	}
	if m.mode == modeError && m.errNotice && strings.Contains(m.errBody, "No checksum") {
		t.Error("told there was no checksum when there was one")
	}
	if !strings.Contains(m.flash, "installed") || m.flashErr {
		t.Errorf("flash %q (error: %v)", m.flash, m.flashErr)
	}
}

func TestCustomISOPromptsRejectWhatCannotWork(t *testing.T) {
	m := newInstallModel(t)
	root := m.opts.Root
	must(t, os.WriteFile(filepath.Join(root, "taken.conf"), []byte("guest_os=\"linux\"\n"), 0o644))
	src := filepath.Join(t.TempDir(), "taken.iso")
	writeISO(t, src)

	m, _ = keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = press(m, tea.KeyEnter)

	m = answer(t, m, filepath.Join(t.TempDir(), "missing.iso"))
	if m.mode != modePrompt || !strings.Contains(m.flash, "does not exist") || !m.flashErr {
		t.Fatalf("a missing file: mode %v flash %q", m.mode, m.flash)
	}
	m = press(m, tea.KeyCtrlU)
	m = answer(t, m, "ftp://host/x.iso")
	if m.mode != modePrompt {
		t.Fatalf("a non-http URL moved on: mode %v flash %q", m.mode, m.flash)
	}
	m = press(m, tea.KeyCtrlU)
	m = answer(t, m, src) // taken.iso -> the suggested name is taken
	m = press(m, tea.KeyEnter)
	if m.mode != modePrompt || !strings.Contains(m.flash, "already exists") {
		t.Fatalf("an existing VM's name: mode %v flash %q", m.mode, m.flash)
	}
	m = press(m, tea.KeyCtrlU)
	m = answer(t, m, "bad name")
	if m.mode != modePrompt || !strings.Contains(m.flash, "letters, digits") {
		t.Fatalf("an invalid name: mode %v flash %q", m.mode, m.flash)
	}
	m = press(m, tea.KeyCtrlU)
	m = answer(t, m, "fresh")
	m = answer(t, m, "not-a-digest")
	if m.mode != modePrompt || !strings.Contains(m.flash, "SHA-256") {
		t.Fatalf("a bad checksum: mode %v flash %q", m.mode, m.flash)
	}
}

func TestCustomISOSettingsEscReturnsToTheList(t *testing.T) {
	m := newInstallModel(t)
	src := filepath.Join(t.TempDir(), "d.iso")
	writeISO(t, src)
	m = customAtSettings(t, m, src, "")
	m = press(m, tea.KeyEsc)
	if m.instStep != stepOS || m.custom.active {
		t.Errorf("step %v, custom active %v", m.instStep, m.custom.active)
	}
	// choosing an ordinary system afterwards must not carry the custom ISO's state
	m = typed(m, "ubuntu")
	m = press(m, tea.KeyEnter)
	if m.instStep != stepRelease || m.custom.active {
		t.Errorf("step %v, custom active %v", m.instStep, m.custom.active)
	}
}

func TestCustomISOCPUStepEscGoesBackToTheSettings(t *testing.T) {
	m := newInstallModel(t)
	src := filepath.Join(t.TempDir(), "d.iso")
	writeISO(t, src)
	m = customAtSettings(t, m, src, "")
	m = press(m, tea.KeyEnter)
	if m.instStep != stepCPU || !strings.Contains(ansi.Strip(m.viewInstallPick()), "New VM: d – choose number of CPUs") {
		t.Fatalf("step %v:\n%s", m.instStep, ansi.Strip(m.viewInstallPick()))
	}
	m = press(m, tea.KeyEsc)
	if m.instStep != stepCustomOptions {
		t.Errorf("step %v, want the settings", m.instStep)
	}
}

func TestACustomISOWithTheWrongChecksumIsRemovedAndMakesNoVM(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "d.iso")
	writeISO(t, src)
	c := qemu.CustomISO{Name: "d", Source: src, SHA256: strings.Repeat("0", 64), GuestOS: "linux", Boot: "efi", DiskSize: "32G"}

	done := runCustomInstall(context.Background(), root, c, nil, nil, nil)
	if done.err == nil || !strings.Contains(done.err.Error(), "doesn't match") {
		t.Fatalf("err %v, want a mismatch", done.err)
	}
	if _, err := os.Stat(filepath.Join(root, "d", "d.iso")); !os.IsNotExist(err) {
		t.Errorf("the bad image was kept (a retry would use it): %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "d.conf")); !os.IsNotExist(err) {
		t.Errorf("a .conf was written: %v", err)
	}
}

func TestACustomISOThatFailsToFetchMakesNoVM(t *testing.T) {
	root := t.TempDir()
	c := qemu.CustomISO{Name: "d", Source: filepath.Join(t.TempDir(), "missing.iso"), GuestOS: "linux", Boot: "efi", DiskSize: "32G"}
	if done := runCustomInstall(context.Background(), root, c, nil, nil, nil); done.err == nil {
		t.Fatal("no error for a missing file")
	}
	if _, err := os.Stat(filepath.Join(root, "d.conf")); !os.IsNotExist(err) {
		t.Errorf("a .conf was written: %v", err)
	}
}

// sourceForgeLike serves an ISO and its checksum file behind redirects, the
// way a browser's download link reaches them on SourceForge.
func sourceForgeLike(t *testing.T, image []byte, sumsBody func(string) string) *httptest.Server {
	t.Helper()
	return sourceForgeLikeCounting(t, image, nil, sumsBody)
}

// sourceForgeLikeCounting is sourceForgeLike, adding to *hits each time the
// image itself is fetched (if hits isn't nil).
func sourceForgeLikeCounting(t *testing.T, image []byte, hits *int, sumsBody func(string) string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/files/1.2/distro-1.2.iso/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mirror/project/distro/1.2/distro-1.2.iso?token=abc", http.StatusFound)
	})
	mux.HandleFunc("/files/latest/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mirror/project/distro/1.2/distro-1.2.iso?token=latest", http.StatusFound)
	})
	mux.HandleFunc("/mirror/project/distro/1.2/distro-1.2.iso", func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			mu.Lock()
			*hits++
			mu.Unlock()
		}
		http.ServeContent(w, r, "d.iso", time.Unix(0, 0), bytes.NewReader(image))
	})
	mux.HandleFunc("/files/1.2/distro-1.2.iso.sha256/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mirror/distro-1.2.iso.sha256?token=def", http.StatusFound)
	})
	mux.HandleFunc("/mirror/distro-1.2.iso.sha256", func(w http.ResponseWriter, r *http.Request) {
		sum := sha256.Sum256(image)
		_, _ = w.Write([]byte(sumsBody(hex.EncodeToString(sum[:]))))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCustomISOBehindRedirectsWithAChecksumFileIsNamedAndVerified(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	m := newInstallModel(t)
	root := m.opts.Root
	image := writeISO(t, filepath.Join(t.TempDir(), "served.iso"))
	srv := sourceForgeLike(t, image, func(sum string) string {
		return strings.Repeat("0", 64) + "  other.iso\n" + sum + "  distro-1.2.iso\n"
	})

	m = customAtSettings(t, m, srv.URL+"/files/1.2/distro-1.2.iso/download", srv.URL+"/files/1.2/distro-1.2.iso.sha256/download")
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if !strings.Contains(m.confirmText, "fetched from") || !strings.Contains(m.confirmText, "Download:") {
		t.Fatalf("confirm:\n%s", m.confirmText)
	}
	m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = finishInstall(t, m, cmd)

	if m.flashErr || !strings.Contains(m.flash, "installed") {
		t.Fatalf("flash %q (error %v), mode %v body %q", m.flash, m.flashErr, m.mode, m.errBody)
	}
	// named from the link without "/download", and the file from where it landed
	conf, err := qemu.VM{ConfPath: filepath.Join(root, "distro-1.2.conf")}.Conf()
	if err != nil {
		t.Fatal(err)
	}
	if conf["iso"] != "distro-1.2/distro-1.2.iso" {
		t.Errorf("iso = %q", conf["iso"])
	}
	if have, _ := os.ReadFile(filepath.Join(root, "distro-1.2", "distro-1.2.iso")); !bytes.Equal(have, image) {
		t.Error("the image wasn't downloaded")
	}
	if m.mode == modeError {
		t.Errorf("an unexpected dialog: %s", m.errBody)
	}
}

func TestChecksumFileThatDoesNotListTheImageStopsTheInstall(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	root := t.TempDir()
	image := writeISO(t, filepath.Join(t.TempDir(), "served.iso"))
	srv := sourceForgeLike(t, image, func(string) string {
		return strings.Repeat("1", 64) + "  a.iso\n" + strings.Repeat("2", 64) + "  b.iso\n"
	})
	c := qemu.CustomISO{
		Name: "distro", Source: srv.URL + "/files/1.2/distro-1.2.iso/download",
		SHA256URL: srv.URL + "/files/1.2/distro-1.2.iso.sha256/download",
		GuestOS:   "linux", Boot: "efi", DiskSize: "32G",
	}
	done := runCustomInstall(context.Background(), root, c, nil, nil, nil)
	if done.err == nil || !strings.Contains(done.err.Error(), "none is named distro-1.2.iso") {
		t.Fatalf("err %v", done.err)
	}
	if _, err := os.Stat(filepath.Join(root, "distro.conf")); !os.IsNotExist(err) {
		t.Errorf("a .conf was written: %v", err)
	}
}

func TestACustomChecksumAddressThatCannotBeFetchedStopsBeforeTheDownload(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	root := t.TempDir()
	image := writeISO(t, filepath.Join(t.TempDir(), "served.iso"))
	srv := sourceForgeLike(t, image, func(string) string { return "" })
	c := qemu.CustomISO{
		Name: "distro", Source: srv.URL + "/files/1.2/distro-1.2.iso/download",
		SHA256URL: srv.URL + "/nothing/here",
		GuestOS:   "linux", Boot: "efi", DiskSize: "32G",
	}
	done := runCustomInstall(context.Background(), root, c, nil, nil, nil)
	if done.err == nil || !strings.Contains(done.err.Error(), "couldn't fetch the checksum") {
		t.Fatalf("err %v", done.err)
	}
	if _, err := os.Stat(filepath.Join(root, "distro")); !os.IsNotExist(err) {
		t.Errorf("the download started anyway: %v", err)
	}
}

func TestCheckCustomSourceTurnsASourceForgeProjectPageIntoItsLatestDownload(t *testing.T) {
	got, problem := checkCustomSource("https://sourceforge.net/projects/sloth-os/")
	if problem != "" || got != "https://sourceforge.net/projects/sloth-os/files/latest/download" {
		t.Errorf("got %q, %q", got, problem)
	}
}

func TestCustomISOSettingsFlowWithTabAndTheUpDownArrows(t *testing.T) {
	m := newInstallModel(t)
	src := filepath.Join(t.TempDir(), "d.iso")
	writeISO(t, src)
	m = customAtSettings(t, m, src, "")

	m = press(m, tea.KeyTab)
	if m.custom.row != customRowBoot {
		t.Fatalf("tab: row %d, want the boot row", m.custom.row)
	}
	m = press(m, tea.KeyTab, tea.KeyTab)
	if m.custom.row != customRowOS {
		t.Errorf("tab past the last row: row %d, want it back at the first", m.custom.row)
	}
	m = press(m, tea.KeyShiftTab)
	if m.custom.row != customRowDisk {
		t.Errorf("shift-tab from the first row: row %d, want the last", m.custom.row)
	}

	m = press(m, tea.KeyDown) // the disk size: after 32G
	if m.custom.choice[customRowDisk] != indexOf(qemu.DiskSizeChoices, "64G") {
		t.Errorf("down: disk choice %d, want 64G", m.custom.choice[customRowDisk])
	}
	m = press(m, tea.KeyUp, tea.KeyUp, tea.KeyUp)
	if m.custom.choice[customRowDisk] != 0 {
		t.Errorf("up past the first choice: %d, want it to stop at 0", m.custom.choice[customRowDisk])
	}

	// left and right no longer do anything here
	before := m.custom
	m = press(m, tea.KeyLeft, tea.KeyRight)
	if m.custom.row != before.row || m.custom.choice != before.choice {
		t.Errorf("left/right changed the settings: %+v -> %+v", before, m.custom)
	}
	if v := ansi.Strip(m.viewInstallPick()); !strings.Contains(v, "tab/shift-tab") {
		t.Errorf("the screen doesn't say how to move:\n%s", v)
	}
}

func TestAnExistingDiskForTheNameIsWarnedAboutAndEnterSaysNo(t *testing.T) {
	for _, leftover := range []bool{true, false} {
		m := newInstallModel(t)
		src := filepath.Join(t.TempDir(), "old.iso")
		writeISO(t, src)
		disk := filepath.Join(m.opts.Root, "old", "disk.qcow2")
		if leftover {
			must(t, os.MkdirAll(filepath.Dir(disk), 0o755))
			must(t, os.WriteFile(disk, []byte("an earlier install"), 0o644))
		}
		m = customAtSettings(t, m, src, "")
		m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
		if m.mode != modeConfirm {
			t.Fatalf("mode %v, want the confirmation", m.mode)
		}
		warned := strings.Contains(m.confirmText, "WARNING") && strings.Contains(m.confirmText, "overwrite")
		if warned != leftover {
			t.Errorf("leftover disk %v: warned %v\n%s", leftover, warned, m.confirmText)
		}
		if want := map[bool]confirmDefault{true: defaultNo, false: defaultYes}[leftover]; m.confirmDefault != want {
			t.Errorf("leftover disk %v: enter means %v", leftover, m.confirmDefault)
		}
		if leftover {
			// the user can still go ahead: it's their disk
			m, cmd := keyOf(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
			if cmd == nil || m.install == nil {
				t.Error("y didn't start the install")
			}
			m.stopInstall()
		}
	}
}

func TestARetryAfterAFailureFollowingTheDownloadResumesFromTheFinishedImage(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	root := t.TempDir()
	image := writeISO(t, filepath.Join(t.TempDir(), "served.iso"))
	var hits int
	srv := sourceForgeLikeCounting(t, image, &hits, func(string) string {
		return strings.Repeat("1", 64) + "  a.iso\n" + strings.Repeat("2", 64) + "  b.iso\n" // doesn't list the image
	})
	c := qemu.CustomISO{
		// its name from the address typed ("latest") isn't the image's, which only the redirect gives
		Name: "distro", Source: srv.URL + "/files/latest/download",
		SHA256URL: srv.URL + "/files/1.2/distro-1.2.iso.sha256/download",
		GuestOS:   "linux", Boot: "efi", DiskSize: "32G",
	}
	if done := runCustomInstall(context.Background(), root, c, nil, nil, nil); done.err == nil {
		t.Fatal("a checksum file without the image was accepted")
	}
	if hits != 1 {
		t.Fatalf("%d downloads so far, want 1", hits)
	}

	c.SHA256URL = "" // the user corrects the mistake and tries again
	done := runCustomInstall(context.Background(), root, c, nil, nil, nil)
	if done.err != nil {
		t.Fatal(done.err)
	}
	if hits != 1 {
		t.Errorf("the retry downloaded again: %d downloads", hits)
	}
	if _, err := os.Stat(filepath.Join(root, "distro", ".quickemu-tui-fetch")); !os.IsNotExist(err) {
		t.Errorf("the download record is left once the VM exists: %v", err)
	}
	if conf, err := (qemu.VM{ConfPath: filepath.Join(root, "distro.conf")}).Conf(); err != nil || conf["iso"] != "distro/distro-1.2.iso" {
		t.Errorf("conf %v, %v", conf, err)
	}
}

func TestCancelMessagesSayWhatStartingAgainWillDo(t *testing.T) {
	for name, tc := range map[string]struct {
		note, want string
	}{
		"quickget":      {"", "install it again to resume"},
		"custom URL":    {"start it again with the same address to resume the download", "same address to resume the download"},
		"custom a copy": {"start it again to copy the image (a half-made copy isn't kept)", "half-made copy isn't kept"},
	} {
		t.Run(name, func(t *testing.T) {
			m := newInstallModel(t)
			m.install = &installState{title: "x", cancelNote: tc.note}
			next, _ := m.onInstallDone(installDoneMsg{err: context.Canceled})
			m = next.(Model)
			if !strings.Contains(m.flash, tc.want) {
				t.Errorf("flash %q, want it to say %q", m.flash, tc.want)
			}
		})
	}
}

func TestTheServersTotalDrivesThePercentageAndTheTimeLeftOnceKnown(t *testing.T) {
	old := clock
	now := t0
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = old })

	m := newInstallModel(t)
	m.install = &installState{title: "x", percent: -1, started: t0, msgs: make(chan tea.Msg, 1)}
	m.mode = modeInstallProgress
	feed := func(p qemu.InstallProgress) {
		next, _ := m.onInstallProgress(installProgressMsg(p))
		m = next.(Model)
	}
	// curl's coarse percentage is what there is until the total is known
	feed(qemu.InstallProgress{Percent: 3})
	if m.install.percent != 3 {
		t.Errorf("percent %v before the total, want curl's 3", m.install.percent)
	}
	// 1000 MiB in all, and 1 MiB/s measured over 10 s: 100 MiB done, so 900 s left
	for i := 0; i <= 10; i++ {
		now = t0.Add(time.Duration(i) * time.Second)
		feed(qemu.InstallProgress{Percent: -1, Bytes: int64(90+i) << 20, Total: 1000 << 20})
	}
	feed(qemu.InstallProgress{Percent: 40}) // curl's figure no longer counts
	if got := m.install.percent; got != 10 {
		t.Errorf("percent %v, want 10 from the 100 MiB of 1000 MiB", got)
	}
	if got := m.install.speedText(); got != "1.0 MiB/s, about 15m 00s left" {
		t.Errorf("got %q", got)
	}
}
