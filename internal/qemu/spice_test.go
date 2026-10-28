package qemu

import (
	"strings"
	"testing"
)

func argv(s string) []string { return strings.Fields(s) }

func TestSpiceFromArgsReadsAUnixSocketRelativeToTheConfsDirectory(t *testing.T) {
	v := VM{ConfPath: "/home/u/quickemu/windows-11.conf"}
	// from a real quickemu windows-11 qemu command line
	got, ok := spiceFromArgs(v, argv("/usr/bin/qemu-system-x86_64 -vga none -display egl-headless,rendernode=/dev/dri/renderD128 "+
		"-spice disable-ticketing=on,unix=on,addr=windows-11/windows-11.sock -device virtio-serial-pci"))
	if !ok || got != (SpiceTarget{Socket: "/home/u/quickemu/windows-11/windows-11.sock"}) {
		t.Errorf("got %+v, %v", got, ok)
	}
}

func TestSpiceFromArgsReadsAPort(t *testing.T) {
	got, ok := spiceFromArgs(VM{ConfPath: "/q/a.conf"}, argv("qemu -spice disable-ticketing=on,port=5930,addr=localhost"))
	if !ok || got != (SpiceTarget{Port: 5930}) {
		t.Errorf("got %+v, %v", got, ok)
	}
}

func TestSpiceFromArgsFindsNothingWithoutAUsableSpiceOption(t *testing.T) {
	v := VM{ConfPath: "/q/a.conf"}
	for name, args := range map[string][]string{
		"no spice option":      argv("qemu -display sdl -audiodev spice,id=audio0"),
		"option without value": argv("qemu -spice"),
		"unix but no addr":     argv("qemu -spice unix=on"),
		"nothing to reach":     argv("qemu -spice disable-ticketing=on"),
		"no arguments":         nil,
	} {
		if got, ok := spiceFromArgs(v, args); ok || !got.IsZero() {
			t.Errorf("%s: got %+v, %v; want none", name, got, ok)
		}
	}
}

func TestViewerArgsPerViewerAndTarget(t *testing.T) {
	sock, port := SpiceTarget{Socket: "/q/a.sock"}, SpiceTarget{Port: 5930}
	for name, tc := range map[string]struct {
		target SpiceTarget
		viewer string
		want   []string
	}{
		"spicy socket":         {sock, "spicy", []string{"--uri=spice+unix:///q/a.sock"}},
		"spicy port":           {port, "/usr/bin/spicy", []string{"-h", "localhost", "-p", "5930"}},
		"remote-viewer socket": {sock, "remote-viewer", []string{"spice+unix:///q/a.sock"}},
		"remote-viewer port":   {port, "/usr/bin/remote-viewer", []string{"spice://localhost:5930"}},
	} {
		got := tc.target.ViewerArgs(tc.viewer)
		if len(got) != len(tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: %v, want %v", name, got, tc.want)
			}
		}
	}
}
