package upgrade

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAssetName(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"linux", "amd64"}:  "quickemu-tui-linux-amd64.zip",
		{"linux", "arm64"}:  "quickemu-tui-linux-arm64.zip",
		{"darwin", "arm64"}: "quickemu-tui-macos-arm64.zip",
		{"darwin", "amd64"}: "quickemu-tui-macos-amd64.zip",
	} {
		if got, err := AssetName(in[0], in[1]); err != nil || got != want {
			t.Errorf("AssetName%v = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range [][2]string{{"windows", "amd64"}, {"linux", "386"}, {"freebsd", "amd64"}} {
		if _, err := AssetName(in[0], in[1]); err == nil {
			t.Errorf("AssetName%v should fail", in)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.1.2", "0.1.10", -1, true}, // numeric, not lexical
		{"0.1.57", "v0.1.57", 0, true},
		{"0.2.0", "0.1.99", 1, true},
		{"0.1", "0.1.0", 0, true},
		{"dev", "0.1.1", 0, false},
		{"", "0.1.1", 0, false},
		{"0.1.x", "0.1.1", 0, false},
	} {
		if got, ok := Compare(c.a, c.b); got != c.want || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

// fakeBinary is a shell script that behaves like `quickemu-tui -version`.
func fakeBinary(version string) []byte {
	return []byte("#!/bin/sh\necho 'quickemu-tui " + version + "'\n")
}

func makeZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sumOf(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type fakeRelease struct {
	tag      string
	zip      []byte
	sums     string // contents of SHA256SUMS; "-" means don't publish one
	status   int    // API status override
	zipHits  atomic.Int32
	asset    string
	srv      *httptest.Server
	noAssets bool
}

func newRelease(t *testing.T, tag string, zipBytes []byte) *fakeRelease {
	t.Helper()
	asset, _ := AssetName(runtime.GOOS, runtime.GOARCH)
	r := &fakeRelease{tag: tag, zip: zipBytes, asset: asset}
	r.sums = sumOf(zipBytes) + "  " + asset + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, req *http.Request) {
		if r.status != 0 {
			w.WriteHeader(r.status)
			return
		}
		assets := fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, r.asset, r.srv.URL+"/dl/"+r.asset)
		if r.sums != "-" {
			assets += fmt.Sprintf(`,{"name":"SHA256SUMS","browser_download_url":%q}`, r.srv.URL+"/dl/SHA256SUMS")
		}
		if r.noAssets {
			assets = ""
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, r.tag, assets)
	})
	mux.HandleFunc("/dl/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, r.sums) })
	mux.HandleFunc("/dl/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		r.zipHits.Add(1)
		w.Write(r.zip)
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

func installedExe(t *testing.T, version string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "quickemu-tui")
	if err := os.WriteFile(p, fakeBinary(version), 0o750); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(t *testing.T, r *fakeRelease, exe, current string) (Result, error) {
	t.Helper()
	return Run(context.Background(), Options{
		Current: current, ExePath: exe, Repo: "o/r", APIBase: r.srv.URL,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	})
}

func contentOf(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func strayFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		if e.Name() != "quickemu-tui" {
			out = append(out, e.Name())
		}
	}
	return out
}

func skipUnlessLinuxOrMac(t *testing.T) {
	t.Helper()
	if _, err := AssetName(runtime.GOOS, runtime.GOARCH); err != nil {
		t.Skip("no releases for this platform")
	}
}

func TestUpgradeReplacesBinary(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	r := newRelease(t, "v0.1.99", makeZip(t, map[string][]byte{
		"quickemu-tui-0.1.99-x/":             nil,
		"quickemu-tui-0.1.99-x/README.md":    []byte("docs"),
		"quickemu-tui-0.1.99-x/quickemu-tui": fakeBinary("0.1.99"),
	}))
	exe := installedExe(t, "0.1.2")

	res, err := run(t, r, exe, "0.1.2")
	if err != nil || !res.Replaced || res.From != "0.1.2" || res.To != "0.1.99" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got := contentOf(t, exe); !strings.Contains(got, "0.1.99") {
		t.Errorf("binary not replaced: %q", got)
	}
	if st, _ := os.Stat(exe); st.Mode().Perm()&0o100 == 0 {
		t.Errorf("not executable: %v", st.Mode())
	}
	if st, _ := os.Stat(exe); st.Mode().Perm() != 0o750 {
		t.Errorf("mode changed from 0750 to %v", st.Mode().Perm())
	}
	if s := strayFiles(t, filepath.Dir(exe)); len(s) != 0 {
		t.Errorf("temp files left behind: %v", s)
	}
}

func TestUpgradeFollowsSymlinkToTheRealBinary(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	r := newRelease(t, "v0.1.99", makeZip(t, map[string][]byte{"d/quickemu-tui": fakeBinary("0.1.99")}))
	real := installedExe(t, "0.1.2")
	link := filepath.Join(t.TempDir(), "qtui")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, r, link, "0.1.2"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}
	if !strings.Contains(contentOf(t, real), "0.1.99") {
		t.Error("the real binary wasn't upgraded")
	}
}

func TestUpgradeDoesNothingWhenCurrentOrNewer(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	r := newRelease(t, "v0.1.5", makeZip(t, map[string][]byte{"d/quickemu-tui": fakeBinary("0.1.5")}))
	for _, current := range []string{"0.1.5", "0.1.6", "0.2.0"} {
		exe := installedExe(t, current)
		res, err := run(t, r, exe, current)
		if err != nil || res.Replaced {
			t.Errorf("current %s: res = %+v, err = %v", current, res, err)
		}
		if !strings.Contains(contentOf(t, exe), current) {
			t.Errorf("current %s: binary was touched", current)
		}
	}
	if r.zipHits.Load() != 0 {
		t.Error("downloaded the zip although already up to date")
	}
}

func TestUpgradeFromDevBuildAlwaysUpgrades(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	r := newRelease(t, "v0.1.5", makeZip(t, map[string][]byte{"d/quickemu-tui": fakeBinary("0.1.5")}))
	exe := installedExe(t, "dev")
	if res, err := run(t, r, exe, "dev"); err != nil || !res.Replaced {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

// Every refusal must leave the installed binary untouched and no litter.
func TestUpgradeRefusesAndChangesNothing(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	good := makeZip(t, map[string][]byte{"d/quickemu-tui": fakeBinary("0.1.99")})
	cases := map[string]struct {
		setup func(*fakeRelease)
		zip   []byte
		want  string
	}{
		"checksum mismatch": {zip: good, want: "checksum mismatch", setup: func(r *fakeRelease) {
			r.sums = strings.Repeat("0", 64) + "  " + r.asset + "\n"
		}},
		"no SHA256SUMS published": {zip: good, want: "no SHA256SUMS", setup: func(r *fakeRelease) { r.sums = "-" }},
		"no entry for this asset": {zip: good, want: "no entry", setup: func(r *fakeRelease) {
			r.sums = sumOf(good) + "  some-other-file.zip\n"
		}},
		"asset missing":                  {zip: good, want: "has no quickemu-tui-", setup: func(r *fakeRelease) { r.noAssets = true }},
		"not a zip":                      {zip: []byte("<html>moved</html>"), want: "valid zip"},
		"zip without the binary":         {zip: makeZip(t, map[string][]byte{"d/README.md": []byte("x")}), want: "no quickemu-tui binary"},
		"binary reports another version": {zip: makeZip(t, map[string][]byte{"d/quickemu-tui": fakeBinary("9.9.9")}), want: "doesn't work"},
		"binary that can't run":          {zip: makeZip(t, map[string][]byte{"d/quickemu-tui": []byte("not an executable")}), want: "doesn't work"},
		"no releases":                    {zip: good, want: "no published releases", setup: func(r *fakeRelease) { r.status = 404 }},
		"GitHub error":                   {zip: good, want: "503", setup: func(r *fakeRelease) { r.status = 503 }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRelease(t, "v0.1.99", c.zip)
			if c.setup != nil {
				c.setup(r)
			}
			exe := installedExe(t, "0.1.2")
			res, err := run(t, r, exe, "0.1.2")
			if err == nil || !strings.Contains(err.Error(), c.want) || res.Replaced {
				t.Fatalf("want an error containing %q; got res=%+v err=%v", c.want, res, err)
			}
			if got := contentOf(t, exe); !strings.Contains(got, "0.1.2") {
				t.Errorf("installed binary was changed: %q", got)
			}
			if s := strayFiles(t, filepath.Dir(exe)); len(s) != 0 {
				t.Errorf("temp files left behind: %v", s)
			}
		})
	}
}

func TestUpgradeIgnoresHostileZipEntryNames(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	os.MkdirAll(exeDir, 0o755)
	exe := filepath.Join(exeDir, "quickemu-tui")
	os.WriteFile(exe, fakeBinary("0.1.2"), 0o755)
	// an entry that would land outside exeDir if names were trusted as paths
	r := newRelease(t, "v0.1.99", makeZip(t, map[string][]byte{"../../evil/quickemu-tui": fakeBinary("0.1.99")}))
	if _, err := run(t, r, exe, "0.1.2"); err != nil {
		t.Fatal(err)
	}
	var found []string
	filepath.Walk(root, func(p string, i os.FileInfo, _ error) error {
		if !i.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	if len(found) != 1 || found[0] != exe {
		t.Errorf("files after upgrade = %v, want only %s", found, exe)
	}
}

func TestUpgradeExplainsAnUnwritableDirectory(t *testing.T) {
	skipUnlessLinuxOrMac(t)
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	r := newRelease(t, "v0.1.99", makeZip(t, map[string][]byte{"d/quickemu-tui": fakeBinary("0.1.99")}))
	exe := installedExe(t, "0.1.2")
	dir := filepath.Dir(exe)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err := run(t, r, exe, "0.1.2")
	if err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("err = %v, want advice about sudo", err)
	}
	if r.zipHits.Load() != 0 {
		t.Error("downloaded before discovering the directory is read-only")
	}
}
