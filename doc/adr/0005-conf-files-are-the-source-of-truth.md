# 5. `.conf` files are the source of truth, read as plain assignments

Status: accepted (recorded retrospectively)

## Context

quickemu describes each VM in a bash file, `<name>.conf`, which quickemu itself
sources. The app lists and edits VMs.

## Decision

- A VM is identified by its `.conf` path (`qemu.VM{ConfPath}`); its name is the
  file name without `.conf`. `Discover` lists `*.conf` directly inside the VM
  directory, sorted by name. The app keeps no VM registry of its own.
- `ParseConf` reads only simple `key="value"` assignments, applying bash's
  quoting rules for a single word, and does no expansion: `"$HOME"` stays
  literal. Anything that isn't a plain assignment is ignored.
- File locations follow quickemu's: `<dir>/<name>/disk.qcow2`, unless `disk_img`
  says otherwise, in which case the VM's folder is that disk's directory
  (`VM.Paths`). Sockets, pid, ports, logs and the launch script are named
  after the VM inside that folder.
- Edits to a `.conf` are made as line-level changes that leave other lines
  alone: merging default options, quick settings, and repointing paths on
  rename. Default-option writes keep the file's mode, since quickemu confs are
  executable.
- Because quickemu runs the `.conf` as bash, anything the app writes into one
  from user input is checked first (see 0008) and VM names are restricted
  (`ValidateVMName`: letters, digits, `.`, `_`, `-`, starting with a letter or
  digit).

## Consequences

- The app and quickemu can't disagree about what a VM is, and VMs made by
  hand or by quickget appear without any import.
- A conf that uses bash features the parser ignores (expansion, computed
  values) shows what the literal text says, not what bash would compute.
- Settings the UI doesn't know about are preserved because edits are
  line-level rather than a rewrite.
