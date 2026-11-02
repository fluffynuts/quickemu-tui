# 12. Check installation images are real ISOs

Status: accepted (recorded retrospectively)

## Context

README: a vendor that has moved its download typically leaves a saved web page
under the `.iso` name instead of an image (Windows is the usual offender).

## Decision

`ISOProblemFor` reads a few KiB of a file and accepts it only with an ISO 9660
(`CD001`) or UDF (`BEA01`) signature in one of the volume descriptor sectors
16 to 19 (byte 32768 onwards, 2048-byte sectors). A file that fails is
described as an HTML page, as far too small, or as simply not an ISO. After an install finishes, the files named by
`iso=`/`fixed_iso=` in the new `.conf`, plus any `*.iso` in its folder, are
checked. If one is missing or not a real image, a dialog gives its path, the
problem, and what to do (download a genuine ISO and save it at that path, or
edit `iso=`). The VM is still created. The same check gates custom ISO
downloads and copies (see 0011).

## Consequences

- A bad download is caught at install time, not at first boot.
- A genuine image in a format the check doesn't recognise would be flagged;
  `.img` is exempt in the custom flow.
