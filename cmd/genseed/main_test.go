package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const header = "Display Name,OS,Release,Option\n"

func TestMergeCatalogKeepsSupportedSystemsQuickgetMissed(t *testing.T) {
	previous := header + "Alpine,alpine,v3.22,\nDevuan,devuan,daedalus,\nGone,gone,1,\n"
	listed := header + "Zorin,zorin,18,\nDevuan,devuan,daedalus,\nDevuan,devuan,excalibur,\n"
	supported := map[string]bool{"alpine": true, "devuan": true, "zorin": true} // "gone" was dropped from quickget
	got, err := mergeCatalog([]byte(listed), []byte(previous), supported, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := header + "Alpine,alpine,v3.22,\nDevuan,devuan,daedalus,\nDevuan,devuan,excalibur,\nZorin,zorin,18,\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMergeCatalogRefusesAnOutage(t *testing.T) {
	previous := header + "A,a,1,\nB,b,1,\nC,c,1,\n"
	if _, err := mergeCatalog([]byte(header+"A,a,1,\n"), []byte(previous), nil, io.Discard); err == nil {
		t.Error("losing most systems at once should be refused")
	}
	if _, err := mergeCatalog([]byte(header), nil, nil, io.Discard); err == nil {
		t.Error("an empty list should be refused")
	}
}

func TestSupportedOSes(t *testing.T) {
	usage := "ERROR! You must specify an operating system.\n- Supported Operating Systems:\nalma alpine\nubuntu\n\nTo see all possible arguments, use:\n   quickget -h\n"
	got := supportedOSes([]byte(usage))
	if len(got) != 3 || !got["alma"] || !got["ubuntu"] || got["To"] {
		t.Errorf("got %v", got)
	}
}

func TestTrimReleaseDatesKeepsOnlyWhatsUsed(t *testing.T) {
	body := `{"schema_version":"1","generated_at":"now","result":{"name":"devuan","releases":[
		{"name":"5","codename":"Daedalus","releaseDate":"2023-08-14","isEol":false,"latest":{"name":"5.0.1"}},
		{"name":"4","codename":null,"releaseDate":"2021-10-14","isEol":true}]}}`
	got, err := trimReleaseDates([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, gone := range []string{"generated_at", "latest", "5.0.1", "schema_version"} {
		if strings.Contains(s, gone) {
			t.Errorf("%q should be dropped:\n%s", gone, s)
		}
	}
	for _, kept := range []string{`"Daedalus"`, `"2021-10-14"`, `"isEol": true`} {
		if !strings.Contains(s, kept) {
			t.Errorf("%s missing:\n%s", kept, s)
		}
	}
}

func TestSameDataIgnoresTheStamp(t *testing.T) {
	a := map[string][]byte{"catalog.csv": []byte("x"), "generated": []byte("1"), "README.md": []byte("r")}
	b := map[string][]byte{"catalog.csv": []byte("x"), "generated": []byte("2")}
	if !sameData(a, b) {
		t.Error("only the stamp differs")
	}
	b["release-dates/devuan.json"] = []byte("{}")
	if sameData(a, b) {
		t.Error("a new release-dates file is a change")
	}
}

func TestWriteSeedReplacesOldFilesButNotTheReadme(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("keep"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "release-dates"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "release-dates", "old.json"), []byte("{}"), 0o644)
	if err := writeSeed(dir, map[string][]byte{"catalog.csv": []byte("c"), "release-dates/new.json": []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "release-dates", "old.json")); err == nil {
		t.Error("a product no longer needed was kept")
	}
	got := readSeed(dir)
	if string(got["README.md"]) != "keep" || string(got["catalog.csv"]) != "c" || got["release-dates/new.json"] == nil {
		t.Errorf("got %v", got)
	}
}
