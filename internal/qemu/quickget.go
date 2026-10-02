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
// is deliberately discarded. This can take many seconds.
func FetchCatalog(quickget string) (Catalog, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

// ReadCatalogCache loads a catalog saved by WriteCatalogCache. A missing file
// is an error the caller can ignore: the cache is only an accelerator.
func ReadCatalogCache(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cat, err := ParseCatalog(data)
	if err == nil && len(cat) == 0 {
		err = errors.New("empty catalog cache")
	}
	return cat, err
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

// Releases lists an OS's releases in quickget's order.
func (c Catalog) Releases(os string) []string {
	return c.distinct(func(e CatalogEntry) (string, bool) { return e.Release, e.OS == os })
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

// Install runs quickget in dir to download an OS and create its VM config,
// reporting progress as it goes. Cancelling ctx stops quickget and the
// downloader under it; a re-run resumes the download.
//
// It returns quickget's own output (minus progress bars) even on success,
// because quickget can exit 0 after a failed unpack; see OutputLooksFailed.
func Install(ctx context.Context, quickget, dir, os, release, edition string, progress func(InstallProgress)) (string, error) {
	cmd := exec.CommandContext(ctx, quickget, InstallArgs(os, release, edition)...)
	cmd.Dir = dir
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
