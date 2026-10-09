package qemu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReleaseInfo is what endoflife.date knows about one quickget release.
type ReleaseInfo struct {
	Date time.Time // when the release (cycle) first came out
	EOL  bool      // no longer supported upstream
}

// ReleaseDates maps quickget release names to their info. Releases
// endoflife.date doesn't know (rolling, daily, betas) are absent.
type ReleaseDates map[string]ReleaseInfo

// eolProduct is the endoflife.date product for a quickget OS. prefix is put
// before quickget's release to make endoflife.date's name ("6" -> "lmde6").
type eolProduct struct{ name, prefix string }

// eolProducts maps quickget OS ids to endoflife.date products. Windows is left
// out: quickget's "11" is whatever build is current, while endoflife.date
// lists every feature update separately.
var eolProducts = map[string]eolProduct{
	"alma":           {name: "almalinux"},
	"alpine":         {name: "alpine-linux"},
	"android":        {name: "android"},
	"antix":          {name: "antix"},
	"centos-stream":  {name: "centos-stream"},
	"debian":         {name: "debian"},
	"devuan":         {name: "devuan"},
	"edubuntu":       {name: "ubuntu"},
	"fedora":         {name: "fedora"},
	"freebsd":        {name: "freebsd"},
	"kubuntu":        {name: "ubuntu"},
	"linuxmint":      {name: "linuxmint"},
	"lmde":           {name: "linuxmint", prefix: "lmde"},
	"lubuntu":        {name: "ubuntu"},
	"macos":          {name: "macos"},
	"mageia":         {name: "mageia"},
	"mxlinux":        {name: "mxlinux"},
	"netbsd":         {name: "netbsd"},
	"nixos":          {name: "nixos"},
	"openbsd":        {name: "openbsd"},
	"opensuse":       {name: "opensuse"},
	"oraclelinux":    {name: "oracle-linux"},
	"popos":          {name: "pop-os"},
	"proxmox-ve":     {name: "proxmox-ve"},
	"rockylinux":     {name: "rocky-linux"},
	"slackware":      {name: "slackware"},
	"ubuntu":         {name: "ubuntu"},
	"ubuntu-budgie":  {name: "ubuntu"},
	"ubuntu-mate":    {name: "ubuntu"},
	"ubuntu-server":  {name: "ubuntu"},
	"ubuntu-unity":   {name: "ubuntu"},
	"ubuntucinnamon": {name: "ubuntu"},
	"ubuntukylin":    {name: "ubuntu"},
	"ubuntustudio":   {name: "ubuntu"},
	"windows-server": {name: "windows-server"},
	"xubuntu":        {name: "ubuntu"},
}

// HasReleaseDates reports whether release dates can be looked up for an OS.
func HasReleaseDates(osID string) bool {
	_, ok := eolProducts[osID]
	return ok
}

// ReleaseDatesProduct is the endoflife.date product holding an OS's release
// dates, which is also the name of its cache file (<product>.json).
func ReleaseDatesProduct(osID string) (string, bool) {
	p, ok := eolProducts[osID]
	return p.name, ok
}

// ReleaseDatesURL is where endoflife.date describes a product.
func ReleaseDatesURL(product string) string { return releaseDatesURL + product }

var releaseDatesURL = "https://endoflife.date/api/v1/products/"

// FetchReleaseDates looks up when an OS's releases came out. cacheDir, if not
// empty, holds endoflife.date's answers so most lookups need no network; an
// answer is refreshed once it's older than CacheTTL, but a stale one is still
// used if the refresh fails.
func FetchReleaseDates(ctx context.Context, osID string, releases []string, cacheDir string) (ReleaseDates, error) {
	p, ok := eolProducts[osID]
	if !ok {
		return nil, nil
	}
	cachePath := ""
	if cacheDir != "" {
		cachePath = filepath.Join(cacheDir, p.name+".json")
	}
	var cached []byte
	if cachePath != "" {
		if st, err := os.Stat(cachePath); err == nil {
			if cached, err = os.ReadFile(cachePath); err == nil && time.Since(st.ModTime()) < CacheTTL {
				if d, err := ParseReleaseDates(cached, p.prefix, releases); err == nil {
					return d, nil
				}
			}
		}
	}
	data, err := getReleaseDates(ctx, p.name)
	if err != nil {
		if cached != nil {
			return ParseReleaseDates(cached, p.prefix, releases)
		}
		return nil, err
	}
	d, err := ParseReleaseDates(data, p.prefix, releases)
	if err == nil && cachePath != "" {
		_ = WriteCatalogCache(cachePath, data) // best effort
	}
	return d, err
}

