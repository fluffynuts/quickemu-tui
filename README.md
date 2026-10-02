# quickemu-tui

A terminal UI for [quickemu](https://github.com/quickemu-project/quickemu) VMs,
focused on the "disposable test box" loop: start, poke, shut down, revert to
`pristine`, repeat.

## Build

```bash
go mod tidy          # resolves go.sum (and toolchain, if a dep needs newer Go)
go test ./...        # core logic: conf parsing, monitor protocol, parsers
go build -o quickemu-tui .
```

`make dist` (optionally `GOOS=darwin GOARCH=arm64 BUILD=7`) builds a zip like
`dist/quickemu-tui-0.1.7-macos-arm64.zip`. CI (`.github/workflows/build.yml`)
does this for Linux and macOS on amd64 and arm64, and each push to `master`
publishes a release: the version is `VERSION` (major.minor, bump by hand) plus
the CI run number. `quickemu-tui -version` prints it with the commit and build
time, e.g. `quickemu-tui 0.1.57 (c84a1bb6abd5, built 2026-10-02T13:23:26Z)`; a
build from uncommitted changes shows `-dirty` after the commit.

To upgrade an installed copy, run `quickemu-tui --upgrade`: it downloads the
latest GitHub release for your platform, checks it against the release's
`SHA256SUMS`, confirms the new binary runs, and only then replaces the old one
(in place, keeping its permissions). It does nothing if you're already on the
latest. The binary has to be in a directory you can write to; otherwise run it
with `sudo`.

Run it from anywhere:

```bash
./quickemu-tui                 # defaults to ~/quickemu
./quickemu-tui ~/vms           # or -dir ~/vms
./quickemu-tui -quickemu /opt/quickemu/quickemu
```

Needs `quickemu` and `qemu-img` on `PATH`. `xdg-open` and `ssh` are only used
by their respective keys.

## Keys

| key | action |
| --- | --- |
| ↑/k ↓/j, tab | move; switch between VM list and snapshot list |
| r, ?, q | refresh, help, quit (VMs keep running after you quit) |
| d | edit default VM options (see below) |
| n | install a new VM with `quickget` (see below) |
| enter | open the actions menu for the selected VM (↑↓ then enter runs one; esc closes) |

The keys below are shortcuts that work **inside the actions menu only**: pressing one is the same as choosing its item.

| key | action |
| --- | --- |
| s / p / K | start / ACPI shutdown / force stop (`quickemu --kill`, falls back to monitor `quit`) |
| c / a / A / d | create snapshot / revert / revert then start / delete: `d` opens a checklist (space ticks, `a` ticks all, enter deletes the ticked ones after confirming) |
| m | removable media: swap or eject ISOs on a running VM |
| e | edit the `.conf` in `$VISUAL`/`$EDITOR` (nano if unset) |
| l | logs: quickemu's log, the launch output, the generated launch script |
| x | ssh in via the forwarded port |
| o | open the VM folder |
| D | delete the VM: its `.conf` and its folder, after a confirmation that lists exactly what goes (not while it runs) |

## Installing a new VM

Press `n`. The list of what can be installed comes from `quickget --list-csv`
(so it always matches your installed quickemu). That command can take several
seconds, so it runs in the background at startup and the result is cached in
`~/.cache/quickemu-tui/catalog.csv`: the cached list is available immediately
on the next launch while a fresh one is fetched. If you press `n` before any
list exists, the picker waits for the fetch; if the fetch fails you'll be told
(and `n` retries), but a failed refresh with a cached list is silent. Pick an OS (type to filter),
a release, and an edition if there is one, then confirm. `quickget` runs in
your VM directory and the new VM appears in the list when it finishes.

A progress dialog shows a bar and percentage (parsed from curl's progress
output). `esc` keeps the download running in the background, with its progress
in the header, and `n` brings the dialog back. `c` cancels; partial downloads
are kept, so installing the same thing again resumes it. Quitting during an
install asks first. If `quickget` exits 0 but printed errors (it does this
after a failed unzip), the install is reported as having problems rather than
as a success.

When an install finishes, the new VM's installation images are checked: the
files its `.conf` names with `iso=` / `fixed_iso=`, plus any `*.iso` in its
folder. A real image has an ISO 9660 (or UDF) signature at byte 32768; a vendor
that has moved its download typically leaves a saved web page under the `.iso`
name instead (Windows is the usual offender). If an image is missing or isn't a
real ISO, a dialog gives its full path, what is wrong with it, and tells you to
download a genuine ISO and save it at that path (or edit `iso=` in the `.conf`).
The VM is still created.

## Deleting a VM

`D` in the actions menu removes the `.conf`, the VM's folder (where its disk
lives, with everything in it) and the desktop shortcut quickemu may have made,
after a confirmation listing each path and the folder's size. It is never done
to a running VM. The folder is only removed if it is a real directory inside
your VM directory that no other VM uses: if a VM's disk sits directly in the VM
directory, outside it, behind a symlink, or is shared, only the `.conf` is
removed and the confirmation says what was left and why.

## Default VM options

Some settings are needed on every VM on a given machine (for example
`gl="off"` when quickemu shows nothing with GL on). Press `d` to list them,
one `key="value"` per line, as they'd appear in a `.conf`; `ctrl+s` saves. They
are stored in the user config file (`default_conf`, next to `vm_dir`).

- **New VMs** get all of them added to their `.conf` when `quickget` finishes.
- **Existing VMs** get the ones their `.conf` doesn't already set, when you
  start them. A value the VM sets itself always wins, so an explicit
  `gl="on"` is never overwritten. (A commented-out `#gl="on"` counts as unset.)

Because quickemu runs the `.conf` as a bash script, only plain assignments are
accepted: lines with `;`, `&`, `|`, redirections, backticks or `$(...)` outside
quotes are rejected.

## How it works

All VM logic is in `internal/qemu` (no UI imports); `internal/tui` is the
Bubble Tea layer. Every slow call runs in a `tea.Cmd`, so the UI never blocks,
and `View()` does no IO.

- **Run state** comes from the HMP monitor socket quickemu creates
  (`info status`), with the pid file as a fallback. A leftover socket with
  nobody listening counts as stopped. A live qemu whose monitor doesn't answer
  shows as *monitor busy*: the monitor serves one client at a time, so an open
  `socat` session causes this.
- **Snapshots** are qcow2 internal snapshots via `qemu-img snapshot`. Create,
  revert and delete re-check that the VM is off right before running, since
  qemu-img on a live disk is how you corrupt it. Listing uses
  `qemu-img info -U --output=json`, which also works while the VM runs.
- **Launching** runs quickemu in its own session with output going to
  `<vm>/<name>.tui-launch.log`, not a pipe (qemu inherits those descriptors and
  would SIGPIPE once the TUI exits). A launching VM counts as "up", so you can't
  snapshot it mid-boot.
- **Media** swaps use the monitor's `change`/`eject -f`. Paths containing
  whitespace are rejected, because HMP can't take them.

## Assumptions worth checking on a real install

These are based on quickemu 4.9's file layout, not verified against its source:

- the pid file is `<vm_dir>/<name>.pid` (only the fallback; the monitor is primary)
- `<name>.ports` lines look like `ssh,22220` (the parser is lenient)
