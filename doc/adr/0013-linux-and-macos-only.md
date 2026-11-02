# 13. Linux and macOS only

Status: accepted (recorded retrospectively)

## Context

The app drives quickemu and QEMU, which don't run on Windows, and it uses Unix
process groups and sessions (`Setsid`, `Setpgid`), so it doesn't build there
(`.github/workflows/build.yml`).

## Decision

Build and release for Linux and macOS on amd64 and arm64. Where the OS matters,
the code has per-OS files:

- `proc_linux.go` reads `/proc` for process identity and start time;
  `proc_darwin.go` uses sysctl `kern.proc.pid`; `proc_other.go` is the fallback.
- `procargs_linux.go` reads `/proc/<pid>/cmdline` to find a running VM's SPICE
  endpoint, so opening a SPICE viewer on a running VM is Linux only
  (README).
- `allocsize_unix.go`/`allocsize_other.go` give the space a file takes on disk
  (512-byte blocks, like `du`), which for a sparse disk image is less than its
  length; the other-OS version falls back to the length.

Tests for both OSs have needed their own fixes (several commits are "fixes for
osx tests"), so keep CI green on both.

## Consequences

- A new OS-specific feature needs a Darwin story or an explicit
  Linux-only note.
