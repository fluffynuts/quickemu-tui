// Package seed carries a copy of the installable-systems list and release
// dates, made when quickemu-tui was built, so a first run has something to
// show without waiting minutes for quickget. Unpack copies it into the cache
// folder; from there it's used, and refreshed, like any other cached data.
// Nothing reads the embedded files directly.
//
// The data is made by `make seed` (cmd/genseed) and isn't kept in git. CI
// attaches it to each release; pushes reuse the last release's copy, and a
// weekly run makes a fresh one, releasing only if it changed (see
// .github/workflows/build.yml). A build without it just has no seed.
package seed

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed data
var data embed.FS

// Generated names the file holding when the data was made (RFC 3339).
const Generated = "generated"

// CatalogFile is the seeded installable-systems list, as `quickget
// --list-csv` would write it.
const CatalogFile = "catalog.csv"

// ReleaseDatesDir holds the seeded endoflife.date answers, <product>.json.
const ReleaseDatesDir = "release-dates"

// Unpack copies the seed data into cacheDir: each file that's missing, or
// that's older than the seed and differs from it. Newer cached data, fetched
// on this machine, is left alone. Unpacked files get the seed's own date, so
// they're refreshed once that's a week old, like any other cached data.
func Unpack(cacheDir string) error {
	return unpack(data, cacheDir)
}

func unpack(fsys fs.FS, cacheDir string) error {
	root, err := fs.Sub(fsys, "data")
	if err != nil {
		return err
	}
	stamp, err := fs.ReadFile(root, Generated)
	if err != nil {
		return nil // no seed in this build
	}
	made, err := time.Parse(time.RFC3339, strings.TrimSpace(string(stamp)))
	if err != nil {
		return fmt.Errorf("seed data has a bad %s stamp: %w", Generated, err)
	}
	var errs []error
	err = fs.WalkDir(root, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isData(name) {
			return err
		}
		if err := unpackFile(root, name, filepath.Join(cacheDir, filepath.FromSlash(name)), made); err != nil {
			errs = append(errs, err)
		}
		return nil
	})
	return errors.Join(append(errs, err)...)
}

// isData reports whether a seed file belongs in the cache, as opposed to the
// stamp or the README that keeps the folder in git.
func isData(name string) bool {
	return name == CatalogFile || path.Dir(name) == ReleaseDatesDir && path.Ext(name) == ".json"
}

func unpackFile(root fs.FS, name, dest string, made time.Time) error {
	want, err := fs.ReadFile(root, name)
	if err != nil {
		return err
	}
	if st, err := os.Stat(dest); err == nil {
		if !st.ModTime().Before(made) {
			return nil // fetched here since the seed was made
		}
		if have, err := os.ReadFile(dest); err == nil && bytes.Equal(have, want) {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+path.Base(name)+"-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(want)
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		werr = os.Chtimes(tmp.Name(), made, made)
	}
	if werr == nil && cerr == nil {
		werr = os.Rename(tmp.Name(), dest)
	}
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("seeding %s: %w", dest, errors.Join(werr, cerr))
	}
	return nil
}
