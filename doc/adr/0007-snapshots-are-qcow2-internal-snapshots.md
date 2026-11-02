# 7. Snapshots are qcow2 internal snapshots, only touched while the VM is off

Status: accepted (recorded retrospectively)

## Context

The core loop is "revert to `pristine`". The README says `qemu-img` on a live
disk is how you corrupt it.

## Decision

- Snapshots are qcow2 internal snapshots managed with `qemu-img snapshot`
  (`internal/qemu/snapshot.go`): create (`-c`), apply/revert (`-a`), delete
  (`-d`), addressed by tag, or by id for untagged ones (`Snapshot.Ref`).
- Listing uses `qemu-img info -U --output=json`; `-U` (force-share) lets it
  read a disk a running qemu has locked, so the list also works while the VM
  is up.
- Create, revert and delete call `RequireStopped` right before running,
  because the UI's state can be a few seconds old. The menu also disables
  them for an up VM, and says so.
- Revert can pick any snapshot, not just the latest; `s` reverts then starts
  the VM. Delete is a checklist with a confirmation. Tags are validated
  (`ValidateTag`): letters, digits, `.`, `_`, `-`, and not purely numeric,
  since qemu-img would confuse that with a snapshot id. A new snapshot's suggested tag is `pristine` if there are
  none, otherwise `snap-<timestamp>`.
- Display order and the cursor are in 0001.

## Consequences

- No separate snapshot store: the disk is the truth, and tools outside the app
  see the same snapshots.
- Snapshots live inside the disk image, so deleting the VM deletes them.
- A snapshot can't be taken of a running VM (disk-only snapshots are what
  `qemu-img` creates; `VMStateSize` is 0 for them).
