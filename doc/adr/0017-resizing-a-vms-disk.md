# 17. Resizing a VM's disk: plan, warn, and keep `disk_size` in step

Status: accepted

## Context

A guest installer can refuse to install because the VM's disk is too small, and
the only fix was editing by hand. The disk is a qcow2 image driven through
`qemu-img` (0002, 0007).

What `qemu-img resize` does, checked against qemu-img 10.2.1 with a real
image:

- it grows and shrinks, with or without internal snapshots on the disk;
- shrinking needs `--shrink`, and says it deletes data past the new end;
- **reverting to a snapshot restores the disk size the snapshot was taken at**,
  so a resize is undone by reverting to an older snapshot.

Resizing only changes the virtual disk. The guest's partitions and filesystems
are the guest's business.

## Decision

- `qemu.ParseDiskSize` reads `64G`, `512M`, `1.5T` (binary units, as
  qemu-img), `+20G` and `-5G`. The unit is required, since a bare `40` would
  mean 40 bytes. Sizes are rounded up to qemu's 512-byte sector.
- `PlanResize` works out the target from the current virtual size and says how
  many snapshots the disk has; it only reads, so (like the other plans, 0009) the
  dialog can show it. `ResizeDisk` carries it out, re-checks that the VM is
  stopped (`RequireStopped`, 0007) and that the disk is still the size that was
  planned, passes `--shrink` for a shrink, then sets `disk_size` in the `.conf`
  to match (0005). If only that last step fails, the error says the disk *was*
  resized.
- The actions menu's `z` opens a picker: `+10G`, `+20G`, `+50G`, `+100G`, each
  showing the size it gives, and *Other size…*, which asks for a typed size.
  Every shrink is typed, never a preset.
- The confirmation says the guest's partitions aren't grown. A shrink is
  allowed, behind a strong warning that it destroys data, and enter on it means
  no. A disk with snapshots gets a warning that reverting restores their size.
- `qemu-img` doesn't report a snapshot's disk size, so a revert can't warn
  beforehand. `RevertToSnapshot` reads the size before and after instead, and
  when it changed the success message says so (kept beside the "starting" message
  when the revert was "revert and start"). A size that couldn't be read claims
  nothing.
- A quickget install asks for the disk size after memory (`default` writes
  nothing, so quickget's own choice stands). A custom ISO already chose it on
  its settings screen, so it skips that step.

## Consequences

- Nothing stops a shrink below what the guest uses; the warning is all there
  is, by choice.
- The user's usual loop, reverting to `pristine`, resets the size to what it was
  when `pristine` was taken. After growing a disk, take a new `pristine`.
- A revert that changes the size leaves the `.conf`'s `disk_size` as it was.
- Whether quickemu itself ever acts on `disk_size` for a disk that already
  exists is not known to this record: it is only kept consistent.
