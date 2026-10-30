package qemu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var time0 = time.Unix(0, 0)

func TestISOFileNameAndSuggestedVMName(t *testing.T) {
	for source, want := range map[string]struct{ file, name string }{
		"https://mirror.example.org/pub/distro-1.2-amd64.iso":          {"distro-1.2-amd64.iso", "distro-1.2-amd64"},
		"https://mirror.example.org/pub/My%20Distro.iso?mirror=3#frag": {"My_Distro.iso", "My_Distro"},
		"http://host/download/":                 {"download", "download"},
		"https://host/":                         {"installer.iso", "installer"},
		"/home/u/Downloads/some distro (1).iso": {"some_distro__1_.iso", "some_distro__1_"},
		"~/dl/.hidden.iso":                      {"hidden.iso", "hidden"},
		"/x/odd+name.img":                       {"odd+name.img", "odd-name"},
	} {
		if got := ISOFileName(source); got != want.file {
			t.Errorf("ISOFileName(%q) = %q, want %q", source, got, want.file)
		}
		got := SuggestVMName(source)
		if got != want.name {
			t.Errorf("SuggestVMName(%q) = %q, want %q", source, got, want.name)
		}
		if err := ValidateVMName(got); err != nil {
			t.Errorf("SuggestVMName(%q) = %q is not a valid VM name: %v", source, got, err)
		}
	}
}

func TestIsURLAndCheckURL(t *testing.T) {
	for source, want := range map[string]bool{"https://a/b.iso": true, "HTTP://a/b.iso": true, "ftp://a/b.iso": false, "file:///etc/x": false, "/tmp/a.iso": false, "~/a.iso": false} {
		if IsURL(source) != want {
			t.Errorf("IsURL(%q) = %v", source, !want)
		}
	}
	if CheckURL("https://example.org/a.iso") != "" || CheckURL("https:///a.iso") == "" {
		t.Error("CheckURL: want a host to be required")
	}
}

