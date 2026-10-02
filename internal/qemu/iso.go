package qemu

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ISOProblem is an installation image that can't be used.
type ISOProblem struct {
	Path   string // absolute path
	Reason string // what is wrong with it
}

const isoSectorSize = 2048

// Volume descriptors start at sector 16. ISO 9660 images carry "CD001"
// there; UDF-only images start their descriptor sequence with "BEA01".
var isoSignatures = [][]byte{[]byte("CD001"), []byte("BEA01")}

// ISOProblemFor says what's wrong with the file at path, or returns "" when it
// is a genuine ISO 9660 or UDF image. It reads only a few KiB.
func ISOProblemFor(path string) string {
	st, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return "does not exist"
	case err != nil:
		return err.Error()
	case st.IsDir():
		return "is a directory, not an ISO image (no ISO file was downloaded)"
	}
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer f.Close()

	head := make([]byte, 1024)
	n, _ := io.ReadFull(f, head)
	head = head[:n]

	// descriptor sectors 16..19
	buf := make([]byte, isoSectorSize)
	for sector := int64(16); sector < 20; sector++ {
		if _, err := f.ReadAt(buf, sector*isoSectorSize); err != nil && err != io.EOF {
			break
		}
		for _, sig := range isoSignatures {
			if bytes.Equal(buf[1:1+len(sig)], sig) {
				return ""
			}
		}
	}

	lower := bytes.ToLower(head)
	switch {
	case bytes.Contains(lower, []byte("<html")), bytes.Contains(lower, []byte("<!doctype html")), bytes.Contains(lower, []byte("<head")):
		return fmt.Sprintf("is an HTML web page (%s), not an ISO image: the download link has probably moved", HumanSize(st.Size()))
	case st.Size() < 16*isoSectorSize:
		return fmt.Sprintf("is only %s, far too small to be an ISO image", HumanSize(st.Size()))
	}
	return fmt.Sprintf("is not an ISO image (%s, no ISO 9660 or UDF signature)", HumanSize(st.Size()))
}

// VerifyISOs checks the installation images of a VM: the files its .conf names
// with iso= or fixed_iso=, and any *.iso in its folder. It returns the ones
// that are missing or not real ISO images.
func VerifyISOs(v VM) []ISOProblem {
	var candidates []string
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}
	if conf, err := v.Conf(); err == nil {
		for _, key := range []string{"iso", "fixed_iso"} {
			if val := conf[key]; val != "" {
				add(v.Resolve(val))
			}
		}
	}
	if p, err := v.Paths(); err == nil {
		// only scan a folder that belongs to this VM, never the whole VM directory
		if rel, err := filepath.Rel(v.BaseDir(), p.VMDir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			_ = filepath.WalkDir(p.VMDir, func(path string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".iso") {
					add(path)
				}
				return nil
			})
		}
	}

	var problems []ISOProblem
	for _, c := range candidates {
		if reason := ISOProblemFor(c); reason != "" {
			problems = append(problems, ISOProblem{Path: c, Reason: reason})
		}
	}
	return problems
}
