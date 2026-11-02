# 4. Run state comes from the HMP monitor socket, pid file as fallback

Status: accepted (recorded retrospectively)

## Context

The UI needs to know whether a VM is running, paused, or stopped, cheaply and
for every VM every poll. quickemu starts qemu with an HMP monitor on a unix
socket and writes a pid file.

## Decision

`QueryStatus` (`internal/qemu/status.go`):

1. If the monitor socket exists, send `info status`. An answer containing
   "paused" is `Paused`; any other answer is `Running`.
2. Otherwise, or if the monitor can't be reached, read the pid file and check
   the pid really is a qemu (`qemuProcess`, per OS, to guard against PID reuse
   after a stale pid file). Alive but not answering is `Busy`
   ("running (monitor busy)").
3. Otherwise `Stopped`. A leftover socket with nobody listening counts as
   stopped.

`Busy` exists because the monitor serves one client at a time, so for example an
open `socat` session makes it unreachable (`ErrMonitorTimeout`). Anything but
`Stopped` counts as up (`IsUp`). The start time shown in the UI comes from the
process (`/proc` on Linux, sysctl on macOS).

## Consequences

- No state is kept by the app: it is rediscovered each poll, so VMs started or
  stopped outside the app show correctly.
- A VM the app has just launched but which has no socket yet is covered
  separately: the model treats a launching VM as up (`Model.isUp`).
- The status can be seconds old; disk operations re-check with
  `RequireStopped` (see 0007).
