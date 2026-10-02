package qemu

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BlockDevice is one entry from the monitor's "info block".
type BlockDevice struct {
	Name      string
	File      string // empty when no medium is inserted
	Removable bool
}

var (
	blockHeaderRe = regexp.MustCompile(`^(\S+?)(?: \(#[^)]*\))?: (.*)$`)
	blockSuffixRe = regexp.MustCompile(` \([^()]*\)$`)
)

// ParseInfoBlock parses HMP "info block" output.
func ParseInfoBlock(text string) []BlockDevice {
	var devices []BlockDevice
	var cur *BlockDevice
	flush := func() {
		if cur != nil {
			devices = append(devices, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			flush()
			m := blockHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			rest := strings.TrimSpace(m[2])
			file := ""
			if !strings.HasPrefix(rest, "[not inserted]") {
				file = blockSuffixRe.ReplaceAllString(rest, "")
			}
			cur = &BlockDevice{Name: m[1], File: file}
		} else if cur != nil && strings.Contains(line, "Removable device") {
			cur.Removable = true
		}
	}
	flush()
	return devices
}

// ListBlockDevices asks the running VM which drives it has.
func ListBlockDevices(v VM) ([]BlockDevice, error) {
	p, err := v.Paths()
	if err != nil {
		return nil, err
	}
	out, err := MonitorCommand(p.MonitorSocket, "info block", 3*time.Second)
	if err != nil {
		return nil, err
	}
	return ParseInfoBlock(out), nil
}

// ChangeMedia inserts image into a removable drive (e.g. swap ISOs mid-install).
func ChangeMedia(v VM, device, image string) error {
	if strings.ContainsAny(image, " \t\n") {
		return errors.New("the QEMU monitor can't take paths containing whitespace; rename or symlink the file")
	}
	return monitorExpectSilence(v, "change "+device+" "+image)
}

// EjectMedia empties a removable drive.
func EjectMedia(v VM, device string) error {
	return monitorExpectSilence(v, "eject -f "+device)
}

// monitorExpectSilence runs a command whose success is printing nothing.
func monitorExpectSilence(v VM, command string) error {
	p, err := v.Paths()
	if err != nil {
		return err
	}
	out, err := MonitorCommand(p.MonitorSocket, command, 5*time.Second)
	if err != nil {
		return err
	}
	if out != "" {
		return &MonitorError{Msg: out}
	}
	return nil
}

var portRe = regexp.MustCompile(`^\s*([A-Za-z][\w-]*)\D*?(\d{2,5})\b`)

// ParsePorts reads quickemu's <name>.ports file (lines like "ssh,22220").
func ParsePorts(text string) map[string]int {
	ports := make(map[string]int)
	for _, line := range strings.Split(text, "\n") {
		m := portRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[2]); err == nil {
			ports[strings.ToLower(m[1])] = n
		}
	}
	return ports
}

// ReadPorts returns the forwarded ports quickemu recorded (empty if none).
func ReadPorts(v VM) map[string]int {
	p, err := v.Paths()
	if err != nil {
		return map[string]int{}
	}
	data, err := os.ReadFile(p.PortsFile)
	if err != nil {
		return map[string]int{}
	}
	return ParsePorts(string(data))
}

// HumanSize formats bytes with binary units.
func HumanSize(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(n)
	for i, unit := range units {
		if size < 1024 || i == len(units)-1 {
			if i == 0 {
				return fmt.Sprintf("%d B", n)
			}
			return fmt.Sprintf("%.1f %s", size, unit)
		}
		size /= 1024
	}
	return fmt.Sprintf("%d B", n)
}

// ReadTail returns up to maxBytes from the end of a file, or a note if unreadable.
func ReadTail(path string, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return "(" + err.Error() + ")"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "(" + err.Error() + ")"
	}
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "(" + err.Error() + ")"
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "(" + err.Error() + ")"
	}
	return string(data)
}

// LastLine returns the last non-blank line of text.
func LastLine(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}
