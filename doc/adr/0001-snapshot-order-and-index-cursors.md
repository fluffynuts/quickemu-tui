# 1. Snapshots are listed newest first; cursors stay index-based

Status: accepted

## Context

`qemu-img info` lists snapshots in its own order. The UI shows them in the
detail pane and in the revert and delete pickers, and the cursor starts on the
first row, so the newest snapshot should be first.

## Decision

- `qemu.ParseDiskInfo` stable-sorts snapshots by date, newest first. Snapshots
  with equal dates keep the later-listed one on top. Ids are not used for
  ordering.
- The snapshot pane shows only tag and date (an untagged snapshot shows its id
  in the tag column).
- `snapCursor`, `revCursor` and `delCursor` remain plain indexes, clamped on
  refresh.

## Consequences

A new snapshot lands at index 0, so an index cursor on row N then points at a
different snapshot after the next disk refresh. This can only matter if the
disk changes from outside the TUI while a revert or delete picker is open:
creating, reverting and deleting all need the VM off, and the pickers are
modal.

## Alternative considered: track the highlighted snapshot by `snapKey`

Rejected for now as more code than the risk warrants. Costs: the key has to be
kept in step with the index on every keypress and refresh; a fallback (clamp)
is still needed when the snapshot disappears; and `snapKey` is only as unique
as the snapshot id, which we have not verified qemu never reuses after a
deletion (nor whether duplicate tags are possible).

Revisit if the drift is ever seen in practice.
