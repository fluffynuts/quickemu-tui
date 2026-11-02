package tui

import (
	"strings"
	"testing"

	"github.com/fluffynuts/quickemu-tui/internal/qemu"
)

func modelWithParsedSnapshots(t *testing.T) Model {
	t.Helper()
	info, err := qemu.ParseDiskInfo([]byte(`{"snapshots": [
		{"id": "1", "name": "oldest", "date-sec": 1000000000},
		{"id": "2", "name": "newest", "date-sec": 1700000000},
		{"id": "3", "name": "middle", "date-sec": 1500000000}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := newMouseModel(t, "alpha")
	m.disks[m.vms[0].ConfPath] = diskState{loaded: true, info: info}
	return m
}

func rowsMentioning(view string, tags ...string) []string {
	var found []string
	for _, line := range strings.Split(view, "\n") {
		for _, tag := range tags {
			if strings.Contains(line, tag) {
				found = append(found, tag)
			}
		}
	}
	return found
}

func TestSnapshotViewsListNewestFirst(t *testing.T) {
	want := "newest,middle,oldest"
	m := modelWithParsedSnapshots(t)
	vm := m.vms[0]
	tags := []string{"oldest", "newest", "middle"}

	pane := strings.Join(rowsMentioning(strings.Join(m.viewSnapshots(vm, 80, 20), "\n"), tags...), ",")
	if pane != want {
		t.Errorf("snapshot pane order = %s, want %s", pane, want)
	}

	m.openSnapRevert(vm)
	revert := strings.Join(rowsMentioning(m.viewSnapRevert(), tags...), ",")
	if revert != want {
		t.Errorf("revert picker order = %s, want %s", revert, want)
	}

	m.openSnapDelete(vm)
	del := strings.Join(rowsMentioning(m.viewSnapDelete(), tags...), ",")
	if del != want {
		t.Errorf("delete picker order = %s, want %s", del, want)
	}
}

func TestSnapshotCursorStartsOnNewest(t *testing.T) {
	m := modelWithParsedSnapshots(t)
	vm := m.vms[0]
	if got := m.snapshots(vm)[m.snapCursor].Name; got != "newest" {
		t.Errorf("snapshot pane cursor on %q, want newest", got)
	}
	m.openSnapRevert(vm)
	if got := m.snapshots(vm)[m.revCursor].Name; got != "newest" {
		t.Errorf("revert cursor on %q, want newest", got)
	}
	m.openSnapDelete(vm)
	if got := m.snapshots(vm)[m.delCursor].Name; got != "newest" {
		t.Errorf("delete cursor on %q, want newest", got)
	}
}

func TestSnapshotPaneShowsOnlyTagAndDate(t *testing.T) {
	m := modelWithParsedSnapshots(t)
	view := strings.Join(m.viewSnapshots(m.vms[0], 80, 20), "\n")
	for _, unwanted := range []string{"ID", "VM STATE"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("snapshot pane still shows %q:\n%s", unwanted, view)
		}
	}
	if !strings.Contains(view, "TAG") || !strings.Contains(view, "DATE") {
		t.Errorf("snapshot pane lacks TAG/DATE header:\n%s", view)
	}
}
