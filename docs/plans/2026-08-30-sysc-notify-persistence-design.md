# sysc-notify Persistence Addendum

Date: 2026-08-30
Status: Locked for Milestone 5 (owner D2). Implementation lives in `sysc-notify`, not `sysc-shell`.
Supersedes: `2026-08-27-sysc-notify-design.md` § State and failure, sentence "Version one keeps bounded active state and history in memory. Restart loses both."

## Decision

Disk history is owned by **sysc-notify**. The shell never writes a history file. The presentation snapshot grows a `history` array beside `active`. Service restart loads history from disk; active notifications start empty (the D-Bus name was lost).

## Files

| Path | Mode | Contents |
|---|---|---|
| `$XDG_STATE_HOME/sysc-notify/history.json` | 0600 | Versioned JSON object containing history entries, newest last |
| `$XDG_STATE_HOME/sysc-notify/images/<sha256>.png` | 0600 | Downscaled PNG, at most 96 px on the long side |

`XDG_STATE_HOME` defaults to `$HOME/.local/state`. Create and verify the service directory as 0700 before
creating any temporary, JSON, or image file. Create every file as 0600; never create broadly then chmod.

## Entry schema

```json
{
  "id": 42,
  "app_name": "Firefox",
  "app_icon": "firefox",
  "desktop_entry": "firefox",
  "summary": "Download complete",
  "body": "report.pdf",
  "urgency": 1,
  "category": "",
  "timestamp": "2026-08-30T10:15:00Z",
  "image": "images/<sha256>.png"
}
```

No actions on disk. History cards cannot invoke after the live notification is gone (DMS). Active notifications keep actions in the `active[]` snapshot only.

Skip: `transient` true, DND does not affect this file.

## Bounds

- Cap **100** entries (Noctalia). Evict oldest when inserting past the cap; delete orphaned image files.
- Retention **7 days** default (DMS). A 60 s timer drops entries older than retention. Configurable later; hard-code 7 for v1.
- String fields already bounded by the 1.3 ingress limits. Do not re-copy unbounded image-data onto disk: decode, downscale, PNG-encode, hash, write once.
- The root object carries `schema_version: 1`. Load v1 only. Preserve and report a higher version without
  rewriting it; a future migration plan owns any conversion. Reject malformed entries independently when
  the rest of the file remains structurally valid.
- Atomic replace: create a same-directory uniquely named temporary file as 0600, write and `fsync` it,
  rename it over `history.json`, then `fsync` the service directory. Remove stale temporary files during
  startup after the last committed file has been loaded.
- Write PNG sidecars with the same create, sync, and rename discipline before publishing their relative
  path in `history.json`. On startup and after eviction, delete sidecars not referenced by the committed
  history object; account for one hash referenced by several entries.

## Snapshot

On shell connect, send:

```json
{"type":"snapshot","active":[...],"history":[...]}
```

Later events: `added` / `replaced` / `closed` for active; `history-added` / `history-removed` / `history-cleared` for disk. If the shell falls behind, drop the connection (existing rule). Reconnect gets a fresh snapshot including history.

Clear-all from the shell is an IPC command `history.clear` → service closes any still-active IDs with reason Dismissed, truncates the file, emits `history-cleared`.

## Privacy

0600 files, 0700 directory. Refuse to persist when those owner/mode invariants cannot be established. No
bodies in a world-readable cache. Do not persist sender PIDs. The service implementation plan must name
and test every supported transient or private/sensitive hint that suppresses persistence before M5 ships.

## Capabilities

Once this path ships, advertise `persistence`. Until then, do not.

## Tests

- Restart the service: `history.json` reloads; `active` is empty.
- Cap 100: the 101st insert drops the oldest and its PNG.
- Transient and each supported private/sensitive hint never appear on disk.
- Malformed JSON on load: log, start empty, do not crash, and preserve the bad file for inspection. A
  later notification must not overwrite it until the implementation has moved it to a same-directory
  private quarantine name.
- A future schema version is preserved and rejected without rewriting.
- Crash after temporary-file sync and before or after rename recovers either the previous complete file or
  the new complete file, never a partial JSON document. Modes are 0700/0600 throughout the test.
- Startup removes stale temporary files and unreferenced PNGs without deleting a shared referenced PNG.
- Shell reconnect after persist: snapshot `history` matches disk.
