// Package upgrade replaces the running quickemu-tui binary with the latest
// GitHub release for this machine.
package upgrade

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultRepo is where releases are published.
	DefaultRepo = "fluffynuts/quickemu-tui"
	// DefaultAPI is the GitHub API root.
	DefaultAPI = "https://api.github.com"

	binaryName   = "quickemu-tui"
	maxDownload  = 200 << 20 // a release zip is a few MB; this only stops runaway downloads
	maxBinary    = 200 << 20
	apiTimeout   = 30 * time.Second
	checkTimeout = 20 * time.Second
)

// Options describe an upgrade. Only Current and ExePath are required.
type Options struct {
	Current string // the running version, e.g. "0.1.7"; "dev" or unparsable always upgrades
	ExePath string // the binary to replace

	Repo    string // default DefaultRepo
	APIBase string // default DefaultAPI
	GOOS    string // default: this machine's
	GOARCH  string
	Client  *http.Client
	Log     func(format string, args ...any)
}

// Result says what happened.
type Result struct {
	From, To string
	Replaced bool // false: already up to date
}

// AssetName is the release asset for a platform: quickemu-tui-linux-amd64.zip,
// quickemu-tui-macos-arm64.zip, ...
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		goos = "macos"
	case "linux":
	default:
		return "", fmt.Errorf("no releases are published for %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("no releases are published for %s/%s", goos, goarch)
	}
	return fmt.Sprintf("%s-%s-%s.zip", binaryName, goos, goarch), nil
}

// Compare orders dotted numeric versions ("0.1.57", with an optional leading
// v): -1, 0 or 1. ok is false if either isn't made of numbers.
func Compare(a, b string) (cmp int, ok bool) {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

func parseVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r release) asset(name string) (string, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

// Run downloads the latest release for this machine, checks it against the
// release's SHA256SUMS, makes sure it runs, and swaps it in for opts.ExePath.
// Nothing is touched unless every check passes.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.APIBase == "" {
		opts.APIBase = DefaultAPI
	}
	if opts.Client == nil {
		opts.Client = &http.Client{}
	}
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	goos, goarch := opts.GOOS, opts.GOARCH
	if goos == "" || goarch == "" {
		return Result{}, errors.New("upgrade: GOOS and GOARCH are required")
	}
	asset, err := AssetName(goos, goarch)
	if err != nil {
		return Result{}, err
	}
	res := Result{From: opts.Current}

	opts.Log("Checking for the latest release of %s…", opts.Repo)
	rel, err := latestRelease(ctx, opts)
	if err != nil {
		return res, err
	}
	res.To = strings.TrimPrefix(rel.Tag, "v")
	if cmp, ok := Compare(opts.Current, res.To); ok && cmp >= 0 {
		opts.Log("Already up to date (%s).", opts.Current)
		return res, nil
	}

	zipURL, ok := rel.asset(asset)
	if !ok {
		return res, fmt.Errorf("release %s has no %s", rel.Tag, asset)
	}
	sumsURL, ok := rel.asset("SHA256SUMS")
	if !ok {
		return res, fmt.Errorf("release %s has no SHA256SUMS, so the download can't be verified", rel.Tag)
	}

	exe, err := filepath.EvalSymlinks(opts.ExePath)
	if err != nil {
		return res, fmt.Errorf("locating the running binary: %w", err)
	}
	dir := filepath.Dir(exe)
	// the new binary is built next to the old one so the final rename is atomic
	staged, err := os.CreateTemp(dir, ".quickemu-tui-upgrade-*")
	if err != nil {
		return res, fmt.Errorf("can't write to %s (%v): run the upgrade as a user who owns it, or with sudo", dir, err)
	}
	stagedPath := staged.Name()
	defer func() { staged.Close(); os.Remove(stagedPath) }() // no-op once renamed into place

	opts.Log("Downloading %s %s…", asset, rel.Tag)
	zipFile, err := os.CreateTemp("", "quickemu-tui-*.zip")
	if err != nil {
		return res, err
	}
	defer func() { zipFile.Close(); os.Remove(zipFile.Name()) }()
	sum, err := download(ctx, opts.Client, zipURL, zipFile)
	if err != nil {
		return res, fmt.Errorf("downloading %s: %w", asset, err)
	}

	sums, err := fetchText(ctx, opts.Client, sumsURL)
	if err != nil {
		return res, fmt.Errorf("downloading SHA256SUMS: %w", err)
	}
	want, ok := checksumFor(sums, asset)
	if !ok {
		return res, fmt.Errorf("SHA256SUMS has no entry for %s", asset)
	}
	if !strings.EqualFold(want, sum) {
		return res, fmt.Errorf("checksum mismatch for %s: expected %s, got %s (nothing was changed)", asset, want, sum)
	}
	opts.Log("Checksum OK.")

	if err := extractBinary(zipFile.Name(), staged); err != nil {
		return res, err
	}
	if err := staged.Close(); err != nil {
		return res, err
	}
	mode := os.FileMode(0o755)
	if st, err := os.Stat(exe); err == nil {
		mode = st.Mode().Perm() // keep whatever the installer chose
	}
	if err := os.Chmod(stagedPath, mode); err != nil {
		return res, err
	}

	// A binary for the wrong platform, or a corrupt one, fails here, before
	// it can replace one that works.
	if err := selfCheck(ctx, stagedPath, res.To); err != nil {
		return res, fmt.Errorf("the downloaded binary doesn't work (%v); nothing was changed", err)
	}

	if err := os.Rename(stagedPath, exe); err != nil {
		return res, fmt.Errorf("replacing %s: %w", exe, err)
	}
	res.Replaced = true
	opts.Log("Upgraded %s: %s → %s", exe, orDev(opts.Current), res.To)
	return res, nil
}

