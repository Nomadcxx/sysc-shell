# Launcher Providers — Design

Tracking: `sysc-624` (UI parity slice 5), parent of `sysc-79` (calculator), `sysc-80` (emoji) and
`sysc-84` (searchable desktop actions). Owner-approved 2026-09-27. Source: the gap analysis's
critical gap 7, "Launcher breadth". The windows provider (`sysc-81`) and the app grid / category
chips (`sysc-83`) are out of this slice.

## Where the launcher stands

- `sysc-launch` (module `github.com/Nomadcxx/sysc-launch`, the shell pins v0.1.0) owns query
  routing: a private `/`-prefix provider table (`prefix.go`) that holds only Applications, a
  `/` overview, and usage history. Activation always means "spawn this desktop entry through
  `niri msg action spawn`".
- The shell injects its own ranking (`internal/shell/launcher_rank.go`, `launcherRank`), a copy of
  the library's with the browse cap removed; `TestLauncherRankMatchesLibrary` pins parity.
- Notes is bolted on in the shell as three special cases: `notesLauncherResults` intercepts `/nt`
  queries before the service, `addNotesProvider` injects an overview row, and
  `launcherActivateSelected` recognises two magic IDs.
- Desktop actions are reachable only through a right-click menu on an application row.

The launcher design's D10 (`2026-09-01-launcher-design.md`) already said calculator, emoji and
windows "slot into the table as one entry". This slice builds the seam that makes that true.

## Decisions

### D1. sysc-launch v0.2.0 gains a provider seam

Additive, backwards compatible, and not touching `score.go` (which carries another session's
uncommitted change removing the result cap):

```go
type Provider struct {
	Name, Prefix, Glyph, Description string
	Query    func(query string) []Result
	// Activate runs a row this provider produced, given the routed query.
	// Nil means the Applications behaviour: spawn the entry (or its action).
	Activate func(query, id, action string) error
	// Inline providers are also queried for bare (unprefixed) text; their
	// rows are placed above the default provider's.
	Inline bool
}

type ServiceConfig struct {
	// ... existing fields
	Providers []Provider // appended after Applications, in order
}

type Result struct {
	Entry  Entry
	Score  int
	Action string // a desktop action ID when this row is one of Entry's actions
}
```

- Prefixes are unique and begin with `/`. A provider whose prefix is empty, lacks the `/`, repeats
  an earlier one, or is `/apps` is skipped and logged once through `ServiceConfig.Logf`.
- The service remembers, for the result set it last published, which provider produced each row
  (by `Entry.ID` and `Action`). `Activate(id, action)` routes to that provider, so an inline
  calculator row under a bare query activates through the calculator, not the spawn path.
- History records every successful activation, whatever the provider.
- The `/` overview lists every registered provider, Applications first.
- Released as tag `v0.2.0`; the shell bumps its pin in the same change that uses it.

### D2. Calculator — `/calc`, and inline

A pure evaluator in the shell (`internal/calc`): recursive descent over `float64`.

- Grammar: `+ - * / %`, `^` (right-associative), unary minus, parentheses, decimals and `1e3`,
  constants `pi` and `e`, functions `sqrt abs ln log sin cos tan floor ceil round` (`log` is
  base 10). Case-insensitive; whitespace ignored; `×`, `÷` accepted as `*`, `/`.
- Result format: up to 12 significant digits, trailing zeros trimmed (`%.12g`), `-0` shown as `0`.
  Infinity and NaN are not results.
- Inline rule: a bare query shows a calculator row only when it parses completely **and** contains
  at least one operator or function call. `firefox`, `2048`, `e` and `pi` alone never show one.
- Under `/calc`, input that does not parse shows one non-activatable hint row, "Invalid
  expression"; empty input shows the grammar summary.
- Row: `Entry.Name` = `= 42`, `Entry.Comment` = `6*7 · Enter copies`. Enter copies the bare
  result text (`42`).

### D3. Emoji — `/emo`

- Data: an embedded, committed table `internal/emoji/emoji.tsv` (code points, name, keywords),
  generated at authoring time by `tools/emojigen` from pinned Unicode 16.0 `emoji-test.txt`
  (fully-qualified sequences only, no skin-tone variants, no components) and CLDR 46 English
  annotations. Licence: Unicode License v3, copied to `internal/emoji/LICENSE`, with the pinned
  URLs and SHA-256 sums in `internal/emoji/SOURCE.md` — the same record the Material subset keeps.
- Search, case-insensitive, first match class wins: exact name, name prefix, a word in the name
  with the query as prefix, a keyword with the query as prefix, name substring. Ties keep table
  order. At most 50 rows. Empty query lists the table's first 50 (the "Smileys" group).
- Row: `Entry.Name` = `😀  grinning face`, `Entry.Comment` = the first three keywords. Enter
  copies the emoji.
- Rendering relies on the font map's CBDT colour-emoji path (Noto Color Emoji). Multi-code-point
  sequences (flags, ZWJ families) depend on shaping; the live gate checks them, and a sequence that
  renders as separate glyphs is a known limitation, not a blocker.

### D4. Desktop actions as ranked rows

