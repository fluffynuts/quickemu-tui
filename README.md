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
| s / p / K | start / ACPI shutdown / force stop (`quickemu --kill`, falls back to monitor `quit`) |
| c / a / A / d | create snapshot / revert / revert then start / delete |
| m | removable media: swap or eject ISOs on a running VM |
| e | edit the `.conf` in `$VISUAL`/`$EDITOR` (nano if unset) |
| l | logs: quickemu's log, the launch output, the generated launch script |
| x | ssh in via the forwarded port |
| o | open the VM folder |
| r, ?, q | refresh, help, quit (VMs keep running after you quit) |

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