func orDev(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

func latestRelease(ctx context.Context, opts Options) (release, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	url := strings.TrimRight(opts.APIBase, "/") + "/repos/" + opts.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", binaryName)
	resp, err := opts.Client.Do(req)
	if err != nil {
		return release{}, fmt.Errorf("asking GitHub for the latest release: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return release{}, fmt.Errorf("%s has no published releases yet", opts.Repo)
	case resp.StatusCode != http.StatusOK:
		return release{}, fmt.Errorf("GitHub answered %s for %s", resp.Status, url)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	if rel.Tag == "" {
		return release{}, errors.New("GitHub's answer has no release tag")
	}
	return rel, nil
}

// download copies url into w, returning the hex SHA-256 of what was written.
func download(ctx context.Context, c *http.Client, url string, w io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", binaryName)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", resp.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", err
	}
	if n > maxDownload {
		return "", fmt.Errorf("larger than %d MB, which no release should be", maxDownload>>20)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fetchText(ctx context.Context, c *http.Client, url string) (string, error) {
	var b strings.Builder
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	if _, err := download(ctx, c, url, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

// checksumFor finds name in `sha256sum` output ("<hex>  <name>").
func checksumFor(sums, name string) (string, bool) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], true
		}
	}
	return "", false
}

// extractBinary copies the quickemu-tui executable out of the release zip.
// Entry names are only matched, never used as paths, so a hostile archive
// can't write anywhere else.
func extractBinary(zipPath string, dst io.Writer) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("the download isn't a valid zip: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !f.Mode().IsRegular() || path.Base(f.Name) != binaryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		n, err := io.Copy(dst, io.LimitReader(rc, maxBinary+1))
		if err != nil {
			return err
		}
		if n > maxBinary {
			return errors.New("the binary in the zip is implausibly large")
		}
		if n == 0 {
			return errors.New("the binary in the zip is empty")
		}
		return nil
	}
	return fmt.Errorf("no %s binary found in the zip", binaryName)
}

// selfCheck runs the new binary with -version and expects the new version back.
func selfCheck(ctx context.Context, binary, wantVersion string) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "-version").Output()
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), wantVersion) {
		return fmt.Errorf("it reports %q instead of version %s", strings.TrimSpace(string(out)), wantVersion)
	}
	return nil
}
