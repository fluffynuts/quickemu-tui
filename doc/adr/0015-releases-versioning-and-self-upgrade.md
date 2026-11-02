# 15. Every push to master is released; `--upgrade` verifies before replacing

Status: accepted (recorded retrospectively)

## Decision

- **Version:** `VERSION` holds major.minor (bumped by hand); CI appends the run
  number as the third part, so every push to master is a distinct, valid
  semantic version that GitHub sorts above the last. `make` stamps the version,
  short commit (`-dirty` if uncommitted) and build time into the binary;
  `-version` prints them.
- **Builds:** `make dist` cross-compiles without cgo and zips the binary with the
  README; CI does Linux and macOS on amd64 and arm64 and publishes a release on
  each push to master. A weekly run refreshes the seed (0010) and releases only
  if it changed. `make fmtcheck` is run by CI; `go.mod`'s Go version is pinned
  so `gofmt` behaves the same everywhere (`gotoolchain_test.go`).
- **`--upgrade`** (`internal/upgrade`): fetch the latest GitHub release for this
  platform, check the zip against the release's `SHA256SUMS`, confirm the new
  binary runs, and only then replace the old one in place, keeping its
  permissions. Nothing is touched unless every check passes. It does nothing
  when already on the latest, and `dev` or an unparsable version always
  upgrades. Download sizes are capped (200 MiB) against runaway downloads.
  `QUICKEMU_TUI_API_BASE` redirects the API so an upgrade can be tried against a
  stand-in for GitHub.

## Consequences

- The binary must be in a directory the user can write to, or `--upgrade` needs
  `sudo`.
- Version numbers say nothing about the size of a change; the minor part is
  the only deliberate signal.
