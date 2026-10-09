package qemu

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CatalogEntry is one installable combination known to quickget.
type CatalogEntry struct {
	DisplayName string
	OS          string
	Release     string
	Edition     string // quickget's "Option"; often empty
}

// Catalog is everything quickget can install, in quickget's own order.
type Catalog []CatalogEntry

// OSChoice is one operating system in the catalog.
type OSChoice struct {
	ID          string
	DisplayName string
}

// FindQuickget locates quickget: next to an explicit quickemu path if one was
// given, otherwise on PATH.
func FindQuickget(quickemuOverride string) (string, error) {
	if quickemuOverride != "" {
		sibling := filepath.Join(filepath.Dir(quickemuOverride), "quickget")
		if st, err := os.Stat(sibling); err == nil && !st.IsDir() {
			return sibling, nil
		}
	}
	return exec.LookPath("quickget")
}

// FetchCatalog asks quickget what it can install, returning the parsed list and
// the raw CSV (for caching). quickget's stderr is noisy (stray jq errors) and
// is deliberately discarded. This takes minutes: quickget asks every
// distribution's website for its releases.
func FetchCatalog(quickget string) (Catalog, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, quickget, "--list-csv")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("quickget --list-csv: %w", err)
	}
	cat, err := ParseCatalog(out.Bytes())
	return cat, out.Bytes(), err
}

// CacheTTL is how long the cached catalog and release dates are used before
// they're refreshed. quickget's list and release dates change rarely.
const CacheTTL = 7 * 24 * time.Hour

// ReadCatalogCache loads a catalog saved by WriteCatalogCache, and says
// whether it's older than CacheTTL. A missing file is an error the caller
// can ignore: the cache is only an accelerator.
func ReadCatalogCache(path string) (cat Catalog, stale bool, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	cat, err = ParseCatalog(data)
	if err == nil && len(cat) == 0 {
		err = errors.New("empty catalog cache")
	}
	return cat, time.Since(st.ModTime()) >= CacheTTL, err
}

// WriteCatalogCache saves quickget's CSV, replacing the old file atomically so
// a crash can't leave a truncated cache.
func WriteCatalogCache(path string, csv []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(csv)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// ParseCatalog decodes `quickget --list-csv` output.
func ParseCatalog(data []byte) (Catalog, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing quickget list: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("quickget returned no list")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimSpace(h)] = i
	}
	for _, need := range []string{"OS", "Release"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("quickget list has no %q column", need)
		}
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var cat Catalog
	for _, row := range rows[1:] {
		e := CatalogEntry{DisplayName: get(row, "Display Name"), OS: get(row, "OS"), Release: get(row, "Release"), Edition: get(row, "Option")}
		if e.OS == "" || e.Release == "" {
			continue
		}
		if e.DisplayName == "" {
			e.DisplayName = e.OS
		}
		cat = append(cat, e)
	}
	return cat, nil
}

// OSes lists the operating systems, sorted by display name.
func (c Catalog) OSes() []OSChoice {
	seen := map[string]bool{}
	var out []OSChoice
	for _, e := range c {
		if !seen[e.OS] {
			seen[e.OS] = true
			out = append(out, OSChoice{ID: e.OS, DisplayName: e.DisplayName})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
	})
	return out
}

// Releases lists an OS's releases, newest first as far as can be told from
// the names alone. quickget's own order is no help: --list-csv sorts its
// rows, so a release list arrives alphabetical, not chronological.
func (c Catalog) Releases(os string) []string {
	rels := c.distinct(func(e CatalogEntry) (string, bool) { return e.Release, e.OS == os })
	sort.SliceStable(rels, func(i, j int) bool { return compareRelease(rels[i], rels[j]) > 0 })
	return rels
}

var releaseChunkRe = regexp.MustCompile(`\d+|\D+`)

