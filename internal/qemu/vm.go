package qemu

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// VM is one quickemu virtual machine, identified by its .conf file.
type VM struct {
	ConfPath string
}

// Name is the conf file name without ".conf" (quickemu's VMNAME).
func (v VM) Name() string {
	return strings.TrimSuffix(filepath.Base(v.ConfPath), ".conf")
}

// BaseDir is where quickemu runs from, and what relative conf paths resolve against.
func (v VM) BaseDir() string {
	return filepath.Dir(v.ConfPath)
}

// Conf reads and parses the VM's .conf file.
func (v VM) Conf() (map[string]string, error) {
	data, err := os.ReadFile(v.ConfPath)
	if err != nil {
		return nil, err
	}
	return ParseConf(string(data)), nil
}

// Resolve expands ~ and makes a conf-style path absolute against BaseDir.
func (v VM) Resolve(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(v.BaseDir(), p)
}

// Paths are the files quickemu creates for a VM.
type Paths struct {
	VMDir         string
	Disk          string
	MonitorSocket string
	PidFile       string
	PortsFile     string
	LaunchScript  string
	QuickemuLog   string
	LaunchLog     string // our own capture of quickemu's stdout/stderr
}

// Paths derives quickemu's file locations, following disk_img like quickemu does.
func (v VM) Paths() (Paths, error) {
	conf, err := v.Conf()
	if err != nil {
		return Paths{}, err
	}
	name := v.Name()
	vmDir := filepath.Join(v.BaseDir(), name)
	disk := filepath.Join(vmDir, "disk.qcow2")
	if img := conf["disk_img"]; img != "" {
		disk = v.Resolve(img)
		vmDir = filepath.Dir(disk)
	}
	return Paths{
		VMDir:         vmDir,
		Disk:          disk,
		MonitorSocket: filepath.Join(vmDir, name+"-monitor.socket"),
		PidFile:       filepath.Join(vmDir, name+".pid"),
		PortsFile:     filepath.Join(vmDir, name+".ports"),
		LaunchScript:  filepath.Join(vmDir, name+".sh"),
		QuickemuLog:   filepath.Join(vmDir, name+".log"),
		LaunchLog:     filepath.Join(vmDir, name+".tui-launch.log"),
	}, nil
}

// Discover lists the *.conf files directly inside root, sorted by name.
func Discover(root string) ([]VM, error) {
	matches, err := filepath.Glob(filepath.Join(root, "*.conf"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	vms := make([]VM, 0, len(matches))
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.Mode().IsRegular() {
			vms = append(vms, VM{ConfPath: m})
		}
	}
	return vms, nil
}
