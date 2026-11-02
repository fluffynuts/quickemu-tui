# 3. UI-free `internal/qemu`; a non-blocking Bubble Tea UI

Status: accepted (recorded retrospectively)

## Context

The app is a terminal UI over slow things: process checks, monitor sockets,
`qemu-img`, `quickget` (minutes for the catalog), downloads.

## Decision

- All VM logic lives in `internal/qemu`, which imports no UI code
  (`conf.go`'s package comment: "the UI-free logic"). `internal/tui` is the
  Bubble Tea layer; `internal/config`, `internal/seed` and `internal/upgrade`
  are separate small packages; `main.go` wires flags, config and the program.
- Every slow call runs in a `tea.Cmd`, so the UI never blocks, and `View()`
  does no IO (README, "How it works").
- Per-VM facts (status, parsed conf, paths, ports, SPICE target) are gathered
  off the UI thread on each poll into `vmInfo` (`model.go`), and `View()` only
  reads that. The poll interval is 2.5s (`pollInterval`); starting a VM also
  schedules an early poll after 1.5s. `QueryAll` checks all VMs concurrently.
- Dialogs are modes of one `Model` (`modeMenu`, `modeSnapDelete`,
  `modeInstallPick`, …); key handling dispatches on the mode.

## Consequences

- Logic is testable without a terminal: `internal/qemu` tests need no UI, and
  `internal/tui` tests drive `Model` directly.
- `View()` can show state up to a poll old. Operations that must not act on
  stale state therefore re-check right before acting (see 0007).
- `model.go` is the largest file and holds most of the mode and key handling.
  Nothing in the code or README says whether that was a deliberate choice;
  treat splitting it as an open option rather than a settled design.
