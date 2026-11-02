# 10. New VMs via quickget, with a cached, seeded catalog and a resuming curl shim

Status: accepted (recorded retrospectively)

## Context

`quickget --list-csv` takes minutes, because quickget asks every distribution's
website for its releases. quickget deletes a partial download when curl fails.

## Decision

- The list of installable systems comes from the user's own
  `quickget --list-csv`. It is cached in `<user cache dir>/quickemu-tui/catalog.csv`
  and refreshed in the background once older than 7 days (`CacheTTL`). Release
  dates (and end-of-life) come from endoflife.date, cached per product under
  `release-dates/` with the same TTL. A failed refresh is silent while there's
  a cached list to use. The cache is "only an accelerator".
- Release builds embed a copy of both lists (`internal/seed`, made by
  `make seed` / `cmd/genseed`, not kept in git). At startup the seed is
  unpacked into the cache folder if files are missing or older than the seed;
  newer locally fetched data is left alone. A build without a seed (such as
  a local `make build`) waits for quickget on first run.
- Quitting during a background refresh asks whether to abort it; the default is
  to wait so it gets cached. `ctrl+c` quits at once. Quitting during an install
  asks first.
- quickget runs in the VM directory, which `main.go` creates if missing (quickget
  otherwise fails with a misleading `fork/exec … no such file or directory`).
- `quickemu-tui` puts a small curl wrapper (`curlShim`) first on quickget's
  `PATH`. It reruns only calls that pass `--continue-at` (quickget's downloads),
  on curl exit codes meaning a dropped or stalled connection, up to 5 times, 5
  seconds apart. Other curl calls pass straight through. The wrapper also lets
  the app learn which file each download is writing, to show a percentage,
  speed and ETA from file size and response headers rather than only curl's own
  percentage.
- If quickget exits 0 but printed errors (as after a failed unzip), the install
  is reported as having problems.
- CPU and memory are chosen in the flow (or left `auto`) and written as
  `cpu_cores=`/`ram=`; with `spicy` installed, `display="spice"` is added unless
  the user's defaults set a `display`.

## Consequences

- The app can offer exactly what the installed quickget supports, and first
  run isn't a multi-minute wait for a release build.
- Resuming depends on quickget's current curl usage (`--continue-at`,
  `--disable`); a quickget change there would silently disable the retry.
- Partial downloads are kept, so installing the same thing again resumes.
