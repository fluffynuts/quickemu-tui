package qemu

import (
	"os"
	"path/filepath"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(data)
}

func TestRollLogLeavesASmallOrMissingLogAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.log")
	if err := rollLog(path, 10, 3); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "0123456789") // exactly the limit
	if err := rollLog(path, 10, 3); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "0123456789" {
		t.Errorf("log is %q", got)
	}
}

func TestRollLogKeepsThreeFilesAndDropsTheOldest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.log")
	for _, gen := range []string{"first", "second", "third", "fourth"} {
		writeFile(t, path, gen+" is longer than the limit")
		if err := rollLog(path, 10, 3); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the log itself should have moved on, stat: %v", err)
	}
	for suffix, want := range map[string]string{".1": "fourth", ".2": "third"} {
		if got := readFile(t, path+suffix); got != want+" is longer than the limit" {
			t.Errorf("%s is %q, want %s", suffix, got, want)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("a fourth file was kept, stat: %v", err)
	}
}

func TestReadFromReturnsWhatCameAfterTheOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.log")
	writeFile(t, path, "old run\nnew run: boom\n")
	if got := ReadFrom(path, int64(len("old run\n"))); got != "new run: boom" {
		t.Errorf("got %q", got)
	}
	// rolled over since: the offset means nothing, so everything
	if got := ReadFrom(path, 1<<20); got != "old run\nnew run: boom" {
		t.Errorf("got %q", got)
	}
}
