# Architecture decision records

Decisions that shape quickemu-tui, one file each. 0001 was written when the
decision was made; 0002 onwards were written afterwards (2026-11-02) to record
the app as it stands. For those, the *Context* and *Decision* come from the
code, its comments and the README. A rationale is only given where the code or
README states one; otherwise the record says it isn't known. Status is
"accepted" for everything in force. When you change a decision, add a new ADR
that supersedes the old one rather than rewriting history.

| # | Decision |
| --- | --- |
| [0001](0001-snapshot-order-and-index-cursors.md) | Snapshots are listed newest first; cursors stay index-based |
| [0002](0002-drive-quickemu-and-qemu-through-their-cli-and-monitor.md) | Drive quickemu, quickget and qemu-img as external tools |
| [0003](0003-ui-free-qemu-package-and-non-blocking-tui.md) | UI-free `internal/qemu`; a non-blocking Bubble Tea UI polling every 2.5s |
| [0004](0004-run-state-from-the-monitor-socket.md) | Run state comes from the HMP monitor socket, pid file as fallback |
| [0005](0005-conf-files-are-the-source-of-truth.md) | `.conf` files are the source of truth, read as plain assignments |
| [0006](0006-vms-run-detached-from-the-tui.md) | VMs and viewers run detached, logging to files |
| [0007](0007-snapshots-are-qcow2-internal-snapshots.md) | Snapshots are qcow2 internal snapshots, only touched while the VM is off |
| [0008](0008-default-vm-options.md) | Default VM options: validated plain assignments that never override a VM |
| [0009](0009-plan-then-execute-for-delete-and-rename.md) | Delete and rename: plan first, only touch folders a VM owns alone |
| [0010](0010-install-through-quickget-with-cached-catalog.md) | New VMs via quickget, with a cached, seeded catalog and a resuming curl shim |
| [0011](0011-custom-iso-installs.md) | Custom ISO installs |
| [0012](0012-check-installation-images-are-real-isos.md) | Check installation images are real ISOs |
| [0013](0013-linux-and-macos-only.md) | Linux and macOS only |
| [0014](0014-config-and-cache-locations.md) | Where settings and caches live |
| [0015](0015-releases-versioning-and-self-upgrade.md) | Every push to master is released; `--upgrade` verifies before replacing |
| [0016](0016-mouse-support-follows-the-drawn-layout.md) | Mouse support maps clicks onto the same layout the views draw |
