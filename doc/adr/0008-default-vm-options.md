# 8. Default VM options: validated plain assignments that never override a VM

Status: accepted (recorded retrospectively)

## Context

Some settings are needed on every VM on a given machine, for example
`gl="off"` when quickemu shows nothing with GL on (README).

## Decision

- The user edits a list of `key="value"` lines (`d` key), stored in the user
  config as `default_conf` (0014).
- New VMs get all of them when `quickget` finishes. Existing VMs get the ones
  their `.conf` doesn't already set, when started (`MergeDefaults`,
  `ApplyDefaults`). A value the VM sets itself always wins, including an
  explicit `gl="on"`; a commented-out assignment counts as unset.
- Because quickemu sources the `.conf` as bash, `ValidateDefaults` accepts only
  plain assignments: no `;`, `&`, `|`, redirections, backticks or `$(...)`
  outside quotes, none of the last two inside double quotes either, and no
  unbalanced quotes. A key may appear once; blank lines and comments are fine.
- Quick settings (`Q`) and the install flow write specific keys (`cpu_cores`,
  `ram`, `width`/`height`, `fullscreen`, `gl`, `display`) in the same
  line-level way; an install's CPU/RAM choice overrides a default for the same
  key.

## Consequences

- A user-typed default can't run code when quickemu sources the conf.
- Defaults only fill gaps, so removing a default later doesn't remove it from
  VMs it was already merged into.
