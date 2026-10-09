package qemu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// trimmed endoflife.date answers
const (
	devuanJSON = `{"result":{"name":"devuan","releases":[
		{"name":"6","codename":"Excalibur","releaseDate":"2025-11-02","isEol":false},
		{"name":"5","codename":"Daedalus","releaseDate":"2023-08-14","isEol":false},
		{"name":"4","codename":"Chimaera","releaseDate":"2021-10-14","isEol":true}]}}`
	macosJSON = `{"result":{"name":"macos","releases":[
		{"name":"15","codename":"Sequoia","releaseDate":"2024-09-16","isEol":false},
		{"name":"13","codename":"Ventura","releaseDate":"2022-10-24","isEol":true},
		{"name":"11","codename":"Big Sur","releaseDate":"2020-11-12","isEol":true}]}}`
	debianJSON = `{"result":{"name":"debian","releases":[
		{"name":"13","codename":"Trixie","releaseDate":"2025-08-09","isEol":false},
		{"name":"12","codename":"Bookworm","releaseDate":"2023-06-10","isEol":false}]}}`
	mintJSON = `{"result":{"name":"linuxmint","releases":[
		{"name":"22.1","codename":"Xia","releaseDate":"2025-01-16","isEol":false},
		{"name":"lmde6","codename":"Faye","releaseDate":"2023-09-27","isEol":false},
		{"name":"22","codename":"Wilma","releaseDate":"2024-07-25","isEol":false}]}}`
	alpineJSON = `{"result":{"name":"alpine-linux","releases":[
		{"name":"3.22","codename":null,"releaseDate":"2025-05-30","isEol":false}]}}`
)

func day(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s)
	return d
}

func TestParseReleaseDates(t *testing.T) {
	for name, tc := range map[string]struct {
		json, prefix string
		releases     []string
		want         ReleaseDates
	}{
		"code names": {devuanJSON, "", []string{"chimaera", "daedalus"}, ReleaseDates{
			"daedalus": {Date: day("2023-08-14")},
			"chimaera": {Date: day("2021-10-14"), EOL: true},
		}},
		"code names with spaces": {macosJSON, "", []string{"big-sur", "sequoia", "tahoe"}, ReleaseDates{
			"big-sur": {Date: day("2020-11-12"), EOL: true},
			"sequoia": {Date: day("2024-09-16")},
		}},
		"point releases": {debianJSON, "", []string{"13.1.0", "12.12.0", "sid"}, ReleaseDates{
			"13.1.0":  {Date: day("2025-08-09")},
			"12.12.0": {Date: day("2023-06-10")},
		}},
		"exact beats a shorter cycle": {mintJSON, "", []string{"22.1", "22"}, ReleaseDates{
			"22.1": {Date: day("2025-01-16")},
			"22":   {Date: day("2024-07-25")},
		}},
		"prefixed": {mintJSON, "lmde", []string{"6"}, ReleaseDates{
			"6": {Date: day("2023-09-27")},
		}},
		"v-prefixed": {alpineJSON, "", []string{"v3.22"}, ReleaseDates{
			"v3.22": {Date: day("2025-05-30")},
		}},
	} {
		got, err := ParseReleaseDates([]byte(tc.json), tc.prefix, tc.releases)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
	if _, err := ParseReleaseDates([]byte(`{"result":{}}`), "", []string{"1"}); err == nil {
		t.Error("expected an error for an answer without releases")
	}
}

func TestSortByDateLeavesUndatedInPlace(t *testing.T) {
	rels := []string{"ventura", "sequoia", "big-sur", "daily"}
	dates := ReleaseDates{
		"ventura": {Date: day("2022-10-24")},
		"sequoia": {Date: day("2024-09-16")},
		"big-sur": {Date: day("2020-11-12")},
	}
	if got, want := SortByDate(rels, dates), []string{"sequoia", "ventura", "big-sur", "daily"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	rels = []string{"25.04", "24.04", "daily-live"}
	if got, want := SortByDate(rels, nil), []string{"25.04", "24.04", "daily-live"}; !reflect.DeepEqual(got, want) {
		t.Errorf("without dates: got %v, want %v", got, want)
	}
}

func TestFetchReleaseDatesCaches(t *testing.T) {
	hits := 0
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !up || r.URL.Path != "/devuan" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(devuanJSON))
	}))
	defer srv.Close()
	oldURL := releaseDatesURL
	releaseDatesURL = srv.URL + "/"
	defer func() { releaseDatesURL = oldURL }()

	dir := t.TempDir()
	want := ReleaseDates{"daedalus": {Date: day("2023-08-14")}}
	fetch := func() ReleaseDates {
		t.Helper()
		d, err := FetchReleaseDates(context.Background(), "devuan", []string{"daedalus"}, dir)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if got := fetch(); !reflect.DeepEqual(got, want) || hits != 1 {
		t.Fatalf("first fetch: got %v after %d hits", got, hits)
	}
	if got := fetch(); !reflect.DeepEqual(got, want) || hits != 1 {
		t.Fatalf("a fresh cache should be used: got %v after %d hits", got, hits)
	}
	// a stale cache is refreshed, but still used if the refresh fails
	old := time.Now().Add(-2 * CacheTTL)
	if err := os.Chtimes(filepath.Join(dir, "devuan.json"), old, old); err != nil {
		t.Fatal(err)
	}
	up = false
	if got := fetch(); !reflect.DeepEqual(got, want) || hits != 2 {
		t.Fatalf("stale cache, failed refresh: got %v after %d hits", got, hits)
	}

	if d, err := FetchReleaseDates(context.Background(), "windows", []string{"11"}, dir); d != nil || err != nil {
		t.Errorf("an OS without a product: got %v, %v", d, err)
	}
	if HasReleaseDates("windows") || !HasReleaseDates("devuan") {
		t.Error("HasReleaseDates")
	}
}

func TestPrefetchReleaseDatesAsksOncePerProduct(t *testing.T) {
	answers := map[string]string{
		"/devuan": devuanJSON,
		"/ubuntu": `{"result":{"releases":[{"name":"24.04","codename":"Noble Numbat","releaseDate":"2024-04-25","isEol":false}]}}`,
	}
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		if a, ok := answers[r.URL.Path]; ok {
			_, _ = w.Write([]byte(a))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	oldURL := releaseDatesURL
	releaseDatesURL = srv.URL + "/"
	defer func() { releaseDatesURL = oldURL }()

	cat, err := ParseCatalog([]byte("Display Name,OS,Release,Option\n" +
		"Ubuntu,ubuntu,24.04,\nKubuntu,kubuntu,24.04,\nDevuan,devuan,daedalus,\nHaiku,haiku,r1beta5,\nDebian,debian,13.1.0,\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := PrefetchReleaseDates(context.Background(), cat, t.TempDir())
	noble := ReleaseDates{"24.04": {Date: day("2024-04-25")}}
	want := map[string]ReleaseDates{
		"ubuntu":  noble,
		"kubuntu": noble,
		"devuan":  {"daedalus": {Date: day("2023-08-14")}},
		// haiku has no product; debian's lookup failed
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if hits["/ubuntu"] != 1 || hits["/devuan"] != 1 {
		t.Errorf("each product should be asked for once: %v", hits)
	}
}
