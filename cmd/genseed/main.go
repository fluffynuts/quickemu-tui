// Command genseed makes the seed data quickemu-tui embeds (see internal/seed):
// what `quickget --list-csv` lists, and endoflife.date's release dates for
// those systems.
//
// quickget asks every distribution's website for its releases, and lists
// nothing for one whose site is down. So that an outage doesn't look like a
// change (and a week later, its recovery like another), a system quickget
// still supports but listed nothing for keeps its rows from the previous seed.
//
// Only the release-date fields quickemu-tui uses are kept, so the output is
// the same from run to run unless something that matters changed. The last
// line of output is changed=true or changed=false, compared with -previous,
// for CI to act on.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
	"github.com/fluffynuts/quickemu-tui/internal/seed"
)

func main() {
	out := flag.String("out", filepath.Join("internal", "seed", "data"), "where to write the seed")
	previous := flag.String("previous", "", "the last seed, to fall back on and compare with (default: -out)")
	quickget := flag.String("quickget", "", "path to quickget (default: found on PATH)")
	flag.Parse()
	if *previous == "" {
		*previous = *out
	}
	if err := run(*out, *previous, *quickget); err != nil {
		fmt.Fprintln(os.Stderr, "genseed:", err)
		os.Exit(1)
	}
}

func run(out, previous, quickget string) error {
	if quickget == "" {
		q, err := exec.LookPath("quickget")
		if err != nil {
			return errors.New("quickget not found on PATH (it ships with quickemu)")
		}
		quickget = q
	}
	prev := readSeed(previous)

	listed, err := runQuickget(quickget, "--list-csv")
	if err != nil {
		return err
	}
	supported, _ := runQuickget(quickget) // lists the supported systems, then exits 1
	catalog, err := mergeCatalog(listed, prev[seed.CatalogFile], supportedOSes(supported), os.Stderr)
	if err != nil {
		return err
	}
	cat, err := qemu.ParseCatalog(catalog)
	if err != nil {
		return err
	}

	next := map[string][]byte{seed.CatalogFile: catalog}
	for _, product := range products(cat) {
		name := seed.ReleaseDatesDir + "/" + product + ".json"
		data, err := fetchReleaseDates(product)
		if err != nil {
			if old, ok := prev[name]; ok {
				fmt.Fprintf(os.Stderr, "genseed: keeping the last %s: %v\n", name, err)
				next[name] = old
			} else {
				fmt.Fprintf(os.Stderr, "genseed: skipping %s: %v\n", name, err)
			}
			continue
		}
		next[name] = data
	}

	changed := !sameData(prev, next)
	stamp := prev[seed.Generated]
	if changed || stamp == nil {
		stamp = []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
	}
	next[seed.Generated] = stamp
	if err := writeSeed(out, next); err != nil {
		return err
	}
	fmt.Printf("changed=%v\n", changed)
	return nil
}

// runQuickget runs quickget with args, returning its stdout. Its stderr is
// noisy (stray jq errors), so it's dropped.
func runQuickget(quickget string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, quickget, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("quickget %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), err
}

// supportedOSes reads the list quickget prints when run without an OS: the
// lines after "- Supported Operating Systems:", up to a blank line.
func supportedOSes(usage []byte) map[string]bool {
	out := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(usage))
	in := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.Contains(line, "Supported Operating Systems"):
			in = true
		case in && line == "":
			return out
		case in:
			for _, f := range strings.Fields(line) {
				out[f] = true
			}
		}
	}
	return out
}

