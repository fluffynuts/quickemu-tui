# 6. VMs and viewers run detached, logging to files

Status: accepted (recorded retrospectively)

## Context

README: "VMs keep running after you quit". The TUI owns the terminal, and qemu
inherits quickemu's stdout and stderr.

## Decision

- `Start` runs quickemu in its own session (`Setsid`) with stdin closed and
  stdout and stderr going to `<vm>/<name>.tui-launch.log` rather than a pipe.
  The code comment gives the reason: qemu inherits those descriptors, and a
  pipe would SIGPIPE it once the TUI exits. The caller `Wait()`s on the
  command to reap it.
- The SPICE viewer (`spicy`, else `remote-viewer`) is started the same way,
  appending to `<name>.tui-viewer.log`, which is rolled over when it passes
  1 MiB, keeping 3 files (`logroll.go`).
- A VM that has been launched counts as up while it boots, so nobody
  snapshots it mid-boot (see 0004).
- The logs dialog shows quickemu's log, the launch output and the generated
  launch script.

## Consequences

- Quitting the TUI never stops a VM, and an orphaned VM is picked up again by
  the next poll (see 0004).
- Launch failures are visible in `*.tui-launch.log`, not lost.
