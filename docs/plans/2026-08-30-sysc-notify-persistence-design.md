# sysc-notify Persistence Addendum

Date: 2026-08-30
Status: Locked for Milestone 5 (owner D2). Implementation lives in `sysc-notify`, not `sysc-shell`.
Supersedes: `2026-08-27-sysc-notify-design.md` § State and failure, sentence "Version one keeps bounded active state and history in memory. Restart loses both."

## Decision

Disk history is owned by **sysc-notify**. The shell never writes a history file. The presentation snapshot grows a `history` array beside `active`. Service restart loads history from disk; active notifications start empty (the D-Bus name was lost).

## Files

| Path | Mode | Contents |
|---|---|---|
| `$XDG_STATE_HOME/sysc-notify/history.json` | 0600 | JSON array of history entries, newest last |
| `$XDG_STATE_HOME/sysc-notify/images/<sha256>.png` | 0600 | Downscaled PNG, at most 96 px on the long side |

`XDG_STATE_HOME` defaults to `$HOME/.local/state`. Create the directory 0700.

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
- Atomic replace: write `history.json.tmp` then rename.

## Snapshot

On shell connect, send:

```json
{"type":"snapshot","active":[...],"history":[...]}
```

Later events: `added` / `replaced` / `closed` for active; `history-added` / `history-removed` / `history-cleared` for disk. If the shell falls behind, drop the connection (existing rule). Reconnect gets a fresh snapshot including history.

Clear-all from the shell is an IPC command `history.clear` → service closes any still-active IDs with reason Dismissed, truncates the file, emits `history-cleared`.

## Privacy

0600 files, 0700 directory. No bodies in world-readable cache. Do not persist sender PIDs (existing design: ephemeral only).

## Capabilities

Once this path ships, advertise `persistence`. Until then, do not.

## Tests

- Restart the service: `history.json` reloads; `active` is empty.
- Cap 100: the 101st insert drops the oldest and its PNG.
- Transient never appears on disk.
- Malformed JSON on load: log, start empty, do not crash, do not delete the bad file until a successful rewrite (operator can inspect).
- Shell reconnect after persist: snapshot `history` matches disk.
