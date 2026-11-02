# 2. Drive quickemu, quickget and qemu-img as external tools

Status: accepted (recorded retrospectively)

## Context

quickemu-tui manages VMs that quickemu creates and runs. The README describes
it as a UI for quickemu VMs, focused on the "disposable test box" loop: start,
poke, shut down, revert to `pristine`, repeat.

## Decision

The app does not implement virtualisation or image handling itself. It shells
out to:

- `quickemu` to start (`--vm <conf>`) and force-stop (`--kill`) VMs;
- `quickget` to list (`--list-csv`) and install systems;
- `qemu-img` for snapshots and disk info;
- the QEMU HMP monitor socket for status, ACPI shutdown, and media changes
  (see 0004);
- `ssh`, `xdg-open`, `spicy`/`remote-viewer` and `$EDITOR` for the keys that
  need them.

`quickemu` and `qemu-img` must be on `PATH`; `-quickemu` overrides the quickemu
path, and quickget is then looked for next to it (`FindQuickget`).

## Consequences

- Behaviour follows the installed quickemu. For example the installable list
  matches the user's quickemu, because it comes from their `quickget`.
- The app depends on quickemu's file layout. The README's "Assumptions worth
  checking" section lists what is based on quickemu 4.9 and not verified
  against its source (pid file, ports file, SPICE endpoint). A quickemu change
  there is the likeliest way for the app to break.
- External output is parsed (`qemu-img info --output=json`, HMP text,
  `quickget --list-csv`), so parsers are unit-tested against sample output.