- `launcherRank` also scores each entry's desktop actions, for non-empty queries only (browse stays
  applications only). An action matches on `"<App name> <Action name>"`; its score is the fuzzy
  score minus 5, so an application ranks above its own actions for the same text.
- Action rows are `Result{Entry: app, Action: action.ID}`. The row shows `Firefox · New Private
  Window` with the action's icon when it names one, else the application's.
- Activation passes `Result.Action` to `Service.Activate`, whose spawn path already resolves it.
- `TestLauncherRankMatchesLibrary` compares application rows only; the library's own rank does not
  produce action rows.

### D5. Notes moves onto the seam

Notes becomes an ordinary provider: prefix `/nt`, `Query` = today's `notesLauncherResults` logic,
`Activate` = today's `launcherNotesAction`. `addNotesProvider`, the interception in `relayLauncher`
and the magic-ID branch in `launcherActivateSelected` are deleted. Existing Notes launcher tests
pin identical behaviour.

### D6. Copy goes through the shell's own clipboard

Calculator and emoji activation sends a `wayland.SelectionRequest{Copy: text, Serial: s}` through
`Registry.requestSelection`, added by text-input parity Phase 3 (`sysc-623` Tasks 11–12), with the
serial of the Enter key or the pointer press that activated the row. One clipboard owner; copies
reach sysc-clipboard's history like any application's. This one task waits for that phase to
merge; everything else in the slice does not.

### D7. Overview and glyphs

Bare `/` lists Applications, Calculator, Emoji and Notes. Two Material glyphs, `calculate` and
`emoji_emotions`, join the committed subset through `internal/render/icons/material/build.py` from
the pinned upstream font in `SOURCE.md` (the `sports_esports` precedent, `a3c838d`).

## Ownership

- `sysc-launch`: `prefix.go` (`Provider` fields, registry assembly, overview), `service.go`
  (`ServiceConfig.Providers`, row → provider map, activation routing, history), `entry.go`
  (`Result.Action`). No change to `score.go`, `apps.go` or `history.go`.
- `internal/calc`, `internal/emoji`, `tools/emojigen`: pure, no shell imports.
- `internal/shell`: provider construction and wiring, `launcher_rank.go` action rows, row
  rendering for action rows, Notes migration, copy activation.
- `internal/render`: two glyphs in the Material subset.

## Testing

- sysc-launch: routing with appended providers, duplicate and malformed prefixes skipped, inline
  rows placed first, activation routed by producing provider (including an inline row under a bare
  query), history recorded for provider activations, overview order, `Result.Action` round trip.
- `internal/calc`: table tests over the grammar, precedence and associativity, errors, formatting,
  and the inline rule's negatives.
- `internal/emoji`: ranking classes and order, cap, empty query, the table's integrity (every row
  valid UTF-8, no skin-tone modifiers, no duplicate sequences).
- `internal/shell`: action rows ranked below their app and absent when browsing, activation of an
  action row, calculator and emoji copy requests carrying the activating serial, Notes behaviour
  unchanged, overview contents.
- Live gate on Niri: `6*7` shows `= 42` above apps and Enter pastes `42` elsewhere; `/emo party`
  finds 🎉 and pastes it; `/calc` hint rows; "firefox private" finds the private-window action and
  opens it; `/` overview lists four providers with glyphs; `/nt` behaves as before.

## Non-goals

Windows provider. App grid, compact mode, category chips. Skin-tone selection. Unit and currency
conversion. Plugin-contributed providers. A calculator history.

## Amendment 2026-09-27 — settled by the rendered mockups (owner-approved)

The owner approved this design subject to a mockup of the finished launcher. The mockups in
`assets/2026-09-27-launcher-providers/` were rendered by the shell's own launcher panel and painter
(real theme, real sysc-launch scan, real app icons, the two new glyphs built from the pinned Material
font), and they settled five details:

1. **D7's emoji glyph is `mood`, not `emoji_emotions`.** Material Symbols Rounded at the pinned
   commit has no `emoji_emotions` (that name is from the older Material Icons set); `mood` is its
   smiley glyph.
2. **Provider rows fill the icon slot by convention.** `Entry.IconName` of `glyph:<name>` draws a
   Material subset glyph in the tinted slot; `text:<s>` draws `s` itself at the display rung on an
   untinted slot, so an emoji reads in colour. Anything else keeps today's icon-theme lookup and
   letter fallback.
3. **Hint rows.** A row with an empty `Entry.ID` explains rather than acts ("Invalid expression"):
   muted text, never highlighted, ignored by activation, not counted by the footer.
4. **The footer pluralises and counts only results**: "1 result", "3 apps", and "No results" when
   only hints show. (Today it reads "1 results".)
5. **`Provider.Activate` also receives the routed query**: `Activate(query, id, action string)`.
   Notes needs its capture body, which lives in the query, and the calculator re-evaluates from it.
   `ServiceConfig.ApplicationsGlyph` sets the Applications row's overview glyph (default
   `PlaceholderGlyph`), so the shell can give it `glyph:apps`.

Real data also showed that "private window" matches Firefox and Zen but not Brave, whose action is
"New Incognito Window" — the ranking works on the names apps actually ship.
