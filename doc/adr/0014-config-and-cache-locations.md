# 14. Where settings and caches live

Status: accepted (recorded retrospectively)

## Decision

- **Settings:** `<os.UserConfigDir>/quickemu-tui/config.json` (`config.Path`),
  holding `vm_dir`, `default_conf` and `ssh_user` (the user last SSHed in as,
  which pre-fills the next prompt). `config.Update` loads, applies one change
  and saves, so changing one setting never drops the others. A missing file
  means first run: with no `-dir` or argument, a setup dialog asks for the VM
  directory and saves it. A config that can't be read stops startup
  when the VM directory has to come from it (no `-dir` or argument); with an
  explicit directory it is reported and ignored.
- **VM directory:** default `~/quickemu`; `-dir` or the first argument
  overrides it for that run without saving. It is created if missing.
- **Caches:** `<os.UserCacheDir>/quickemu-tui/` (catalog, release dates, see
  0010). Deleting it starts afresh.
- **Per-VM files** live in the VM's own folder (0005, 0006).

## Consequences

- The VM directory isn't tied to the config, so `quickemu-tui ~/vms` works on
  a machine with a different saved directory.
