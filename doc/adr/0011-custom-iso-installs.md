# 11. Custom ISO installs

Status: accepted (recorded retrospectively)

## Context

quickget doesn't know every system. The first row of the install list is
**Custom ISO…**, for an image the user has or can name.

## Decision

The flow (`internal/qemu/custom.go`, `internal/tui/custominstall.go`) takes:
an image (a local file, or an http(s) URL), a VM name, an optional SHA-256, and
the system type, boot mode and disk size, then CPU and memory like any install.

- A URL is downloaded with curl, following redirects, with the same retries,
  resuming and progress as a quickget install; the image is named from where the
  redirects end up. A SourceForge project page address is rewritten to the
  project's latest download. Only http and https are followed, and an https
  address can't be redirected to plain http, as that would let someone on the
  path swap what's downloaded (and the checksum fetched the same way).
- Whatever arrives must pass for an ISO (a `.img` is exempt), so a web page
  where the download should be is refused and nothing is left to resume. A local
  file is copied; the original is left in place.
- A checksum may be pasted as a bare digest, a `SHA256SUMS` line, or the address
  of a checksum file; a mismatch deletes the image and creates no VM. Leaving it
  blank is allowed, and the confirmation and finished message say the image
  wasn't checked.
- `guest_os` is chosen from the values quickemu 4.9 treats specially, because
  an unknown value quietly gets a generic profile; `macos` isn't offered as it
  needs a recovery image and OpenCore only quickget fetches. `boot` is written
  as exactly `efi` or `legacy` since quickemu reads anything without "efi" in it
  as legacy. Defaults are `linux`, `efi`, `32G`.
- The `.conf` is never overwritten (`CreateCustomVM` refuses an existing one). If
  `<name>/disk.qcow2` already exists, the confirmation warns and defaults to no.
- A finished download is remembered in `<name>/.quickemu-tui-fetch` until the VM
  is made, so retrying after a later failure doesn't download again. An
  interrupted one is kept as `<file>.part`.

## Consequences

- The guest-OS list needs updating when quickemu's special cases change.
