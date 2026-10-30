package qemu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// CustomISO is a VM made from an installation image quickget doesn't list.
type CustomISO struct {
	Name   string
	Source string // a local path, or an http(s) URL to download
	SHA256 string // the image's published checksum; "" if the user gave none
	// SHA256URL is where to fetch the checksum from instead, a checksum file
	// that may list other images too (see PickSHA256).
	SHA256URL string
	GuestOS   string
	Boot      string // "efi" or "legacy"
	DiskSize  string // e.g. "32G"
}

// GuestOSChoice is a guest_os value quickemu tunes the virtual hardware for.
type GuestOSChoice struct {
	Value string
	Hint  string
}

// GuestOSChoices are the values quickemu 4.9 treats specially (everything
// else gets a generic profile with slower devices). macOS isn't offered: it
// needs a recovery image and OpenCore, which only quickget fetches.
var GuestOSChoices = []GuestOSChoice{
	{"linux", "most Linux distributions"},
	{"linux_old", "older Linux without virtio graphics (vmware-svga display)"},
	{"windows", "Windows"},
	{"windows-server", "Windows Server"},
	{"freebsd", "FreeBSD"},
	{"ghostbsd", "GhostBSD"},
	{"solaris", "Solaris and illumos"},
	{"haiku", "Haiku"},
	{"freedos", "FreeDOS"},
	{"reactos", "ReactOS"},
	{"kolibrios", "KolibriOS"},
	{"batocera", "Batocera"},
}

// BootChoices are the only values written for boot: quickemu treats any
// value without "efi" in it as legacy, so a typo would quietly boot differently.
var BootChoices = []string{"efi", "legacy"}

// DiskSizeChoices are the disk sizes offered.
var DiskSizeChoices = []string{"16G", "32G", "64G", "128G", "256G"}

// The defaults suit a typical Linux distribution.
const (
	DefaultGuestOS  = "linux"
	DefaultBoot     = "efi"
	DefaultDiskSize = "32G"
)

