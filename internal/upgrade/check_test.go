package upgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAPI struct {
	srv    *httptest.Server
	tag    string
	status int
	hits   atomic.Int32
}

func newFakeAPI(t *testing.T, tag string) *fakeAPI {
	t.Helper()
	a := &fakeAPI{tag: tag}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		a.hits.Add(1)
		if a.status != 0 {
			w.WriteHeader(a.status)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[]}`, a.tag)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeAPI) check(t *testing.T, current, cache string, now time.Time) (string, error) {
	t.Helper()
	return Check(context.Background(), Options{Current: current, Repo: "o/r", APIBase: a.srv.URL}, cache, now)
}

var noon = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func cachePath(t *testing.T) string { return filepath.Join(t.TempDir(), "update-check.json") }

func TestCheckReportsANewerRelease(t *testing.T) {
	api := newFakeAPI(t, "v0.1.40")
	got, err := api.check(t, "0.1.29", cachePath(t), noon)
	if err != nil || got != "0.1.40" {
		t.Fatalf("Check = %q, %v; want 0.1.40", got, err)
	}
}

func TestCheckReportsNothingWhenCurrentOrAhead(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	for _, current := range []string{"0.1.40", "0.1.41", "0.2.0"} {
		if got, err := api.check(t, current, cachePath(t), noon); err != nil || got != "" {
			t.Errorf("Check(current %s) = %q, %v; want nothing", current, got, err)
		}
	}
}

func TestCheckSkipsBuildsWithNoReleaseVersion(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	for _, current := range []string{"dev", "", "not-a-version"} {
		if got, err := api.check(t, current, cachePath(t), noon); err != nil || got != "" {
			t.Errorf("Check(current %q) = %q, %v; want nothing", current, got, err)
		}
	}
	if n := api.hits.Load(); n != 0 {
		t.Fatalf("asked GitHub %d times for builds that can't be compared", n)
	}
}

func TestCheckAsksGitHubOnlyOncePerDay(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	cache := cachePath(t)
	api.check(t, "0.1.29", cache, noon)

	got, err := api.check(t, "0.1.29", cache, noon.Add(CacheTTL-time.Minute))
	if err != nil || got != "0.1.40" || api.hits.Load() != 1 {
		t.Fatalf("within the TTL: %q, %v, %d hits; want 0.1.40 from the cache after 1 hit", got, err, api.hits.Load())
	}

	api.tag = "0.1.50"
	got, err = api.check(t, "0.1.29", cache, noon.Add(CacheTTL))
	if err != nil || got != "0.1.50" || api.hits.Load() != 2 {
		t.Fatalf("after the TTL: %q, %v, %d hits; want a fresh 0.1.50", got, err, api.hits.Load())
	}
}

func TestCheckComparesTheCachedReleaseToTheRunningVersion(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	cache := cachePath(t)
	api.check(t, "0.1.29", cache, noon)

	// the user upgraded since: the notice goes away without asking again
	got, err := api.check(t, "0.1.40", cache, noon.Add(time.Hour))
	if err != nil || got != "" || api.hits.Load() != 1 {
		t.Fatalf("after upgrading: %q, %v, %d hits; want nothing, from the cache", got, err, api.hits.Load())
	}
}

func TestCheckIgnoresAnUnreadableCache(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	cache := cachePath(t)
	if err := os.WriteFile(cache, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := api.check(t, "0.1.29", cache, noon)
	if err != nil || got != "0.1.40" {
		t.Fatalf("Check = %q, %v; want 0.1.40", got, err)
	}
	if got, _ := api.check(t, "0.1.29", cache, noon); got != "0.1.40" || api.hits.Load() != 1 {
		t.Fatalf("the cache wasn't repaired: %d hits", api.hits.Load())
	}
}

func TestCheckWorksWithoutACache(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	got, err := api.check(t, "0.1.29", "", noon)
	if err != nil || got != "0.1.40" {
		t.Fatalf("Check = %q, %v; want 0.1.40", got, err)
	}
}

func TestCheckReportsAFailureToAsk(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	api.status = http.StatusInternalServerError
	cache := cachePath(t)
	got, err := api.check(t, "0.1.29", cache, noon)
	if err == nil || got != "" {
		t.Fatalf("Check = %q, %v; want an error", got, err)
	}
	if _, statErr := os.Stat(cache); statErr == nil {
		t.Fatal("a failed check was cached")
	}
}

func TestCheckStillAnswersWhenTheCacheCantBeWritten(t *testing.T) {
	api := newFakeAPI(t, "0.1.40")
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := api.check(t, "0.1.29", filepath.Join(blocked, "update-check.json"), noon)
	if got != "0.1.40" || err == nil {
		t.Fatalf("Check = %q, %v; want the answer and the cache error", got, err)
	}
}