func TestNormalizeSHA256TakesADigestOrASumsLine(t *testing.T) {
	digest := strings.Repeat("ab12", 16)
	for name, in := range map[string]string{
		"bare":      digest,
		"upper":     strings.ToUpper(digest),
		"sums line": digest + " *distro-1.2-amd64.iso",
		"labelled":  "SHA256: " + digest + "\n",
	} {
		got, err := NormalizeSHA256(in)
		if err != nil || got != digest {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
	for _, bad := range []string{"", "not a digest", digest[:63], digest + "0"} {
		if _, err := NormalizeSHA256(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestCreateCustomVMWritesExplicitSettingsAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	c := CustomISO{Name: "mydistro", GuestOS: "linux", Boot: "efi", DiskSize: "32G"}
	path, err := CreateCustomVM(root, c, "mydistro.iso")
	if err != nil {
		t.Fatal(err)
	}
	conf, err := VM{ConfPath: path}.Conf()
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"guest_os": "linux", "boot": "efi", "disk_size": "32G",
		"disk_img": "mydistro/disk.qcow2", "iso": "mydistro/mydistro.iso",
	} {
		if conf[k] != want {
			t.Errorf("%s = %q, want %q", k, conf[k], want)
		}
	}
	if _, err := CreateCustomVM(root, c, "other.iso"); err == nil {
		t.Error("an existing .conf was overwritten")
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "mydistro.iso") {
		t.Errorf("the .conf changed: %s", got)
	}
}

func progressLog() (func(InstallProgress), *[]InstallProgress) {
	var got []InstallProgress
	return func(p InstallProgress) { got = append(got, p) }, &got
}

func TestFetchISOCopiesALocalFileWithProgress(t *testing.T) {
	src := filepath.Join(t.TempDir(), "distro.iso")
	fakeISO(t, src, "CD001")
	dir := filepath.Join(t.TempDir(), "vm")
	progress, got := progressLog()

	res, err := FetchISO(context.Background(), src, dir, progress)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Copied || res.From != src || res.Path != filepath.Join(dir, "distro.iso") {
		t.Errorf("result %+v", res)
	}
	want, _ := os.ReadFile(src)
	if have, _ := os.ReadFile(res.Path); !bytes.Equal(have, want) {
		t.Error("the copy differs from the original")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("the original is gone: %v", err)
	}
	if _, err := os.Stat(res.Path + ".part"); !os.IsNotExist(err) {
		t.Errorf("a .part file is left: %v", err)
	}
	if n := len(*got); n == 0 || (*got)[n-1].Percent != 100 {
		t.Errorf("progress %v doesn't end at 100%%", *got)
	}
}

func TestFetchISOUsesAnImageThatIsAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "distro.iso")
	writeFile(t, existing, "already here")
	res, err := FetchISO(context.Background(), "https://127.0.0.1:1/distro.iso", dir, nil)
	if err != nil || res.Copied {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := readFile(t, existing); got != "already here" {
		t.Errorf("the image was replaced: %q", got)
	}
}

func TestFetchISORefusesALocalFileThatIsNotAnISO(t *testing.T) {
	src := filepath.Join(t.TempDir(), "page.iso")
	writeFile(t, src, "<html>moved</html>")
	dir := t.TempDir()
	if _, err := FetchISO(context.Background(), src, dir, nil); err == nil {
		t.Fatal("copied a web page as an ISO")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left %d file(s) behind", len(entries))
	}
}

func TestFetchISOStopsWhenCancelled(t *testing.T) {
	src := filepath.Join(t.TempDir(), "distro.iso")
	fakeISO(t, src, "CD001")
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchISO(ctx, src, dir, nil); err == nil {
		t.Fatal("a cancelled copy succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left %d file(s) behind", len(entries))
	}
}

func needCurl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
}

func TestFetchISODownloadsAndResumesAPartialFile(t *testing.T) {
	needCurl(t)
	body := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	copy(body[16*isoSectorSize:], "\x01CD001")                // what makes it an ISO
	var mu sync.Mutex
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		http.ServeContent(w, r, "distro.iso", time0, bytes.NewReader(body)) // honours Range
	}))
	defer srv.Close()
	dir := t.TempDir()
	partial := filepath.Join(dir, "distro.iso.part")
	if err := os.WriteFile(partial, body[:300_000], 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := FetchISO(context.Background(), srv.URL+"/pub/distro.iso", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied {
		t.Error("a download is not a copy")
	}
	if have, _ := os.ReadFile(res.Path); !bytes.Equal(have, body) {
		t.Errorf("downloaded %d bytes, want the %d served", len(have), len(body))
	}
	// what tells a resume from a restart: it asked for the rest, not for everything
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 1 || ranges[0] != "bytes=300000-" {
		t.Errorf("the server was asked for %q, want one request for bytes=300000-", ranges)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Errorf("the .part file is left: %v", err)
	}
}

func TestFetchISODownloadFailureLeavesNoImage(t *testing.T) {
	needCurl(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dir := t.TempDir()
	if _, err := FetchISO(context.Background(), srv.URL+"/gone.iso", dir, nil); err == nil {
		t.Fatal("a 404 counted as a download")
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.iso")); !os.IsNotExist(err) {
		t.Errorf("an image exists after a failed download: %v", err)
	}
}

func TestVerifySHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.iso")
	writeFile(t, p, "some image")
	sum := sha256.Sum256([]byte("some image"))
	good := hex.EncodeToString(sum[:])
	if err := VerifySHA256(context.Background(), p, good, nil); err != nil {
		t.Errorf("the right digest was refused: %v", err)
	}
	err := VerifySHA256(context.Background(), p, strings.Repeat("0", 64), nil)
	if err == nil || !strings.Contains(err.Error(), good) {
		t.Errorf("a wrong digest gave %v, want a mismatch naming the real one", err)
	}
}

// isoBody is a megabyte that passes for an ISO.
func isoBody() []byte {
	body := bytes.Repeat([]byte("0123456789abcdef"), 64*1024)
	copy(body[16*isoSectorSize:], "\x01CD001")
	return body
}

// redirectingServer serves an ISO the way SourceForge does: the link a browser
// follows ("…/files/1.2/distro-1.2.iso/download") redirects, twice, to a
// mirror whose path ends in the real file name and whose query is a token.
func redirectingServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/files/1.2/distro-1.2.iso/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hop?use_mirror=master", http.StatusFound)
	})
	mux.HandleFunc("/hop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/project/distro/1.2/distro-1.2.iso?e=12345&st=token", http.StatusFound)
	})
	mux.HandleFunc("/project/distro/1.2/distro-1.2.iso", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "d.iso", time0, bytes.NewReader(body))
	})
	mux.HandleFunc("/files/latest/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/project/distro/1.2/distro-1.2.iso?e=1", http.StatusFound)
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><html><head><title>Download</title></head><body>click here</body></html>"))
	})
	mux.HandleFunc("/sums/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sums.txt?token=1", http.StatusFound)
	})
	mux.HandleFunc("/sums.txt", func(w http.ResponseWriter, r *http.Request) {
		sum := sha256.Sum256(body)
		_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  other.iso\n" + hex.EncodeToString(sum[:]) + " *distro-1.2.iso\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchISOFollowsRedirectsAndNamesTheImageFromWhereItLands(t *testing.T) {
	needCurl(t)
	body := isoBody()
	srv := redirectingServer(t, body)
	for name, link := range map[string]string{
		"the link a browser follows": srv.URL + "/files/1.2/distro-1.2.iso/download",
		"the latest-file link":       srv.URL + "/files/latest/download",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			res, err := FetchISO(context.Background(), link, dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.File != "distro-1.2.iso" || res.Path != filepath.Join(dir, "distro-1.2.iso") {
				t.Errorf("named %q at %q, want distro-1.2.iso", res.File, res.Path)
			}
			if have, _ := os.ReadFile(res.Path); !bytes.Equal(have, body) {
				t.Error("the image differs from what was served")
			}
			// the image, and the note of what it was downloaded from (not a .part or the headers)
			for _, e := range mustReadDir(t, dir) {
				if e != "distro-1.2.iso" && e != fetchRecord {
					t.Errorf("unexpected file %q left in the folder", e)
				}
			}
		})
	}
}

func TestFetchISORefusesAWebPageAndLeavesNothingToResume(t *testing.T) {
	needCurl(t)
	srv := redirectingServer(t, isoBody())
	dir := t.TempDir()
	_, err := FetchISO(context.Background(), srv.URL+"/page", dir, nil)
	if err == nil || !strings.Contains(err.Error(), "HTML web page") || !strings.Contains(err.Error(), "download button") {
		t.Fatalf("err %v, want a web page explained", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left %d file(s) behind", len(entries))
	}
}

func TestFetchTextFollowsRedirects(t *testing.T) {
	needCurl(t)
	srv := redirectingServer(t, isoBody())
	text, err := FetchText(srv.URL+"/sums/download", 1<<20)
	if err != nil || !strings.Contains(text, "distro-1.2.iso") {
		t.Fatalf("%q, %v", text, err)
	}
	if _, err := FetchText(srv.URL+"/nothing-here", 1<<20); err == nil {
		t.Error("a 404 counted as a checksum file")
	}
}

func TestPickSHA256ChoosesTheLineForTheImage(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	listing := a + "  other.iso\n" + b + " *distro-1.2.iso\n"
	for name, tc := range map[string]struct{ text, file, want string }{
		"a listing":              {listing, "distro-1.2.iso", b},
		"a listing, other image": {listing, "other.iso", a},
		"one bare digest":        {a + "\n", "whatever.iso", a},
		"one digest and a name":  {"SHA256 (distro.iso) = " + strings.ToUpper(b), "x.iso", b},
		"a path in the listing":  {a + "  ./dl/distro.iso\n" + b + "  ./dl/x.iso\n", "distro.iso", a},
	} {
		got, err := PickSHA256(tc.text, tc.file)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, tc.want)
		}
	}
	for name, tc := range map[string]struct{ text, file string }{
		"no digest at all":     {"nothing to see\n", "x.iso"},
		"a listing without it": {listing, "missing.iso"},
	} {
		if got, err := PickSHA256(tc.text, tc.file); err == nil {
			t.Errorf("%s: got %q, want an error", name, got)
		}
	}
}

