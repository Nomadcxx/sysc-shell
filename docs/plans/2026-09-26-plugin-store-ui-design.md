# Plugin store UI — design amendment

Issue: `sysc-508`. Amends D9 of `2026-09-24-plugin-sources-design.md`. Owner-approved 2026-09-26.

The owner's bar for this slice: the UI gets the same rigour as the backend, and parity with DMS and
Noctalia is critical. D9 was written from the prior art's code. This amendment re-measures it against
their running UIs and records what changes.

## Reference capture

Captured 2026-09-26 on the laptop (eDP-1, 1536×864 logical, scale 1.25), running DMS and Noctalia
v5.1.0 against their live registries. The images are in
`docs/plans/assets/2026-09-26-plugin-store-references/`.

| Image | Shows |
|---|---|
| `dms-manage.png` | Settings → Plugins. Plugin Management card with Browse, Scan, Open Dir and Update All; the plugin directory; Registries (the official registry, plus name and URL fields with Add); available plugins as rows with version, author, description, a delete icon and an enable switch. |
| `dms-browse-grid.png` | The Browse Plugins window, maximised. Title with a "363/372" count; Hide 3rd Party, refresh, maximise and close; search; filter/sort chips (Hide installed, Installed first, Votes, Name, Contributor, Category); a 5-column card grid. Each card has a preview image with badges over it (3rd party, reviewed, featured, official), a votes count, the title, the author, a two-line description, and a round install button. |
| `dms-detail.png` | Detail inside the browse window: back arrow, plugin name, and an **Install** button in the header bar. Below: a large screenshot; badge and category chips; "by Author · source · discuss"; the description; capability chips; and dependency chips. |
| `dms-installed-toast.png` | An official plugin **installs straight away, with no confirmation**, and reports it with a toast ("Installed: Phone Connect"). |
| `noctalia-plugins.png` | Settings → Plugins. A Sources / Plugins segmented control, then the installed plugin rows: icon, name, a source badge (Community, local, or the source's name), version, description, a "Requires:" line, and open-page, settings and delete icons plus a switch. A **Browse Plugins** button sits at the top of the list. |
| `noctalia-sources.png` | The Sources panel: each source with its kind (Git or Path) and its URL or path; the precedence note "Lower sources override higher ones"; and an Auto-Update Plugins switch. |
| `noctalia-store-grid.png` | The Plugin Store sheet: search; source chips (All sources, Official, Community, and each custom source); a Categories expander; "231 plugins"; an A→Z sort button; 3 columns of tiles. Each tile shows a screenshot (or the plugin's icon when there is none), then name, version, source badge, description and author. |
| `noctalia-detail-readme.png` | Store detail: back, "Plugin Store", open-page and source icons, and close. The screenshot sits on the left. On the right: name with tag chips, then author · version · licence · source badge, the description, and **Add to Noctalia**. Below that, the plugin **README rendered as markdown**: headings, paragraphs and tables. |

## What changes in D9

### U1. Browse gets a dedicated store panel

Both references browse in a large surface of their own: DMS in a maximisable window with 5 columns,
Noctalia in a sheet over Settings with 3. D9's two columns inside the 900 px Settings body is visibly
more cramped than either. The fix:

- A new panel, `PanelPluginStore`, with IPC name `plugin-store`, and a target size of 1280×820 that
  `Placement.FittedSize` clamps on shorter outputs. It is opened by a **Browse plugins** button in
  Settings → Plugins and by `sysc-shell ipc panel.open '{"panel":"plugin-store"}'`.
- The Settings Plugins page keeps **Installed** and **Sources**. Browse moves to the store panel.
- The store panel follows the wallpaper picker's pattern: a header, a search row, a chip row and a
  tile grid, driven by a service relay.

### U2. Store panel layout

- **Header.** "Plugins", the result count ("N plugins"), refresh, and close. It shows "Checking
  sources…" while a refresh is running.
- **Search.** Matches name, author and description.
- **Chips.** Source chips (All plus each enabled source), a category chip that opens a menu of the
  categories present, a **Hide installed** toggle chip, and a **sort** chip that cycles Name → Updated →
  Added → Category → Installed first. The chips follow DMS's.
- **Grid.** Column count is `floor(width / 300)`, which gives 4 columns at 1280. Each card has:
  - a 16:9 screenshot slot, letterboxed, or when there is no screenshot the plugin's category glyph on
    a tinted field (Noctalia's icon fallback);
  - badges over the image: **Official** / **Community** / source name, plus **Deprecated** and
    **Held back** when they apply;
  - the title, then "v1.4.0 · author";
  - a two-line description;
  - one status action at the bottom right: install glyph, installed check, update glyph, or disabled
    with a reason on hover.
- **Detail view** replaces the grid inside the same panel, with a back arrow (Esc returns to the grid):
  - **Header bar:** name, Homepage and Release notes icons, and the primary action (Install, Update or
    Remove) at the right, as DMS places it.
  - **Top block:** the screenshot on the left. On the right: name with category and tag chips; author ·
    version · licence · source badge; the description; capability chips; and required-command chips.
    This is Noctalia's arrangement plus DMS's chips.
  - **Consent:** pressing the primary action expands the D5 consent block in place. The references have
    no consent step (DMS installs official plugins straight away). This design keeps D5, because these
    plugins are native code running with the user's privileges.
  - **README** (U3) renders below the top block.

### U3. README rendering

- **Catalog field.** A catalog row gains an optional `readme: {url, sha256}`, pinned the same way as
  `screenshot`. The URL must be https and the file at most 256 KiB. `sysc-plugins`' `tools/catalog
  update` fills it when `plugins/<dir>/README.md` exists at the tagged commit, using the raw URL for
  that tag.
- **Fetching.** The store fetches the README when the detail view opens, checks its hash, and caches it
  under the same cache as screenshots. If the fetch fails, the detail view falls back to
  `long_description`, then `description`.
- **Rendering.** The shell renders a markdown subset into existing `KindText`, `KindColumn`, `KindRow`
  and `KindSeparator` nodes. No UI primitive is added. The subset is:
  - ATX headings, levels 1–3;
  - paragraphs;
  - `-`/`*` and numbered lists, one level;
  - fenced code blocks;
  - inline code and bold;
  - links, shown as text followed by the URL;
  - pipe tables, laid out as rows of cells;
  - horizontal rules.

  Images and raw HTML are dropped. Anything else renders as plain text, so a README never fails to
  show.

### U4. Settings → Plugins

- **Segmented control:** Installed · Sources, plus a **Browse plugins** button.
- **Installed.**
  - Rows use the Settings row anatomy. This closes the two open "plugin cards/setting rows do not use
    the settings row anatomy" issues.
  - Each row has an icon, the name, a source badge, the version, the description, a "Requires:" line,
    a provenance line ("local override", "unlisted"), and an enable switch.
  - Actions: plugin settings, **Update to vX**, **Roll back to vY**, and **Remove** with an inline
    confirmation.
  - A header row shows **Updates (N)** and **Update all**.
- **Sources.**
  - Each source row shows kind and URL, "last fetched" or "stale: reason", a plugin count, an enable
    switch, Refresh, and Remove (not for `sysc`).
  - **Add source** takes a name and a URL, then shows the D5 warning.
  - A **Suggested** row offers the community catalog.
  - A note: "The same plugin offered by two sources appears once per source."
  - The local plugin directory and **Rescan** live here.
- **Auto-update.** Noctalia has an auto-update switch. Decision 3 of the sources design (check daily,
  never apply without the user) stands, so there is no switch. The daily check lands with this slice.

### U5. Unchanged from D9

- **Deferred:** votes and ratings; related plugins; change diffs.
- **Carried forward:**
  - consent pinned to what was on screen: the UI sends the version and sha256 it showed, so a refresh
    in between is refused;
  - a shadowed row still shows an available update;
  - reinstalling a plugin that is still in `Plugins.Enabled` says in the consent block that it will
    start.

## Verification bar

- **Headless render tests** for every state:
  - the grid: loading, results, empty, all sources failed, no screenshot;
  - the detail view: with a README, without one, consent open;
  - Installed: updates, local override, errors;
  - Sources: stale, add-source warning.
- **Live screenshots** of the same states:
  - on the desktop (DP-1, 3440×1440, scale 1.0);
  - on the laptop (eDP-1, 1536×864, scale 1.25);
  - each placed side by side with the matching reference image.
- **A visual and interaction review** before merge, separate from the code review: spacing, type scale,
  focus order, keyboard paths, and motion.
