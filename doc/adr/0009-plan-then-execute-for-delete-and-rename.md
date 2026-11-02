# 9. Delete and rename: plan first, only touch folders a VM owns alone

Status: accepted (recorded retrospectively)

## Context

Deleting or renaming a VM touches its `.conf` and its folder, and a folder can
be outside the app's control: `disk_img` can point anywhere, or two VMs can
share a directory.

## Decision

- Both operations are split into `PlanDelete`/`PlanRename`, which work out and
  sanity-check what would change before anything is touched, and
  `DeleteVM`/`RenameVM`, which carry the plan out. The confirmation dialogs
  show the plan (each path and the folder's size for delete).
- The VM's folder is only included if it is a real directory (not a symlink)
  strictly inside the directory holding the `.conf`, and no other VM uses it
  (`ownDir`). Otherwise only the `.conf` is removed or renamed, and the
  dialog says what was left and why. Rename additionally requires the folder
  to be named after the VM.
- Neither runs on a running VM.
- Delete removes the folder first, so if that fails the `.conf` stays and the
  VM remains listed. Rename moves the folder first and the old `.conf` last;
  if a step fails, earlier steps are undone so the VM is never left pointing
  at a folder that isn't there. Logs and UEFI vars are renamed; runtime files
  (pid, ports, sockets, launch script) are removed as they are recreated on
  start. Paths in the `.conf` that point into the old folder are repointed,
  keeping each value's style (relative, `~/`, absolute).
- `MeasureFootprint` (the size shown for a VM) counts the same files delete
  would remove.

## Consequences

- A VM with an unusual layout loses at most its `.conf`, never a shared
  directory.
- Renaming logs and removing runtime files after the move is best-effort.
