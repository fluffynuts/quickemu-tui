package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheTTL is how long a check's answer is reused before GitHub is asked
// again; the API is rate-limited per address when unauthenticated.
const CacheTTL = 24 * time.Hour

type checkCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// Check returns the version of the latest release if it is newer than
// opts.Current, or "" if there is nothing to announce: already current or
// ahead, or a build whose version can't be compared ("dev").
//
// The latest release is cached at cachePath (none when empty) for CacheTTL.
// The cached release is compared with the running version each time, so a
// notice disappears as soon as the user upgrades.
//
// A failure to ask GitHub returns "" and the error. A failure only to write
// the cache returns the answer together with the error.
func Check(ctx context.Context, opts Options, cachePath string, now time.Time) (string, error) {
	if !isReleaseVersion(opts.Current) {
		return "", nil
	}
	latest, ok := readFreshCache(cachePath, now)
	var cacheErr error
	if !ok {
		var err error
		if latest, err = fetchLatest(ctx, opts); err != nil {
			return "", err
		}
		cacheErr = writeCache(cachePath, checkCache{CheckedAt: now, Latest: latest})
	}
	if cmp, ok := Compare(opts.Current, latest); !ok || cmp >= 0 {
		return "", cacheErr
	}
	return latest, cacheErr
}

func fetchLatest(ctx context.Context, opts Options) (string, error) {
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.APIBase == "" {
		opts.APIBase = DefaultAPI
	}
	if opts.Client == nil {
		opts.Client = &http.Client{}
	}
	rel, err := latestRelease(ctx, opts)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.Tag, "v"), nil
}

func readFreshCache(path string, now time.Time) (string, bool) {
	if path == "" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c checkCache
	if json.Unmarshal(data, &c) != nil || c.Latest == "" {
		return "", false // unreadable: asking again repairs it
	}
	if age := now.Sub(c.CheckedAt); age < 0 || age >= CacheTTL {
		return "", false
	}
	return c.Latest, true
}

func writeCache(path string, c checkCache) error {
	if path == "" {
		return nil
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("saving the update check: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("saving the update check: %w", err)
	}
	return nil
}