// IsURL is whether source is something to download rather than a file.
func IsURL(source string) bool {
	s := strings.ToLower(source)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// CheckURL says what's wrong with source as a download address, or "".
func CheckURL(source string) string {
	u, err := url.Parse(source)
	switch {
	case err != nil:
		return err.Error()
	case u.Host == "":
		return "that URL has no host"
	}
	return ""
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._+-]`)

// ISOFileName is the name the image gets in the VM's folder: that of the
// file, or the last part of the URL's path (not counting a trailing
// "download", which is how SourceForge and others end the link to a file),
// kept to characters that are safe in a .conf and on the command line. A
// download that redirects is named from where it ends up instead; see FetchISO.
func ISOFileName(source string) string {
	name := ""
	if IsURL(source) {
		if u, err := url.Parse(source); err == nil {
			name = lastSegment(u.Path)
		}
	} else {
		name = filepath.Base(source)
	}
	name = strings.TrimLeft(unsafeFileChars.ReplaceAllString(name, "_"), ".-_")
	if name == "" {
		return "installer.iso"
	}
	return name
}

func lastSegment(p string) string {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if n := len(segs); n > 1 && segs[n-1] == "download" {
		segs = segs[:n-1]
	}
	return segs[len(segs)-1]
}

var (
	sourceForgeProjectRe = regexp.MustCompile(`^/projects/([^/]+)(?:/files)?/?$`)
	sourceForgeLatestRe  = regexp.MustCompile(`^/projects/([^/]+)/files/latest`)
)

func isSourceForge(host string) bool {
	host = strings.ToLower(host)
	return host == "sourceforge.net" || strings.HasSuffix(host, ".sourceforge.net")
}

// NormalizeDownloadURL turns the address of a SourceForge project page, which
// is a web page, into the link that redirects to the project's latest
// download. Every other address is returned as it is.
func NormalizeDownloadURL(source string) string {
	u, err := url.Parse(source)
	if err != nil || !IsURL(source) || !isSourceForge(u.Hostname()) {
		return source
	}
	if m := sourceForgeProjectRe.FindStringSubmatch(u.Path); m != nil {
		return "https://sourceforge.net/projects/" + m[1] + "/files/latest/download"
	}
	return source
}

// SuggestVMName is a VM name for source, from its file name.
func SuggestVMName(source string) string {
	name := ISOFileName(source)
	if m := sourceForgeLatestRe.FindStringSubmatch(urlPath(source)); m != nil && name == "latest" {
		name = m[1]
	}
	if ext := strings.ToLower(filepath.Ext(name)); ext == ".iso" || ext == ".img" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	name = strings.TrimLeft(strings.ReplaceAll(name, "+", "-"), "._-")
	if name == "" || ValidateVMName(name) != nil {
		return "custom-vm"
	}
	return name
}

func urlPath(source string) string {
	if u, err := url.Parse(source); err == nil && IsURL(source) {
		return u.Path
	}
	return ""
}

var sha256Re = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)

// NormalizeSHA256 finds a SHA-256 digest in text: either the digest on its
// own or a line pasted from a SHA256SUMS file. It returns it in lower case.
func NormalizeSHA256(text string) (string, error) {
	if m := sha256Re.FindString(text); m != "" {
		return strings.ToLower(m), nil
	}
	return "", errors.New("that doesn't contain a SHA-256 digest (64 hexadecimal characters)")
}

// PickSHA256 finds the digest for file in the text of a checksum file: one
// digest on its own or on a line, or a SHA256SUMS listing, from which the
// line naming file ("<digest>  file" or "<digest> *file") is chosen.
func PickSHA256(text, file string) (string, error) {
	var all []string
	for _, line := range strings.Split(text, "\n") {
		digest := sha256Re.FindString(line)
		if digest == "" {
			continue
		}
		digest = strings.ToLower(digest)
		if file != "" && checksumLineName(line, digest) == file {
			return digest, nil
		}
		all = append(all, digest)
	}
	switch {
	case len(all) == 0:
		return "", errors.New("no SHA-256 digest (64 hexadecimal characters) was found in the checksum")
	case len(all) == 1:
		return all[0], nil
	}
	return "", fmt.Errorf("the checksum file lists %d images and none is named %s", len(all), file)
}

var bsdChecksumNameRe = regexp.MustCompile(`\(([^)]+)\)`)

// checksumLineName is the file a line of a checksum file is about: in the
// BSD form "SHA256 (file) = digest" it's in the brackets, otherwise it
// follows the digest, as in "digest  file" or "digest *file" (here the base
// name, as a listing may give "./dl/file").
func checksumLineName(line, digest string) string {
	if m := bsdChecksumNameRe.FindStringSubmatch(line); m != nil {
		return filepath.Base(m[1])
	}
	after := strings.TrimSpace(strings.Replace(line, digest, "", 1))
	return filepath.Base(strings.TrimLeft(after, "*: "))
}

// FetchText fetches the small text file at rawURL, following redirects. More
// than maxBytes of it is an error: curl's own limit only applies when the
// server says how long the reply is, so what is kept is counted here too.
func FetchText(rawURL string, maxBytes int64) (string, error) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		return "", errors.New("curl not found on PATH: it's needed to fetch the checksum")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := append(curlProtocolArgs(rawURL), "--silent", "--show-error", "--location", "--fail",
		"--max-filesize", fmt.Sprint(maxBytes), rawURL)
	cmd := exec.CommandContext(ctx, curl, args...)
	body := &limitedBuffer{max: maxBytes}
	var stderr strings.Builder
	cmd.Stdout, cmd.Stderr = body, &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		const curlFileTooLarge = 63 // its own --max-filesize, which newer versions apply to a chunked reply too
		if body.exceeded || (errors.As(err, &exit) && exit.ExitCode() == curlFileTooLarge) {
			return "", fmt.Errorf("%s is bigger than %s, too big to be a checksum file", rawURL, HumanSize(maxBytes))
		}
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return body.String(), nil
}

// limitedBuffer collects up to max bytes and fails any write beyond that, which
// stops the process writing to it.
type limitedBuffer struct {
	strings.Builder
	max      int64
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len()+len(p)) > b.max {
		b.exceeded = true
		return 0, errors.New("too much data")
	}
	return b.Builder.Write(p)
}

// curlProtocolArgs limit the schemes curl speaks to http and https, and the
// ones it may be redirected to likewise; an https address must stay https,
// as a redirect to plain http would let anyone on the way swap what's
// downloaded (and the checksum fetched the same way).
func curlProtocolArgs(rawURL string) []string {
	redirect := "=http,https"
	if strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		redirect = "=https"
	}
	return []string{"--proto", "=http,https", "--proto-redir", redirect}
}

// ConfLines are the .conf assignments for c, the image being isoFile in the
// VM's folder.
func (c CustomISO) ConfLines(isoFile string) []string {
	return []string{
		`guest_os="` + c.GuestOS + `"`,
		`boot="` + c.Boot + `"`,
		`disk_size="` + c.DiskSize + `"`,
		`disk_img="` + c.Name + `/disk.qcow2"`,
		`iso="` + c.Name + `/` + isoFile + `"`,
	}
}

// ConfPath is where c's .conf goes in root.
func (c CustomISO) ConfPath(root string) string {
	return filepath.Join(root, c.Name+".conf")
}

// CreateCustomVM writes c's .conf into root. It refuses to touch an existing one.
func CreateCustomVM(root string, c CustomISO, isoFile string) (string, error) {
	confPath := c.ConfPath(root)
	f, err := os.OpenFile(confPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(strings.Join(c.ConfLines(isoFile), "\n") + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(confPath)
		return "", err
	}
	return confPath, nil
}

// FetchResult is how an image reached the VM's folder.
type FetchResult struct {
	Path   string
	File   string // the image's name in the folder
	Copied bool   // from a local file, which is still where it was
	From   string // that file
}

// FetchISO puts the image at source, a URL or a local file, into dir. A
// download goes through curl, which follows redirects, and the image is named
// after the address it ends up at (a "…/download" link that redirects to
// "…/distro-1.2.iso" gives distro-1.2.iso); a local file is copied under its
// own name. Whatever arrives must pass for an ISO (unless it is a .img), so a
// web page where the download should be is an error and leaves nothing behind.
//
// A download in progress is kept as <name>.part, named from the address typed
// so that trying the same address again resumes it. The image only appears
// under its name once complete, so one that is there already, under the name
// the address suggests, is used as it is. Cancelling ctx keeps a partial
// download (but not a partial copy).
func FetchISO(ctx context.Context, source, dir string, progress func(InstallProgress)) (FetchResult, error) {
	guess := ISOFileName(source)
	res := FetchResult{Path: filepath.Join(dir, guess), File: guess}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	if _, err := os.Stat(res.Path); err == nil {
		return res, nil
	}
	if IsURL(source) {
		if file, ok := rememberedFetch(dir, source); ok {
			return FetchResult{Path: filepath.Join(dir, file), File: file}, nil
		}
	}
	part := res.Path + ".part"
	if !IsURL(source) {
		if err := copyLocal(ctx, source, part, progress); err != nil {
			return res, err
		}
		res.Copied, res.From = true, source
		return res, os.Rename(part, res.Path)
	}
	final, err := download(ctx, source, part, progress)
	if err != nil {
		return res, err
	}
	res.File = ISOFileName(final)
	res.Path = filepath.Join(dir, res.File)
	if problem := ISOProblemFor(part); problem != "" && !isRawImage(res.File) {
		_ = os.Remove(part) // kept, a retry would resume from a web page
		return res, fmt.Errorf("%s: what it served %s\n\nIf that is a download page, use the address of its download button or link.", source, problem)
	}
	if err := os.Rename(part, res.Path); err != nil {
		return res, err
	}
	rememberFetch(dir, source, res.File)
	return res, nil
}

// A download is named from where its redirects end up, which a retry can't
// know without downloading again, so what it was named is kept beside it.
const fetchRecord = ".quickemu-tui-fetch"

func rememberFetch(dir, source, file string) {
	// only a convenience: without it, a retry downloads the image again
	_ = os.WriteFile(filepath.Join(dir, fetchRecord), []byte(source+"\n"+file+"\n"), 0o644)
}

// rememberedFetch is the name an earlier, finished download of source was
// given, if that image is still there.
func rememberedFetch(dir, source string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, fetchRecord))
	if err != nil {
		return "", false
	}
	was, file, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	if was != source || file == "" || file != filepath.Base(file) {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
		return "", false
	}
	return file, true
}

// ForgetFetch removes the record of how dir's image was downloaded, once the
// VM it was for is made.
func ForgetFetch(dir string) { _ = os.Remove(filepath.Join(dir, fetchRecord)) }

func isRawImage(name string) bool { return strings.EqualFold(filepath.Ext(name), ".img") }

// finalMarker starts the line curl is made to print, once done, with the
// address the download ended up at after its redirects.
const finalMarker = "quickemu-tui-final: "

// download fetches rawURL into part, resuming it, and returns the address the
// body finally came from.
func download(ctx context.Context, rawURL, part string, progress func(InstallProgress)) (finalURL string, err error) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		return "", errors.New("curl not found on PATH: it's needed to download an ISO")
	}
	// the shim resumes a download that drops part way (see curlShim)
	if shimDir, err := writeCurlShim(); err == nil {
		defer removeAll(shimDir)
		curl = filepath.Join(shimDir, "curl")
	}
	// the headers say how big the whole file is, which curl's percentage (coarse,
	// and 100% for a moment when it resumes) can't say as well
	headers := part + ".headers"
	defer os.Remove(headers)
	args := append(curlProtocolArgs(rawURL), "--location", "--fail", "--continue-at", "-", "--progress-bar",
		"--dump-header", headers, "--write-out", "\n"+finalMarker+"%{url_effective}\n", "--output", part, rawURL)
	cmd := exec.CommandContext(ctx, curl, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second

	// curl's bar says how far it is, but not how much has arrived, which is what
	// a speed needs, so the size of the file is reported as well
	var mu sync.Mutex
	report := func(p InstallProgress) {
		mu.Lock()
		defer mu.Unlock()
		if progress != nil {
			progress(p)
		}
	}
	var total int64
	stopPolling := pollSize(ctx, part, sizePollInterval, func(n int64) {
		if total == 0 {
			if data, err := os.ReadFile(headers); err == nil {
				total = totalFromHeaders(string(data))
			}
		}
		report(InstallProgress{Percent: -1, Bytes: n, Total: total})
	})
	output, startErr, waitErr := runScanned(ctx, cmd, func(p InstallProgress) {
		switch {
		case strings.HasPrefix(p.Line, finalMarker):
		case p.Percent >= 100:
			// curl's first line when it resumes is 100%, before it starts counting;
			// being done shows by it finishing
			p.Percent = -1
			report(p)
		default:
			report(p)
		}
	})
	stopPolling()
	if startErr != nil {
		return "", fmt.Errorf("couldn't run curl: %w", startErr)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if waitErr != nil {
		return "", fmt.Errorf("downloading %s failed: %v\n\n%s", rawURL, waitErr, output)
	}
	for _, line := range strings.Split(output, "\n") {
		if u, ok := strings.CutPrefix(line, finalMarker); ok && u != "" {
			return strings.TrimSpace(u), nil
		}
	}
	return rawURL, nil
}

// sizePollInterval is how often the size of a download in progress is
// reported; tests shorten it.
var sizePollInterval = time.Second

// totalFromHeaders is how big the whole file is according to the last
// successful response in headers, which curl dumps for each hop of a
// redirect: from its Content-Range ("bytes 100-999/1000") when it's a
// resumed, partial response, else from its Content-Length. It is 0 when the
// server didn't say.
func totalFromHeaders(headers string) int64 {
	var total int64
	for _, block := range strings.Split(strings.ReplaceAll(headers, "\r\n", "\n"), "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		status := strings.Fields(lines[0])
		if len(status) < 2 || !strings.HasPrefix(lines[0], "HTTP/") || !strings.HasPrefix(status[1], "2") {
			continue
		}
		var length, ranged int64
		for _, l := range lines[1:] {
			name, value, _ := strings.Cut(l, ":")
			value = strings.TrimSpace(value)
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "content-length":
				length, _ = strconv.ParseInt(value, 10, 64)
			case "content-range":
				if _, n, ok := strings.Cut(value, "/"); ok {
					ranged, _ = strconv.ParseInt(strings.TrimSpace(n), 10, 64)
				}
			}
		}
		switch {
		case ranged > 0:
			total = ranged
		case status[1] == "200" && length > 0:
			total = length
		}
	}
	return total
}

// pollSize calls report with the size of the file at path every interval until
// the returned func is called, which waits for any report in progress. It
// reports even when the size hasn't changed, so that a stall shows as one.
func pollSize(ctx context.Context, path string, interval time.Duration, report func(int64)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if st, err := os.Stat(path); err == nil {
					report(st.Size())
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

const copyChunk = 1 << 20

func copyLocal(ctx context.Context, src, part string, progress func(InstallProgress)) error {
	if problem := ISOProblemFor(src); problem != "" && !isRawImage(src) {
		return fmt.Errorf("%s: %s", src, problem)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if free, err := freeBytes(filepath.Dir(part)); err == nil && free < uint64(st.Size()) {
		return fmt.Errorf("not enough free space to copy %s: it is %s and %s is free", filepath.Base(src), HumanSize(st.Size()), HumanSize(int64(free)))
	}
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	err = copyWithProgress(ctx, out, in, st.Size(), "copying "+filepath.Base(src), progress)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(part) // unlike a download, a half copy can't be resumed
	}
	return err
}

// copyWithProgress copies size bytes from in to out, reporting each whole
// percent, and stopping when ctx is cancelled.
func copyWithProgress(ctx context.Context, out io.Writer, in io.Reader, size int64, what string, progress func(InstallProgress)) error {
	buf := make([]byte, copyChunk)
	var done int64
	last := -1.0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if pct := percentOf(done, size); progress != nil && int(pct) != int(last) {
				progress(InstallProgress{Line: fmt.Sprintf("%s: %s of %s", what, HumanSize(int64(done)), HumanSize(int64(size))), Percent: pct, Bytes: done})
				last = pct
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func percentOf(done, size int64) float64 {
	if size <= 0 {
		return -1
	}
	return float64(done) * 100 / float64(size)
}

// VerifySHA256 checks the file at p against want, a lower-case digest.
func VerifySHA256(ctx context.Context, p, want string, progress func(InstallProgress)) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	h := sha256.New()
	if err := copyWithProgress(ctx, h, f, st.Size(), "checking the checksum of "+filepath.Base(p), progress); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("the checksum doesn't match: expected %s, got %s", want, got)
	}
	return nil
}

func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