func getReleaseDates(ctx context.Context, product string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleaseDatesURL(product), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endoflife.date %s: %s", product, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 4<<20))
}

// ParseReleaseDates matches quickget's release names against an
// endoflife.date product. A release matches a cycle by name ("24.04"), by
// code name ("big-sur" is "Big Sur"), or as a point release of it ("13.1.0"
// is cycle "13").
func ParseReleaseDates(data []byte, prefix string, releases []string) (ReleaseDates, error) {
	var doc struct {
		Result struct {
			Releases []struct {
				Name        string `json:"name"`
				Codename    string `json:"codename"`
				ReleaseDate string `json:"releaseDate"`
				IsEOL       bool   `json:"isEol"`
			} `json:"releases"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing endoflife.date answer: %w", err)
	}
	cycles := doc.Result.Releases
	if len(cycles) == 0 {
		return nil, errors.New("endoflife.date lists no releases")
	}
	out := ReleaseDates{}
	for _, rel := range releases {
		name := prefix + strings.ToLower(rel)
		if len(name) > 1 && name[0] == 'v' && name[1] >= '0' && name[1] <= '9' {
			name = name[1:] // alpine's v3.22
		}
		best, bestLen := -1, -1
		for i, c := range cycles {
			cn := strings.ToLower(c.Name)
			switch {
			case cn == name, c.Codename != "" && squash(c.Codename) == squash(rel):
				best, bestLen = i, len(name)+1 // exact beats any point-release match
			case strings.HasPrefix(name, cn+".") && len(cn) > bestLen:
				best, bestLen = i, len(cn)
			}
		}
		if best < 0 {
			continue
		}
		date, err := time.Parse(time.DateOnly, cycles[best].ReleaseDate)
		if err != nil {
			continue
		}
		out[rel] = ReleaseInfo{Date: date, EOL: cycles[best].IsEOL}
	}
	return out, nil
}

// squash lower-cases s and drops everything but letters and digits.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return -1
	}, s)
}

// SortByDate reorders the dated releases in rels newest first, among the
// places they already hold; undated ones (rolling, daily) stay where the name
// sort put them. rels is sorted in place and returned.
func SortByDate(rels []string, dates ReleaseDates) []string {
	var slots []int
	var dated []string
	for i, r := range rels {
		if _, ok := dates[r]; ok {
			slots = append(slots, i)
			dated = append(dated, r)
		}
	}
	sort.SliceStable(dated, func(i, j int) bool { return dates[dated[i]].Date.After(dates[dated[j]].Date) })
	for k, i := range slots {
		rels[i] = dated[k]
	}
	return rels
}

// PrefetchReleaseDates looks up release dates for every OS in the catalog
// that has them, refreshing stale cached answers as it goes, and returns what
// it found by OS id. OSes sharing a product (Ubuntu and its flavours) are
// looked up in turn, so only the first of them can need the network.
func PrefetchReleaseDates(ctx context.Context, cat Catalog, cacheDir string) map[string]ReleaseDates {
	byProduct := map[string][]string{}
	for _, o := range cat.OSes() {
		if p, ok := eolProducts[o.ID]; ok {
			byProduct[p.name] = append(byProduct[p.name], o.ID)
		}
	}
	out := map[string]ReleaseDates{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // be polite to endoflife.date
	for _, ids := range byProduct {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for _, id := range ids {
				d, err := FetchReleaseDates(ctx, id, cat.Releases(id), cacheDir)
				if err != nil {
					return // the rest of this product would fail the same way
				}
				mu.Lock()
				out[id] = d
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}
