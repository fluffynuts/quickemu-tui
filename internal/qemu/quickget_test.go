package qemu

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const sampleCSV = `Display Name,OS,Release,Option,Downloader,PNG,SVG
Zorin OS,zorin,17,core,,,
Ubuntu,ubuntu,22.04,,,,
Ubuntu,ubuntu,24.04,,,,
Windows,windows,11,English (United States),,,
Windows,windows,11,"French, Canadian",,,
Alpine,alpine,3.20,standard,,,
Alpine,alpine,3.20,extended,,,
`

func TestCatalog(t *testing.T) {
	c, err := ParseCatalog([]byte(sampleCSV))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, o := range c.OSes() {
		names = append(names, o.DisplayName)
	}
	if want := []string{"Alpine", "Ubuntu", "Windows", "Zorin OS"}; !reflect.DeepEqual(names, want) {
		t.Errorf("OSes = %v, want %v", names, want)
	}
	if got, want := c.Releases("ubuntu"), []string{"22.04", "24.04"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Releases = %v, want %v", got, want)
	}
	if got := c.Editions("ubuntu", "24.04"); got != nil {
		t.Errorf("ubuntu has no editions, got %v", got)
	}
	if got, want := c.Editions("windows", "11"), []string{"English (United States)", "French, Canadian"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Editions = %v, want %v", got, want)
	}
	if got := c.DisplayName("zorin"); got != "Zorin OS" {
		t.Errorf("DisplayName = %q", got)
	}
}

func TestParseCatalogRejectsGarbage(t *testing.T) {
	if _, err := ParseCatalog([]byte("a,b\n1,2\n")); err == nil {
		t.Error("expected an error for a list without OS/Release columns")
	}
}

func TestParsePercent(t *testing.T) {
	for line, want := range map[string]float64{
		"###########                 45.3%": 45.3,
		"######################## 100.0%":   100,
		"   0.0%":                           0,
	} {
		if got, ok := ParsePercent(line); !ok || got != want {
			t.Errorf("ParsePercent(%q) = %v, %v; want %v", line, got, ok, want)
		}
	}
	for _, line := range []string{"Downloading Ubuntu 24.04", "- URL: https://x/y%20z", "150%"} {
		if _, ok := ParsePercent(line); ok {
			t.Errorf("ParsePercent(%q) matched", line)
		}
	}
}

