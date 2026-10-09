package seed

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

const made = "2026-10-01T12:00:00Z"

func seedFS(catalog, devuan string) fstest.MapFS {
	return fstest.MapFS{
		"data/README.md":                 {Data: []byte("not data")},
		"data/generated":                 {Data: []byte(made + "\n")},
		"data/catalog.csv":               {Data: []byte(catalog)},
		"data/release-dates/devuan.json": {Data: []byte(devuan)},
	}
}

func read(t *testing.T, p string) (string, time.Time) {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	return string(data), st.ModTime()
}

func TestUnpackSeedsAnEmptyCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quickemu-tui") // not there yet
	if err := unpack(seedFS("cat", "dev"), dir); err != nil {
		t.Fatal(err)
	}
	stamp, _ := time.Parse(time.RFC3339, made)
	if got, mtime := read(t, filepath.Join(dir, "catalog.csv")); got != "cat" || !mtime.Before(stamp) {
		t.Errorf("catalog %q dated %v: should be dated long ago, to be refreshed", got, mtime)
	}
	if got, mtime := read(t, filepath.Join(dir, "release-dates", "devuan.json")); got != "dev" || !mtime.Equal(stamp) {
		t.Errorf("release dates %q dated %v, want the seed's date", got, mtime)
	}
	for _, name := range []string{"README.md", "generated"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s shouldn't be unpacked", name)
		}
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 2 {
		t.Errorf("stray files: %v", ents)
	}
}

func TestUnpackKeepsNewerDataAndReplacesOlder(t *testing.T) {
	dir := t.TempDir()
	newer := filepath.Join(dir, "catalog.csv")
	older := filepath.Join(dir, "release-dates", "devuan.json")
	_ = os.MkdirAll(filepath.Dir(older), 0o755)
	_ = os.WriteFile(newer, []byte("fetched here"), 0o644)
	_ = os.WriteFile(older, []byte("from long ago"), 0o644)
	long := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = os.Chtimes(older, long, long)

	if err := unpack(seedFS("cat", "dev"), dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, newer); got != "fetched here" {
		t.Errorf("data fetched since the seed was made was replaced: %q", got)
	}
	if got, _ := read(t, older); got != "dev" {
		t.Errorf("data older than the seed was kept: %q", got)
	}
}

func TestUnpackLeavesASeededCatalogAlone(t *testing.T) {
	// the unpacked catalog is dated long ago, so later runs must see it's
	// unchanged rather than rewrite it every time
	dir := t.TempDir()
	fsys := seedFS("cat", "dev")
	if err := unpack(fsys, dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "catalog.csv")
	_ = os.Chmod(p, 0o444)
	_ = os.Chmod(dir, 0o555) // a rewrite would fail
	defer os.Chmod(dir, 0o755)
	if err := unpack(fsys, dir); err != nil {
		t.Errorf("unchanged seed was rewritten: %v", err)
	}
}

func TestUnpackWithoutSeedData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if err := unpack(fstest.MapFS{"data/README.md": {Data: []byte("x")}}, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("nothing to seed, but the cache folder was made")
	}
}
