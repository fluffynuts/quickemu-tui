package qemu

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseConf(t *testing.T) {
	text := "#!/usr/bin/quickemu --vm\n" +
		"guest_os=\"windows\"\n" +
		"disk_img=\"windows-10/disk.qcow2\"\n" +
		"# a comment\n" +
		"ram=16G # trailing comment\n" +
		"port_forwards=(\"8123:8123\" \"8888:80\")\n"
	conf := ParseConf(text)
	want := map[string]string{
		"guest_os":      "windows",
		"disk_img":      "windows-10/disk.qcow2",
		"ram":           "16G",
		"port_forwards": "(\"8123:8123\" \"8888:80\")",
	}
	if !reflect.DeepEqual(conf, want) {
		t.Fatalf("got %#v\nwant %#v", conf, want)
	}
}

func TestUnquoteValue(t *testing.T) {
	cases := map[string]string{
		`"abc"`:        "abc",
		`'a b'`:        "a b",
		`16G # c`:      "16G",
		`"x"'y'z`:      "xyz",
		`"a\$b"`:       "a$b",
		`"a\nb"`:       `a\nb`, // \n isn't an escape inside bash double quotes
		`a\ b`:         "a b",
		``:             "",
		`"unbalanced`:  `"unbalanced`,
		`"$HOME/x.iso"`: "$HOME/x.iso",
	}
	for raw, want := range cases {
		if got := UnquoteValue(raw); got != want {
			t.Errorf("UnquoteValue(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestPathsFollowDiskImg(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "windows-10.conf")
	writeFile(t, conf, "disk_img=\"windows-10/disk.qcow2\"\n")
	p, err := VM{ConfPath: conf}.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if p.VMDir != filepath.Join(dir, "windows-10") {
		t.Errorf("VMDir = %s", p.VMDir)
	}
	if filepath.Base(p.MonitorSocket) != "windows-10-monitor.socket" {
		t.Errorf("MonitorSocket = %s", p.MonitorSocket)
	}
	if p.Disk != filepath.Join(dir, "windows-10", "disk.qcow2") {
		t.Errorf("Disk = %s", p.Disk)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.conf", "a.conf", "notes.txt"} {
		writeFile(t, filepath.Join(dir, name), "")
	}
	vms, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, vm := range vms {
		names = append(names, vm.Name())
	}
	if !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatalf("got %v", names)
	}
}

// startFakeMonitor serves one HMP exchange the way QEMU does over a unix
// socket: banner, prompt, readline echo of the command, reply, prompt.
// socketDir is a short-pathed temp directory for unix sockets. Their paths are
// limited to ~104 bytes on macOS (108 on Linux), and t.TempDir() embeds the
// test's name under a long per-user temp root there, which can blow the limit.
func socketDir(t *testing.T) string {
	t.Helper()
	base := ""
	if st, err := os.Stat("/tmp"); err == nil && st.IsDir() {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "qt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startFakeMonitor(t *testing.T, path string, replies map[string]string) (*net.UnixListener, <-chan string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("QEMU 8.2.2 monitor - type 'help' for more information\r\n(qemu) "))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimSpace(line)
		received <- command
		reply := ""
		if r, ok := replies[command]; ok {
			reply = r + "\r\n"
		}
		_, _ = conn.Write([]byte(command + "\x1b[K\r\n" + reply + "(qemu) "))
	}()
	return ln.(*net.UnixListener), received
}

func TestMonitorCommand(t *testing.T) {
	sock := filepath.Join(socketDir(t), "vm-monitor.socket")
	startFakeMonitor(t, sock, map[string]string{"info status": "VM status: running"})
	out, err := MonitorCommand(sock, "info status", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if out != "VM status: running" {
		t.Fatalf("got %q", out)
	}
}

func TestMonitorEmptyReply(t *testing.T) {
	sock := filepath.Join(socketDir(t), "vm-monitor.socket")
	_, received := startFakeMonitor(t, sock, nil)
	out, err := MonitorCommand(sock, "system_powerdown", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("got %q", out)
	}
	if got := <-received; got != "system_powerdown" {
		t.Fatalf("server got %q", got)
	}
}

func TestMonitorSilentServerTimesOut(t *testing.T) {
	sock := filepath.Join(socketDir(t), "vm-monitor.socket")
	ln, err := net.Listen("unix", sock) // accepts into the backlog, never speaks: like a busy monitor
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, err = MonitorCommand(sock, "info status", 300*time.Millisecond)
	if !errors.Is(err, ErrMonitorTimeout) {
		t.Fatalf("want ErrMonitorTimeout, got %v", err)
	}
}

func TestQueryStatusRunningThenStale(t *testing.T) {
	dir := socketDir(t)
	conf := filepath.Join(dir, "vm.conf")
	writeFile(t, conf, "disk_img=\""+filepath.Join(dir, "disk.qcow2")+"\"\n")
	vm := VM{ConfPath: conf}
	sock := filepath.Join(dir, "vm-monitor.socket")

	ln, _ := startFakeMonitor(t, sock, map[string]string{"info status": "VM status: running"})
	ln.SetUnlinkOnClose(false) // leave the socket file behind, like a crashed qemu
	if s := QueryStatus(vm, time.Second); s.State != Running {
		t.Fatalf("want running, got %v (%s)", s.State, s.Detail)
	}
	_ = ln.Close()
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket file should still exist: %v", err)
	}
	if s := QueryStatus(vm, time.Second); s.State != Stopped {
		t.Fatalf("stale socket: want stopped, got %v", s.State)
	}
}

func TestParseDiskInfo(t *testing.T) {
	payload := `{"virtual-size": 68719476736, "actual-size": 21474836480,
		"snapshots": [{"id": "1", "name": "pristine", "date-sec": 1790000000, "vm-state-size": 0}]}`
	info, err := ParseDiskInfo([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if info.VirtualSize != 68719476736 || len(info.Snapshots) != 1 || info.Snapshots[0].Name != "pristine" {
		t.Fatalf("got %+v", info)
	}
	if info.Snapshots[0].Date.Unix() != 1790000000 {
		t.Fatalf("date %v", info.Snapshots[0].Date)
	}
	empty, err := ParseDiskInfo([]byte(`{"virtual-size": 1, "actual-size": 2}`))
	if err != nil || len(empty.Snapshots) != 0 {
		t.Fatalf("got %+v, %v", empty, err)
	}
}

func TestValidateTag(t *testing.T) {
	for _, ok := range []string{"pristine", "after-ie.2", "snap_1"} {
		if err := ValidateTag(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "has space", "42", "-leading"} {
		if ValidateTag(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParsePorts(t *testing.T) {
	got := ParsePorts("ssh,22220\nspice,5930\n")
	if !reflect.DeepEqual(got, map[string]int{"ssh": 22220, "spice": 5930}) {
		t.Fatalf("got %v", got)
	}
	if got := ParsePorts("ssh: 22220"); got["ssh"] != 22220 {
		t.Fatalf("got %v", got)
	}
}

func TestParseInfoBlock(t *testing.T) {
	text := "ide0-cd0 (#block174): windows-10/windows-10.iso (raw, read-only)\n" +
		"    Attached to:      /machine/unattached/device[23]\n" +
		"    Removable device: not locked, tray closed\n" +
		"    Cache mode:       writeback\n" +
		"\n" +
		"ide1-cd0: [not inserted]\n" +
		"    Attached to:      /machine/unattached/device[24]\n" +
		"    Removable device: not locked, tray closed\n" +
		"\n" +
		"virtio0 (#block318): windows-10/disk.qcow2 (qcow2)\n" +
		"    Attached to:      /machine/peripheral-anon/device[1]/virtio-backend\n"
	want := []BlockDevice{
		{Name: "ide0-cd0", File: "windows-10/windows-10.iso", Removable: true},
		{Name: "ide1-cd0", File: "", Removable: true},
		{Name: "virtio0", File: "windows-10/disk.qcow2", Removable: false},
	}
	if got := ParseInfoBlock(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{512: "512 B", 1536: "1.5 KiB", 64 << 30: "64.0 GiB"}
	for n, want := range cases {
		if got := HumanSize(n); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestLastLine(t *testing.T) {
	if got := LastLine("a\nERROR! boom\n\n  \n"); got != "ERROR! boom" {
		t.Fatalf("got %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