// compareRelease orders release names naturally: digit runs compare as
// numbers ("9" < "10"), and a number sorts above a word in the same place, so
// "24.04" outranks "daily-live". Words compare case-insensitively, which suits
// alphabetical code names (Devuan's chimaera < daedalus).
func compareRelease(a, b string) int {
	ac := releaseChunkRe.FindAllString(strings.ToLower(a), -1)
	bc := releaseChunkRe.FindAllString(strings.ToLower(b), -1)
	for i := 0; i < len(ac) && i < len(bc); i++ {
		x, y := ac[i], bc[i]
		xn, yn := x[0] >= '0' && x[0] <= '9', y[0] >= '0' && y[0] <= '9'
		switch {
		case xn && yn:
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if len(x) != len(y) {
				return len(x) - len(y)
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		case xn != yn:
			if xn {
				return 1
			}
			return -1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	return len(ac) - len(bc)
}

// Editions lists the editions of an OS release. A release without editions
// yields nil.
func (c Catalog) Editions(os, release string) []string {
	eds := c.distinct(func(e CatalogEntry) (string, bool) { return e.Edition, e.OS == os && e.Release == release })
	if len(eds) == 1 && eds[0] == "" {
		return nil
	}
	return eds
}

func (c Catalog) distinct(pick func(CatalogEntry) (string, bool)) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range c {
		if v, ok := pick(e); ok && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// DisplayName returns the pretty name for an OS id.
func (c Catalog) DisplayName(os string) string {
	for _, e := range c {
		if e.OS == os {
			return e.DisplayName
		}
	}
	return os
}

// barNoiseRe matches curl progress-bar fragments that carry no percentage.
var barNoiseRe = regexp.MustCompile(`^[#=O>. -]*#[#=O>. -]*$`)

var percentRe = regexp.MustCompile(`(?:^|\s)(\d{1,3}(?:\.\d+)?)%\s*$`)

// ParsePercent extracts curl's progress-bar percentage from a line such as
// "######## 45.3%".
func ParsePercent(line string) (float64, bool) {
	m := percentRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v > 100 {
		return 0, false
	}
	return v, true
}

// splitProgress is a bufio.SplitFunc that ends tokens at \r as well as \n:
// curl redraws its progress bar with carriage returns.
func splitProgress(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// InstallProgress is one update from a running install.
type InstallProgress struct {
	Line    string  // the latest line of quickget output; empty for pure progress-bar updates
	Percent float64 // 0-100, or -1 when quickget isn't reporting a percentage
}

// InstallArgs is quickget's command line for an install.
func InstallArgs(os, release, edition string) []string {
	args := []string{os, release}
	if edition != "" {
		args = append(args, edition)
	}
	return args
}

const outputTailLines = 40

// Download retries: curl exit codes for a dropped or stalled connection, which
// are worth resuming, rather than e.g. a 404 or a full disk.
const (
	retryableCurlCodes = "7|16|18|28|35|52|55|56|92"
	retryNotice        = "quickemu-tui: download interrupted"
)

var (
	downloadRetries    = 5
	downloadRetryDelay = 5 // seconds; tests shorten it
)

// curlShim stands in for curl on quickget's PATH. quickget deletes a partial
// ISO as soon as curl fails, and runs curl with --disable so ~/.curlrc can't
// add retries. Its downloads all pass --continue-at -, so rerunning the same
// command carries on from the end of the partial file. Other calls (page
// scraping, redirect checks) go straight to the real curl.
const curlShim = `#!/bin/sh
# quickemu-tui: resume interrupted quickget downloads
real=%s
case " $* " in
*" --continue-at "*) ;;
*) exec "$real" "$@" ;;
esac
n=0
while :; do
	"$real" "$@"
	rc=$?
	case $rc in
	0) exit 0 ;;
	%s) ;;
	*) exit $rc ;;
	esac
	n=$((n + 1))
	if [ $n -gt %d ]; then
		exit $rc
	fi
	echo "%s (curl exit $rc); resuming in %ds, retry $n of %d" >&2
	sleep %d
done
`

// writeCurlShim puts a curl wrapper in a new temp directory and returns it.
// It fails (and quickget uses curl directly) if there's no curl to wrap.
func writeCurlShim() (string, error) {
	real, err := exec.LookPath("curl")
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "quickemu-tui-curl-")
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(curlShim, shellQuote(real), retryableCurlCodes, downloadRetries, retryNotice, downloadRetryDelay, downloadRetries, downloadRetryDelay)
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// shellQuote single-quotes s for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// withPathFirst returns env with dir at the front of PATH.
func withPathFirst(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	path := ""
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			path = strings.TrimPrefix(e, "PATH=")
			continue
		}
		out = append(out, e)
	}
	if path != "" {
		dir += string(filepath.ListSeparator) + path
	}
	return append(out, "PATH="+dir)
}

// indirection so Install's parameter named os doesn't shadow the package
var (
	environ   = os.Environ
	removeAll = os.RemoveAll
)

// Install runs quickget in dir to download an OS and create its VM config,
// reporting progress as it goes. Cancelling ctx stops quickget and the
// downloader under it; a re-run resumes the download.
//
// It returns quickget's own output (minus progress bars) even on success,
// because quickget can exit 0 after a failed unpack; see OutputLooksFailed.
func Install(ctx context.Context, quickget, dir, os, release, edition string, progress func(InstallProgress)) (string, error) {
	cmd := exec.CommandContext(ctx, quickget, InstallArgs(os, release, edition)...)
	cmd.Dir = dir
	if shimDir, err := writeCurlShim(); err == nil {
		defer removeAll(shimDir)
		cmd.Env = withPathFirst(environ(), shimDir)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // so we can stop curl too
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second

	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var tail []string
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(splitProgress)
		last := -1.0
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if pct, ok := ParsePercent(line); ok {
				if pct != last && progress != nil {
					progress(InstallProgress{Percent: pct})
				}
				last = pct
				continue
			}
			if barNoiseRe.MatchString(line) {
				continue // curl's half-drawn bar, e.g. "##O=#  #"
			}
			if strings.HasPrefix(line, retryNotice) {
				// the curl error just before it was dealt with; keeping it
				// would make a resumed download look failed (OutputLooksFailed)
				for len(tail) > 0 && strings.Contains(tail[len(tail)-1], "curl: (") {
					tail = tail[:len(tail)-1]
				}
			}
			tail = append(tail, line)
			if len(tail) > outputTailLines {
				tail = tail[1:]
			}
			if progress != nil {
				progress(InstallProgress{Line: line, Percent: last})
			}
		}
	}()

	waitErr := cmd.Wait()
	_ = pw.Close()
	<-scanDone

	output := strings.Join(tail, "\n")
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	if waitErr != nil {
		return output, fmt.Errorf("quickget %s failed: %v\n\n%s", strings.Join(InstallArgs(os, release, edition), " "), waitErr, output)
	}
	return output, nil
}

var failedOutputRe = regexp.MustCompile(`(?im)^\s*(error|warning)\b|\bcannot\b|\bunable to\b|not a zipfile|\bfailed\b|no such file`)

// OutputLooksFailed reports whether quickget's output contains problems even
// though it exited 0. quickget carries on to write a .conf after, say, an
// unzip failure, which would otherwise look like a successful install.
func OutputLooksFailed(output string) bool { return failedOutputRe.MatchString(output) }