func TestSourceForgeProjectPagesBecomeTheLatestDownload(t *testing.T) {
	for in, want := range map[string]string{
		"https://sourceforge.net/projects/sloth-os/":                         "https://sourceforge.net/projects/sloth-os/files/latest/download",
		"https://sourceforge.net/projects/sloth-os":                          "https://sourceforge.net/projects/sloth-os/files/latest/download",
		"https://sourceforge.net/projects/sloth-os/files/":                   "https://sourceforge.net/projects/sloth-os/files/latest/download",
		"https://sourceforge.net/projects/sloth-os/files/1.2/x.iso/download": "https://sourceforge.net/projects/sloth-os/files/1.2/x.iso/download",
		"https://example.org/projects/sloth-os/":                             "https://example.org/projects/sloth-os/",
		"/home/u/x.iso":                                                      "/home/u/x.iso",
	} {
		if got := NormalizeDownloadURL(in); got != want {
			t.Errorf("NormalizeDownloadURL(%q) = %q, want %q", in, got, want)
		}
	}
	for link, want := range map[string]string{
		"https://sourceforge.net/projects/sloth-os/files/1.2/sloth-os-1.2-x86_64.iso/download": "sloth-os-1.2-x86_64",
		"https://sourceforge.net/projects/sloth-os/files/latest/download":                      "sloth-os",
	} {
		if got := SuggestVMName(link); got != want {
			t.Errorf("SuggestVMName(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestPollSizeReportsGrowthAndStallsUntilStopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.part")
	writeFile(t, path, "12345")
	sizes := make(chan int64, 16)
	stop := pollSize(context.Background(), path, 5*time.Millisecond, func(n int64) { sizes <- n })
	if got := <-sizes; got != 5 {
		t.Errorf("first report %d, want 5", got)
	}
	if got := <-sizes; got != 5 {
		t.Errorf("a stalled file was reported as %d, want 5 again", got)
	}
	writeFile(t, path, "1234567890")
	for got := <-sizes; got != 10; got = <-sizes {
	}
	stop() // returns only once the poller has exited, so nothing reports after it
}

func TestDownloadKeepsItsBookkeepingLineOutOfTheProgress(t *testing.T) {
	needCurl(t)
	srv := redirectingServer(t, isoBody())
	var mu sync.Mutex
	var got []InstallProgress
	progress := func(p InstallProgress) {
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}
	if _, err := FetchISO(context.Background(), srv.URL+"/files/latest/download", t.TempDir(), progress); err != nil {
		t.Fatal(err)
	}
	for _, p := range got {
		if strings.HasPrefix(p.Line, finalMarker) {
			t.Errorf("the bookkeeping line leaked into the progress: %+v", p)
		}
	}
}

func TestCopyReportsBytes(t *testing.T) {
	src := filepath.Join(t.TempDir(), "d.iso")
	fakeISO(t, src, "CD001")
	st, _ := os.Stat(src)
	progress, got := progressLog()
	if _, err := FetchISO(context.Background(), src, t.TempDir(), progress); err != nil {
		t.Fatal(err)
	}
	if n := len(*got); n == 0 || (*got)[n-1].Bytes != st.Size() {
		t.Errorf("progress %+v doesn't end at %d bytes", *got, st.Size())
	}
}

func TestCurlOnlySpeaksHTTPAndAnHTTPSAddressStaysHTTPS(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.org/a.iso": "--proto =http,https --proto-redir =https",
		"HTTPS://example.org/a.iso": "--proto =http,https --proto-redir =https",
		"http://example.org/a.iso":  "--proto =http,https --proto-redir =http,https",
	} {
		if got := strings.Join(curlProtocolArgs(in), " "); got != want {
			t.Errorf("curlProtocolArgs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestARedirectToAnotherProtocolIsRefused(t *testing.T) {
	needCurl(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "ftp://127.0.0.1:1/distro.iso", http.StatusFound)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if _, err := FetchISO(context.Background(), srv.URL+"/go", dir, nil); err == nil {
		t.Error("a redirect to ftp was followed")
	}
	if _, err := FetchText(srv.URL+"/go", 1<<20); err == nil {
		t.Error("a redirect to ftp was followed for a checksum")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left %d file(s) behind", len(entries))
	}
}

func TestFetchTextRefusesMoreThanItWasToldToExpectEvenWhenTheLengthIsNotSent(t *testing.T) {
	needCurl(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		for i := 0; i < 64; i++ { // chunked: no Content-Length for curl to check
			_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
			fl.Flush()
		}
	}))
	defer srv.Close()
	_, err := FetchText(srv.URL, 4096)
	if err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("err %v, want it refused as too big", err)
	}
	if text, err := FetchText(srv.URL, 1<<20); err != nil || len(text) != 64*1024 {
		t.Errorf("within the limit: %d bytes, %v", len(text), err)
	}
}

func TestTotalFromHeadersTakesTheLastSuccessfulResponse(t *testing.T) {
	for name, tc := range map[string]struct {
		headers string
		want    int64
	}{
		"a redirect then the file":                    {"HTTP/2 302\r\nlocation: /x\r\n\r\nHTTP/2 200\r\ncontent-length: 5190336512\r\n\r\n", 5190336512},
		"a resumed partial reply":                     {"HTTP/1.1 206 Partial Content\r\nContent-Range: bytes 100-999/1000\r\nContent-Length: 900\r\n\r\n", 1000},
		"a proxy first":                               {"HTTP/1.0 200 Connection established\r\n\r\nHTTP/1.1 200 OK\r\nContent-Length: 42\r\n\r\n", 42},
		"headers in any case":                         {"HTTP/2 200\r\nCONTENT-LENGTH: 77\r\n\r\n", 77},
		"no length (chunked)":                         {"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n", 0},
		"a partial reply that doesn't know the total": {"HTTP/1.1 206 Partial Content\r\nContent-Range: bytes 100-999/*\r\nContent-Length: 900\r\n\r\n", 0},
		"nothing": {"", 0},
	} {
		if got := totalFromHeaders(tc.headers); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestADownloadReportsTheExactTotalAndNeverACompletePercentWhileRunning(t *testing.T) {
	needCurl(t)
	old := sizePollInterval
	sizePollInterval = 20 * time.Millisecond
	t.Cleanup(func() { sizePollInterval = old })
	body := isoBody()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := int64(0)
		if rng := r.Header.Get("Range"); rng != "" {
			_, _ = fmt.Sscanf(rng, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.Header().Set("Content-Length", fmt.Sprint(int64(len(body))-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		}
		fl := w.(http.Flusher)
		for off := start; off < int64(len(body)); off += 64 * 1024 { // slowly, so there's time to poll
			end := min(off+64*1024, int64(len(body)))
			_, _ = w.Write(body[off:end])
			fl.Flush()
			time.Sleep(15 * time.Millisecond)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.iso.part"), body[:400_000], 0o644); err != nil { // resuming
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []InstallProgress
	progress := func(p InstallProgress) {
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}
	if _, err := FetchISO(context.Background(), srv.URL+"/d.iso", dir, progress); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	sawTotal := false
	for _, p := range got {
		if p.Percent >= 100 {
			t.Errorf("a complete percentage was reported while downloading: %+v", p)
		}
		if p.Total != 0 {
			sawTotal = true
			if p.Total != int64(len(body)) {
				t.Errorf("total %d, want the whole file's %d, not the part being fetched", p.Total, len(body))
			}
		}
	}
	if !sawTotal {
		t.Errorf("no total was reported: %+v", got)
	}
}

func TestARetryAfterAFailureFollowingTheDownloadDoesNotDownloadAgain(t *testing.T) {
	needCurl(t)
	body := isoBody()
	var mu sync.Mutex
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/get/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mirror/distro-1.2.iso?token=1", http.StatusFound)
	})
	mux.HandleFunc("/mirror/distro-1.2.iso", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.ServeContent(w, r, "d.iso", time0, bytes.NewReader(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	link := srv.URL + "/get/download"
	dir := t.TempDir()

	first, err := FetchISO(context.Background(), link, dir, nil)
	if err != nil || first.File != "distro-1.2.iso" {
		t.Fatalf("%+v, %v", first, err)
	}
	// something after that fails (say, the checksum file didn't list the image): the user tries again
	second, err := FetchISO(context.Background(), link, dir, nil)
	if err != nil || second.File != "distro-1.2.iso" || second.Path != first.Path {
		t.Fatalf("%+v, %v", second, err)
	}
	if hits != 1 {
		t.Errorf("the image was downloaded %d times, want once", hits)
	}

	// a different address isn't taken for the same download
	other := srv.URL + "/mirror/distro-1.2.iso?other=1"
	if _, err := FetchISO(context.Background(), other, dir, nil); err != nil {
		t.Fatal(err)
	}

	// and once the VM is made, the record goes
	ForgetFetch(dir)
	if _, ok := rememberedFetch(dir, link); ok {
		t.Error("the record outlived ForgetFetch")
	}
}

func TestARecordedDownloadWhoseImageIsGoneIsDownloadedAgain(t *testing.T) {
	dir := t.TempDir()
	rememberFetch(dir, "https://example.org/get/download", "distro.iso")
	if _, ok := rememberedFetch(dir, "https://example.org/get/download"); ok {
		t.Error("used a record whose image doesn't exist")
	}
	writeFile(t, filepath.Join(dir, "distro.iso"), "x")
	if f, ok := rememberedFetch(dir, "https://example.org/get/download"); !ok || f != "distro.iso" {
		t.Errorf("got %q, %v", f, ok)
	}
	// a record can't point out of the folder
	rememberFetch(dir, "https://example.org/x", "../escape.iso")
	if _, ok := rememberedFetch(dir, "https://example.org/x"); ok {
		t.Error("followed a record out of the folder")
	}
}

func TestSourceForgeHostsAreMatchedByNameNotBySuffix(t *testing.T) {
	for in, want := range map[string]string{
		"https://evilsourceforge.net/projects/x/":          "https://evilsourceforge.net/projects/x/",
		"https://sourceforge.net.evil.example/projects/x/": "https://sourceforge.net.evil.example/projects/x/",
		"https://www.sourceforge.net/projects/x/":          "https://sourceforge.net/projects/x/files/latest/download",
		"https://SourceForge.net/projects/x/":              "https://sourceforge.net/projects/x/files/latest/download",
	} {
		if got := NormalizeDownloadURL(in); got != want {
			t.Errorf("NormalizeDownloadURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickSHA256ReadsBSDStyleListingsToo(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	listing := "SHA256 (other.iso) = " + a + "\nSHA256 (distro-1.2.iso) = " + b + "\n"
	if got, err := PickSHA256(listing, "distro-1.2.iso"); err != nil || got != b {
		t.Errorf("got %q, %v; want %q", got, err, b)
	}
	if got, err := PickSHA256(listing, "other.iso"); err != nil || got != a {
		t.Errorf("got %q, %v; want %q", got, err, a)
	}
	if _, err := PickSHA256(listing, "missing.iso"); err == nil {
		t.Error("a listing without the image was accepted")
	}
}

func TestInstallReturnsAFailureToStartQuickgetAsItIs(t *testing.T) {
	_, err := Install(context.Background(), filepath.Join(t.TempDir(), "no-such-quickget"), t.TempDir(), "ubuntu", "24.04", "", nil)
	var pathErr *os.PathError
	if err == nil || !errors.As(err, &pathErr) || strings.Contains(err.Error(), "failed:") {
		t.Errorf("err %v (%T), want the error from starting the process, not wrapped as quickget failing", err, err)
	}
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestLimitedBufferStopsTheWriterAtItsLimit(t *testing.T) {
	b := &limitedBuffer{max: 10}
	if _, err := b.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("678901")); err == nil || !b.exceeded {
		t.Errorf("err %v exceeded %v, want a refusal past 10 bytes", err, b.exceeded)
	}
	if b.String() != "12345" {
		t.Errorf("kept %q, want only what fit", b.String())
	}
}