func fakeQuickget(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "quickget")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstallReportsProgress(t *testing.T) {
	q := fakeQuickget(t, `echo "Downloading Ubuntu 24.04"
printf '##O=#  #\r#### 10.0%%\r######## 50.0%%\r################ 100.0%%\n' >&2
echo "done" > "$1-$2.conf"
`)
	dir := t.TempDir()
	var got []InstallProgress
	_, err := Install(context.Background(), q, dir, "ubuntu", "24.04", "", func(p InstallProgress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if strings.Contains(p.Line, "O=#") {
			t.Errorf("bar fragment leaked as a status line: %q", p.Line)
		}
	}
	var pcts []float64
	for _, p := range got {
		if p.Line == "" {
			pcts = append(pcts, p.Percent)
		}
	}
	if want := []float64{10, 50, 100}; !reflect.DeepEqual(pcts, want) {
		t.Errorf("percentages = %v, want %v", pcts, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "ubuntu-24.04.conf")); err != nil {
		t.Errorf("quickget didn't run in dir: %v", err)
	}
}

func TestInstallFailureIncludesOutput(t *testing.T) {
	q := fakeQuickget(t, "echo 'ERROR! no such release'; exit 3\n")
	_, err := Install(context.Background(), q, t.TempDir(), "ubuntu", "99", "", nil)
	if err == nil || !strings.Contains(err.Error(), "ERROR! no such release") {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallCancelStopsDownloaderToo(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-alive")
	q := fakeQuickget(t, "(sleep 30; touch "+marker+") &\nsleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := Install(ctx, q, t.TempDir(), "x", "1", "", nil); done <- err }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Install didn't return after cancel")
	}
}

func TestOutputLooksFailed(t *testing.T) {
	bad := "Downloading FreeDOS 1.3\n[x.zip]\n  End-of-central-directory signature not found.\nunzip:  cannot find zipfile directory\nMaking freedos-1.3.conf"
	good := "Downloading Alpine Linux v3.20\n- URL: https://dl-cdn.alpinelinux.org/x.iso\nMaking alpine-v3.20.conf\n - Setting alpine-v3.20.conf executable\n\nTo start your Alpine Linux virtual machine run:\n    quickemu --vm alpine-v3.20.conf"
	if !OutputLooksFailed(bad) {
		t.Error("unzip failure not detected")
	}
	if OutputLooksFailed(good) {
		t.Error("clean install flagged")
	}
	if !OutputLooksFailed("ERROR! something") {
		t.Error("ERROR! line not detected")
	}
}

func TestCatalogCacheRoundTripAndBadCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "catalog.csv")
	if _, err := ReadCatalogCache(p); err == nil {
		t.Error("missing cache should be an error")
	}
	if err := WriteCatalogCache(p, []byte(sampleCSV)); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCatalogCache(p)
	if err != nil || len(c.OSes()) != 4 {
		t.Fatalf("got %d OSes, err %v", len(c.OSes()), err)
	}
	// overwriting leaves no temp files behind
	if err := WriteCatalogCache(p, []byte(sampleCSV)); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(filepath.Dir(p)); len(ents) != 1 {
		t.Errorf("stray files in cache dir: %v", ents)
	}
	if err := os.WriteFile(p, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCatalogCache(p); err == nil {
		t.Error("corrupt cache should be an error")
	}
}

func TestFetchCatalogUsesQuickgetListCsv(t *testing.T) {
	q := fakeQuickget(t, `[ "$1" = "--list-csv" ] || exit 9
echo "jq: parse error: noise" >&2
cat <<'EOF'
`+sampleCSV+`EOF
`)
	c, raw, err := FetchCatalog(q)
	if err != nil || len(c.OSes()) != 4 || !strings.HasPrefix(string(raw), "Display Name,") {
		t.Fatalf("catalog=%d raw=%q err=%v", len(c.OSes()), raw, err)
	}
	if _, _, err := FetchCatalog(fakeQuickget(t, "exit 1\n")); err == nil {
		t.Error("expected an error when quickget fails")
	}
}

// fakeCurl puts a "curl" on PATH that logs its arguments and runs body, and
// shortens the retry delay. It returns the log file.
func fakeCurl(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n" + body
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := downloadRetryDelay
	downloadRetryDelay = 0
	t.Cleanup(func() { downloadRetryDelay = old })
	return log
}

// quickget's download call (web_get) and one of its page-scraping calls.
const quickgetCurls = `curl --disable --silent --location https://example.com/page > /dev/null || exit 9
if ! curl --disable --progress-bar --location --output "$1.iso" --continue-at - -- https://example.com/x.iso; then
	echo "ERROR! Failed to download with curl."
	rm -f "$1.iso"
	exit 1
fi
echo ok > "$1-$2.conf"
`

func TestInstallResumesAnInterruptedDownload(t *testing.T) {
	// fails with HTTP/2 stream error 92 twice, writing a chunk each time, then finishes
	log := fakeCurl(t, `case "$*" in *--continue-at*) ;; *) exit 0 ;; esac
out=$(echo "$*" | sed 's/.*--output \([^ ]*\).*/\1/')
printf 'chunk' >> "$out"
n=$(cat "$0.count" 2>/dev/null | wc -l)
echo x >> "$0.count"
[ "$n" -eq 0 ] && { printf '#### 36.3%%curl: (92) HTTP/2 stream 1 was not closed cleanly\n' >&2; exit 92; }
[ "$n" -eq 1 ] && { echo "curl: (7) Failed to connect to example.com port 443" >&2; exit 7; }
exit 0
`)
	dir := t.TempDir()
	q := fakeQuickget(t, quickgetCurls)
	var lines []string
	out, err := Install(context.Background(), q, dir, "bazzite", "latest", "", func(p InstallProgress) {
		if p.Line != "" {
			lines = append(lines, p.Line)
		}
	})
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "bazzite.iso"))
	if string(data) != "chunkchunkchunk" {
		t.Fatalf("iso = %q, want three resumed chunks", data)
	}
	if qemuOut := out; OutputLooksFailed(qemuOut) || strings.Contains(qemuOut, "curl: (") {
		t.Errorf("recovered curl errors kept in the output, so it looks failed:\n%s", qemuOut)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "resuming in 0s, retry 2 of 5") {
		t.Errorf("no retry notice in progress lines: %q", lines)
	}
	calls, _ := os.ReadFile(log)
	if n := strings.Count(string(calls), "--continue-at"); n != 3 {
		t.Errorf("download ran %d times, want 3:\n%s", n, calls)
	}
	if n := strings.Count(string(calls), "example.com/page"); n != 1 {
		t.Errorf("non-download call ran %d times, want 1 (no retries)", n)
	}
}

func TestInstallDoesNotRetryHardFailuresOrForever(t *testing.T) {
	for _, tc := range []struct {
		code string
		runs int
	}{
		{"22", 1}, // HTTP error such as 404: retrying won't help
		{"56", 6}, // connection reset every time: first try plus 5 retries
	} {
		log := fakeCurl(t, `case "$*" in *--continue-at*) exit `+tc.code+` ;; esac
exit 0
`)
		q := fakeQuickget(t, quickgetCurls)
		if _, err := Install(context.Background(), q, t.TempDir(), "x", "1", "", nil); err == nil {
			t.Fatalf("exit %s: install succeeded", tc.code)
		}
		calls, _ := os.ReadFile(log)
		if n := strings.Count(string(calls), "--continue-at"); n != tc.runs {
			t.Errorf("exit %s: download ran %d times, want %d", tc.code, n, tc.runs)
		}
	}
}
