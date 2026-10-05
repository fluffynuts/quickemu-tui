package qemu

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// AutoTier is one row of quickemu's sizing table: hosts with at least Min
// (logical CPUs, or GB of RAM) give a VM Value.
type AutoTier struct {
	Min   int
	Value int
}

// quickemu 4.9's configure_cpu and configure_ram, used when a .conf doesn't
// set cpu_cores or ram. Largest first; the last row is the fallback.
var (
	AutoCoreTiers = []AutoTier{{32, 16}, {16, 8}, {8, 4}, {4, 2}, {0, 1}}
	AutoRAMTiers  = []AutoTier{{128, 32}, {64, 16}, {16, 8}, {8, 4}, {0, 2}}
)

// AutoTierFor is the row of tiers that applies to a host with have of the resource.
func AutoTierFor(tiers []AutoTier, have int) int {
	for i, t := range tiers {
		if have >= t.Min {
			return i
		}
	}
	return len(tiers) - 1
}

// HostCPUs is the number of logical CPUs, which is what quickemu counts (nproc).
func HostCPUs() int { return runtime.NumCPU() }

// HostRAM is the machine's total memory in bytes.
func HostRAM() (int64, error) {
	if runtime.GOOS == "darwin" {
		out, err := runCommand("", 5*time.Second, "sysctl", "-n", "hw.memsize")
		if err != nil {
			return 0, err
		}
		return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return parseMemTotal(bufio.NewScanner(f))
}

func parseMemTotal(sc *bufio.Scanner) (int64, error) {
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing MemTotal: %w", err)
			}
			return kb * 1024, nil
		}
	}
	return 0, errors.New("no MemTotal in /proc/meminfo")
}

// HostRAMGB is total memory in whole gigabytes, the way quickemu measures it
// (`free --giga` on Linux, GiB from sysctl on macOS).
func HostRAMGB(bytes int64) int {
	if runtime.GOOS == "darwin" {
		return int(bytes / (1 << 30))
	}
	return int(bytes / 1e9)
}

// RAMChoices are 4G steps up to about half the host's memory: 10% leeway, so a
// "32GB" machine reporting 31GiB still offers 16G, but never much past half.
func RAMChoices(hostBytes int64) []int {
	half := float64(hostBytes) / 2 / (1 << 30)
	top := int(math.Floor(half*1.1/4)) * 4
	var out []int
	for g := 4; g <= top; g += 4 {
		out = append(out, g)
	}
	return out
}

// SetConfValue makes text assign line's key exactly once, as line: the first
// existing assignment is replaced and any later ones dropped, or line is
// appended. Commented-out assignments are left alone.
func SetConfValue(text, line string) string {
	key, ok := DefaultKey(line)
	if !ok {
		return text
	}
	lines := strings.Split(text, "\n")
	out := lines[:0]
	done := false
	for _, l := range lines {
		if k, ok := DefaultKey(l); ok && k == key {
			if !done {
				out = append(out, line)
				done = true
			}
			continue
		}
		out = append(out, l)
	}
	result := strings.Join(out, "\n")
	if done {
		return result
	}
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result + line + "\n"
}

// SetConfValues applies SetConfValue for each line to the conf at path,
// keeping its mode.
func SetConfValues(path string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	for _, l := range lines {
		text = SetConfValue(text, l)
	}
	return os.WriteFile(path, []byte(text), st.Mode().Perm())
}