// mergeCatalog tidies quickget's CSV, adding back the previous rows of any
// system quickget supports but listed nothing for. With no list of supported
// systems to go by, every missing system is kept. A list that's lost more
// than half the systems is refused: the network was likely down.
func mergeCatalog(listed, previous []byte, supported map[string]bool, warn io.Writer) ([]byte, error) {
	header, rows, err := readCSV(listed)
	if err != nil {
		return nil, fmt.Errorf("quickget --list-csv: %w", err)
	}
	osCol := -1
	for i, h := range header {
		if strings.TrimSpace(h) == "OS" {
			osCol = i
		}
	}
	if osCol < 0 {
		return nil, errors.New(`quickget --list-csv has no "OS" column`)
	}
	byOS := group(rows, osCol)

	if pHeader, pRows, err := readCSV(previous); err == nil && strings.Join(pHeader, ",") == strings.Join(header, ",") {
		old := group(pRows, osCol)
		if len(byOS)*2 < len(old) {
			return nil, fmt.Errorf("quickget listed %d systems, down from %d: refusing to replace the seed", len(byOS), len(old))
		}
		for id, rows := range old {
			if _, ok := byOS[id]; !ok && (len(supported) == 0 || supported[id]) {
				fmt.Fprintf(warn, "genseed: quickget listed no releases for %s; keeping the last ones\n", id)
				byOS[id] = rows
			}
		}
	}
	if len(byOS) == 0 {
		return nil, errors.New("quickget listed nothing")
	}

	oses := make([]string, 0, len(byOS))
	for id := range byOS {
		oses = append(oses, id)
	}
	sort.Strings(oses)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(header)
	for _, id := range oses {
		_ = w.WriteAll(byOS[id])
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func readCSV(data []byte) (header []string, rows [][]string, err error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(all) == 0 {
		return nil, nil, errors.New("empty")
	}
	return all[0], all[1:], nil
}

// group splits rows by their OS column, keeping their order within each.
func group(rows [][]string, osCol int) map[string][][]string {
	out := map[string][][]string{}
	for _, row := range rows {
		if osCol < len(row) && strings.TrimSpace(row[osCol]) != "" {
			id := strings.TrimSpace(row[osCol])
			out[id] = append(out[id], row)
		}
	}
	return out
}

// products lists the endoflife.date products for the catalog's systems.
func products(cat qemu.Catalog) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range cat.OSes() {
		if p, ok := qemu.ReleaseDatesProduct(o.ID); ok && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// fetchReleaseDates gets a product from endoflife.date, keeping only the
// fields quickemu-tui reads (see qemu.ParseReleaseDates).
func fetchReleaseDates(product string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qemu.ReleaseDatesURL(product), nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return trimReleaseDates(body)
}

type trimmedRelease struct {
	Name        string `json:"name"`
	Codename    string `json:"codename,omitempty"`
	ReleaseDate string `json:"releaseDate"`
	IsEOL       bool   `json:"isEol"`
}

func trimReleaseDates(body []byte) ([]byte, error) {
	var doc struct {
		Result struct {
			Releases []trimmedRelease `json:"releases"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if len(doc.Result.Releases) == 0 {
		return nil, errors.New("no releases listed")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	return append(data, '\n'), err
}

// readSeed loads a seed folder's files by their slash-separated names.
func readSeed(dir string) map[string][]byte {
	out := map[string][]byte{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if data, err := os.ReadFile(p); err == nil {
			out[filepath.ToSlash(rel)] = data
		}
		return nil
	})
	return out
}

// sameData compares two seeds' data files, ignoring the stamp and anything
// else that isn't seed data.
func sameData(a, b map[string][]byte) bool {
	pick := func(m map[string][]byte) map[string][]byte {
		out := map[string][]byte{}
		for k, v := range m {
			if k == seed.CatalogFile || strings.HasPrefix(k, seed.ReleaseDatesDir+"/") {
				out[k] = v
			}
		}
		return out
	}
	pa, pb := pick(a), pick(b)
	if len(pa) != len(pb) {
		return false
	}
	for k, v := range pa {
		if w, ok := pb[k]; !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}

// writeSeed replaces the seed in dir with files, leaving its README alone.
func writeSeed(dir string, files map[string][]byte) error {
	_ = os.Remove(filepath.Join(dir, seed.CatalogFile))
	_ = os.Remove(filepath.Join(dir, seed.Generated))
	if err := os.RemoveAll(filepath.Join(dir, seed.ReleaseDatesDir)); err != nil {
		return err
	}
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
