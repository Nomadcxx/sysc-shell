# Toast and Notification Card Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Toast cards become see-through layout-C cards blurred by the compositor, and notification centre rows share the same text block (GitHub #28, #30).

**Architecture:** `notifycard.go` builds the new tree: a 32 px lead, a pinned summary/time row, one muted body line, action pills packed by measured width. The toast host paints each card as the surface ground itself: panel opacity plus rim when the compositor blurs, overlay opacity otherwise. It hands the compositor each card's silhouette through `BlurShape`. Cards size to their content.

**Tech Stack:** Go, sysc-shell `internal/ui` layout, `internal/render` painter, `ext_background_effect_v1` through the existing `HostCallbacks.BlurShape`.

**Spec:** `docs/plans/2026-09-28-toast-redesign-design.md`

## Global Constraints

- Work in `.worktrees/feature/toast-redesign` (branch `feature/toast-redesign`). Never run `bd` there; run it from `/home/nomadx/sysc-shell`.
- Go test caps: never run uncapped `./...`; use `go test -p 2 <pkg>` and `-race` per package only.
- Commit with `git -c core.hooksPath=/dev/null commit` after checking the message with `~/.git-hooks/commit-msg <file>`: the bd pre-commit hook would flush the wrong database in a worktree. Messages must not contain `bot` (so no "both", "bottom"), `agent`, `claude`, or a Co-Authored-By trailer.
- Lead size 32; icon rasters resolve at 64 so scaled outputs stay sharp.
- Glass applies only when `cfg.Theme.BlurBehind && caps.Blur` (the condition panels use).
- Toast width stays `toastCardWidth` (380); stack placement, slide animation, expiry and hover-pause are out of scope.

## Review Focus

1. **Six or long action labels** must still fit: a pill row wider than the card makes layout fail and the shell drops the toast. Pinned by `TestNotifyCardPacksActionsIntoRowsThatFit` (Task 1), which runs `ui.CheckFit` against real-ish widths.
2. **A summary long enough to clip** must leave the time whole. Pinned by `TestNotifyCardKeepsTheTimeBesideALongSummary` (Task 1).
3. **A notification with no summary** still has a headline, so the card never shows only a time. Pinned by `TestNotifyCardFallsBackToTheAppNameWithoutASummary` (Task 1).
4. **The compositor losing blur at runtime** (Niri restart, capability change) must drop glass and the blur region together, never leaving a translucent card over an unblurred desktop. Pinned by `TestToastWithoutCompositorBlurKeepsTheOverlayGround` and `TestToastBlurShapeIsEmptyWithoutCompositorBlur` (Tasks 2–3).
5. **The last card closing** must clear the blur region, or a blurred ghost stays in the corner. Pinned by `TestToastBlurShapeIsEmptyWithNoCards` (Task 3).

---

### Task 1: Layout-C card tree, shared with the notification centre

**Files:**
- Modify: `internal/shell/notifycard.go` (constants, lead, tree, toast card, action packing, centre stroke)
- Modify: `internal/shell/toasthost.go:547-570` (`cardFor`, `wrapBody`)
- Modify: `internal/shell/popout_notifications.go:275-283` (delete `cloneLifetime`), `:300` (raster size)
- Test: `internal/shell/notifycard_test.go`, `notify_polish_test.go`, `notifywrap_test.go`, `notifyactions_test.go`, `toasthost_test.go:581`

**Interfaces:**
- Produces: `NotificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText) *ui.Node` and `ExpandedNotificationCard(n, raster, allowLinks, measure, wrap func(string) []string) *ui.Node`. The lifetime parameter is gone (no timeout line). A nil `measure` stacks each action on its own full-width row.
- Produces: `actionRows(actions []protocol.Action, width int, measure ui.MeasureText) [][]protocol.Action`.
- Produces: constants `cardIconSize = 32`, `notifyIconRaster = 64`, `cardLeadGap = 10`, `cardSectionGap = 8`.
- The toast tree has no chrome: its root is a padded column whose first child is the lead row. Tasks 2–3 rely on that.

- [ ] **Step 1: Rewrite the tests that pin the old shape, and add the new ones**

In `notifycard_test.go`, change every `NotificationCard(X, nil, nil, B)` to `NotificationCard(X, nil, B, nil)`:

```bash
perl -0pi -e 's/NotificationCard\((baseNotification\(\)|n|protocol\.Notification\{[^{}]*\}), nil, nil, (true|false)\)/NotificationCard($1, nil, $2, nil)/g' internal/shell/notifycard_test.go internal/shell/notify_polish_test.go internal/shell/notifyactions_test.go
grep -n 'NotificationCard(' internal/shell/*_test.go
```

Every remaining call must have the form `(…, nil, <bool>, nil)`. Then fix `notifywrap_test.go:29` by hand:

```go
	root := ExpandedNotificationCard(protocol.Notification{ID: 1, Body: "one two"}, nil, false, nil,
		func(string) []string { return []string{"one", "two"} })
```

Change `toasthost_test.go:581` from `icons.Square("firefox", cardIconSize)` to `icons.Square("firefox", notifyIconRaster)`.

Replace these tests in `notifycard_test.go` whole:

```go
func TestNotifyCardShowsSummaryBodyAndNamesTheApp(t *testing.T) {
	card := NotificationCard(baseNotification(), nil, true, nil)
	joined := strings.Join(texts(card), "\n")
	for _, want := range []string{"Two new messages", "one"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("card text %q lacks %q", joined, want)
		}
	}
	if strings.Contains(joined, "two") {
		t.Fatalf("collapsed card showed the second body line: %q", joined)
	}
	if lead := card.Children[0].Children[0]; lead.Name != "Mail" {
		t.Fatalf("lead name = %q, want the app name", lead.Name)
	}
}

func TestNotifyCardPreservesBodyStyles(t *testing.T) {
	n := baseNotification()
	n.Body = "one <b>two</b>"
	card := NotificationCard(n, nil, true, nil)
	styled := textNode(card, "two")
	if styled == nil || !styled.Bold {
		t.Fatalf("bold run lost its style: %+v", styled)
	}
}

func TestNotifyCardAppliesUrgencyToneToTheSummary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		urgency protocol.Urgency
		want    ui.Tone
	}{
		{name: "low", urgency: protocol.UrgencyLow, want: ui.ToneSubtle},
		{name: "normal", urgency: protocol.UrgencyNormal, want: ui.ToneNormal},
		{name: "critical", urgency: protocol.UrgencyCritical, want: ui.ToneError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := baseNotification()
			n.Urgency = tc.urgency
			card := NotificationCard(n, nil, true, nil)
			if node := textNode(card, "Two new messages"); node == nil || node.Tone != tc.want {
				t.Fatalf("summary = %+v, want tone %v", node, tc.want)
			}
			if node := textNode(card, "one"); node == nil || node.Tone != ui.ToneSubtle {
				t.Fatalf("body = %+v, want subtle", node)
			}
		})
	}
}

func TestNotifyToastHasNoTimeoutMeter(t *testing.T) {
	var meters []*ui.Node
	collectByKind(NotificationCard(baseNotification(), nil, true, nil), ui.KindMeter, &meters)
	if len(meters) != 0 {
		t.Fatalf("toast carries meters %+v, want none without a value", meters)
	}
}

func TestNotifyCardOmitsCriticalBang(t *testing.T) {
	n := baseNotification()
	n.Urgency = protocol.UrgencyCritical
	card := NotificationCard(n, nil, true, nil)
	for _, s := range texts(card) {
		if s == "!" {
			t.Fatal("critical toast still paints !")
		}
	}
	if s := strokeOf(card); s != 0 {
		t.Fatalf("toast tree stroke = %d, want 0: the host paints the rim", s)
	}
	now := time.Unix(1_756_000_000, 0)
	history := HistoryCard(protocol.HistoryEntry{ID: 3, Summary: "Low", Timestamp: now, Urgency: protocol.UrgencyCritical}, now, nil, false)
	if s := strokeOf(history); s != 1 {
		t.Fatalf("critical history stroke = %d, want 1", s)
	}
}

func TestNotifyCardKeepsTheTimeBesideALongSummary(t *testing.T) {
	n := baseNotification()
	n.Summary = strings.Repeat("A very long summary that cannot fit ", 4)
	now := n.Timestamp
	card := notificationCard(n, nil, true, nil, nil, now)
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 8, 16 }
	if err := ui.LayoutColumn(card, ui.Rect{W: toastCardWidth, H: 400}, measure); err != nil {
		t.Fatal(err)
	}
	stamp := textNode(card, formatNotifyTime(now, now))
	if stamp == nil {
		t.Fatal("no time node")
	}
	if want, _ := measure(stamp.Text, ui.TextAttrs{}); stamp.Bounds.W < want {
		t.Fatalf("time clipped to %d px, want %d", stamp.Bounds.W, want)
	}
}

func TestNotifyCardFallsBackToTheAppNameWithoutASummary(t *testing.T) {
	n := baseNotification()
	n.Summary = ""
	if textNode(NotificationCard(n, nil, true, nil), "Mail") == nil {
		t.Fatal("card without a summary has no headline")
	}
}

func TestNotifyCardPacksActionsIntoRowsThatFit(t *testing.T) {
	n := baseNotification()
	n.Actions = []protocol.Action{
		{Key: "a1", Label: "Open in browser"}, {Key: "a2", Label: "Mark as read"},
		{Key: "a3", Label: "Archive"}, {Key: "a4", Label: "Reply"},
		{Key: "a5", Label: "Snooze for an hour"}, {Key: "a6", Label: "Mute conversation forever and ever"},
	}
	measure := func(s string, _ ui.TextAttrs) (int, int) { return len([]rune(s)) * 9, 18 }
	card := NotificationCard(n, nil, true, measure)
	h, err := ui.ContentHeight(card, toastCardWidth, measure)
	if err != nil {
		t.Fatal(err)
	}
	if problems := ui.CheckFit(card, ui.Rect{W: toastCardWidth, H: h}, measure); len(problems) != 0 {
		t.Fatalf("actions do not fit: %+v", problems)
	}
	if got := len(buttons(card)); got != 6 {
		t.Fatalf("buttons = %d, want 6", got)
	}
	var rows []*ui.Node
	collectByKind(card, ui.KindRow, &rows)
	packed := false
	for _, r := range rows {
		if len(r.Children) > 1 && r.Children[0].Kind == ui.KindButton {
			packed = true
		}
	}
	if !packed {
		t.Fatal("no two short actions shared a row")
	}
}
```

Delete the old `TestNotifyCardShowsSummaryBodyAndApp`, `TestNotifyCardAppliesUrgencyToneToIdentitySummaryAndBody` and `TestNotifyCardCountdownUsesTheAuthoritativeLifetime`. In `TestActiveGroupCardShowsCountDismissAndExpand`, change the wanted list to `[]string{"new", "b", "2"}`.

In `notify_polish_test.go`, replace `TestNotifyToastKeepsUnfilledCard` with:

```go
func TestNotifyToastIsItsOwnGround(t *testing.T) {
	toast := NotificationCard(baseNotification(), nil, false, nil)
	if toast.Kind != ui.KindColumn || toast.Padding != cardPadding || toast.Children[0].Kind != ui.KindRow {
		t.Fatalf("toast root = %+v, want a padded column leading with the lead row", toast)
	}
	now := time.Now()
	live := ActiveGroupCard(activeGroup{members: []protocol.Notification{baseNotification()}}, now, false, nil, false)
	history := HistoryCard(protocol.HistoryEntry{ID: 1, Summary: "history"}, now, nil, false)
	for _, card := range []*ui.Node{live, history} {
		if card.Children[0].Fill != ui.FillContainerHigh {
			t.Fatal("centre card lost its high container fill")
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test -p 2 -run 'Notify|Toast|Expanded|VerticalDrag|History|ActiveGroup' ./internal/shell/`
Expected: build failure (`too many arguments` / `undefined: notifyIconRaster`).

- [ ] **Step 3: Implement the tree in `notifycard.go`**

Replace the constant block, `appLetter`, `iconSlot`, `timeoutMeter`, `wrapNotifyCard`, `notificationTree`, `NotificationCard`, `ExpandedNotificationCard` and `notificationCard` with:

```go
const (
	// cardIconSize is the lead a card opens with: the notification's image
	// or a glyph tile.
	cardIconSize = 32
	// notifyIconRaster is the size icons resolve at, twice the lead, so the
	// lead stays sharp on a scaled output.
	notifyIconRaster = 64
	cardGap          = 6
	cardPadding      = 12
	cardLeadGap      = 10
	cardSectionGap   = 8
	cardGlyphSize    = 18
	centreIconSize   = 20
	centreIconPad    = 6
)

// leadSlot is the card's lead: the notification's own image, or a bell in a
// tinted tile. It carries the app name, since layout C has no app line.
func leadSlot(app string, raster *ui.Image, urgency protocol.Urgency) *ui.Node {
	if raster != nil {
		return &ui.Node{Kind: ui.KindImage, Image: raster, ImageSize: cardIconSize, Name: app}
	}
	fill, tone := ui.FillContainerHighest, ui.ToneNormal
	if urgency == protocol.UrgencyCritical {
		fill, tone = ui.FillErrorContainer, ui.ToneError
	}
	return &ui.Node{
		Kind: ui.KindCapsule, Fill: fill, Width: cardIconSize, Height: cardIconSize, Shape: ui.ShapeMedium,
		CenterX: true, CenterY: true, Name: app,
		Children: []*ui.Node{{Kind: ui.KindIcon, Icon: "notifications", IconSize: cardGlyphSize, Tone: tone}},
	}
}

// wrapNotifyCard is the centre's card chrome. A critical entry strokes the
// error colour, the same edge a critical toast paints.
func wrapNotifyCard(inner *ui.Node, critical bool, fill ui.Fill) *ui.Node {
	cap := &ui.Node{
		Kind: ui.KindCapsule, Fill: fill, Padding: cardPadding, Shape: ui.ShapeCard,
		Action: inner.Action, Children: []*ui.Node{inner},
	}
	if critical {
		cap.Stroke, cap.StrokeFill = 1, ui.FillError
	}
	return cap
}

// bodyNodes is the body under the headline. Collapsed, it is the runs up to
// the first break on one row, so a styled word stays in its sentence and the
// row clips at the card edge. Expanded, each run wraps over its own lines.
func bodyNodes(id uint32, body string, allowLinks bool, wrap func(string) []string) []*ui.Node {
	run := func(r Run, text string) *ui.Node {
		node := &ui.Node{Kind: ui.KindText, Text: text, Bold: r.Bold, Italic: r.Italic, Underline: r.Underline, Tone: ui.ToneSubtle}
		if r.Link {
			node.Action = fmt.Sprintf("notify:%d:link:%s", id, r.Href)
		}
		return node
	}
	runs := ParseBody(body, allowLinks)
	if wrap == nil {
		line := &ui.Node{Kind: ui.KindRow}
		for _, r := range runs {
			if r.Break {
				if len(line.Children) > 0 {
					break
				}
				continue
			}
			if r.Text != "" {
				line.Children = append(line.Children, run(r, r.Text))
			}
		}
		switch len(line.Children) {
		case 0:
			return nil
		case 1:
			return line.Children
		}
		return []*ui.Node{line}
	}
	var out []*ui.Node
	for _, r := range runs {
		if r.Break || r.Text == "" {
			continue
		}
		for _, text := range wrap(r.Text) {
			if text != "" {
				out = append(out, run(r, text))
			}
		}
	}
	return out
}

// notificationTree is the text block every notification card shares: the
// lead beside a headline row, with the time pinned right so a long summary
// clips before the time does, then the body and any value bar.
func notificationTree(id uint32, app, summary, body string, urgency protocol.Urgency, raster *ui.Image, value *int32, allowLinks bool, wrap func(string) []string, now, ts time.Time) *ui.Node {
	headline := summary
	if headline == "" {
		headline = app
	}
	head := &ui.Node{Kind: ui.KindRow, Gap: cardGap, Children: []*ui.Node{
		{Kind: ui.KindText, Text: headline, TextRole: theme.RoleFigure, Tone: toneFor(urgency)},
	}}
	if !ts.IsZero() && !now.IsZero() {
		head.PinEnd = true
		head.Children = append(head.Children, &ui.Node{
			Kind: ui.KindText, Text: formatNotifyTime(ts, now), TextRole: theme.RoleCaption, Tone: ui.ToneSubtle,
		})
	}
	text := &ui.Node{Kind: ui.KindColumn, Gap: 1, Children: []*ui.Node{head}}
	text.Children = append(text.Children, bodyNodes(id, body, allowLinks, wrap)...)
	if m := valueMeter(value); m != nil {
		text.Children = append(text.Children, m)
	}
	return &ui.Node{Kind: ui.KindRow, Gap: cardLeadGap, Children: []*ui.Node{
		leadSlot(app, raster, urgency),
		text,
	}}
}

// NotificationCard builds one active toast. raster is the already decoded
// icon, or nil for the glyph tile. measure packs action pills into rows; nil
// stacks them one per row.
func NotificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText) *ui.Node {
	return notificationCard(n, raster, allowLinks, measure, nil, time.Now())
}

// ExpandedNotificationCard is a toast whose body is wrapped over several
// lines after a vertical drag.
func ExpandedNotificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText, wrap func(string) []string) *ui.Node {
	return notificationCard(n, raster, allowLinks, measure, wrap, time.Now())
}

// notificationCard has no chrome of its own: the toast host paints the card
// ground, rim and blur around it.
func notificationCard(n protocol.Notification, raster *ui.Image, allowLinks bool, measure ui.MeasureText, wrap func(string) []string, now time.Time) *ui.Node {
	if raster == nil {
		raster = protocolImage(n.Image)
	}
	root := &ui.Node{Kind: ui.KindColumn, Gap: cardSectionGap, Padding: cardPadding, Children: []*ui.Node{
		notificationTree(n.ID, n.AppName, n.Summary, n.Body, n.Urgency, raster, n.Value, allowLinks, wrap, now, n.Timestamp),
	}}

	hasDefault := false
	var pills []protocol.Action
	for _, a := range n.Actions {
		if a.Key == "default" {
			hasDefault = true
			markDefault(root, n.ID)
			continue
		}
		pills = append(pills, a)
	}
	for _, row := range actionRows(pills, toastCardWidth-2*cardPadding, measure) {
		if len(row) == 1 {
			root.Children = append(root.Children, actionPill(n.ID, row[0]))
			continue
		}
		line := &ui.Node{Kind: ui.KindRow, Gap: cardGap}
		for _, a := range row {
			line.Children = append(line.Children, actionPill(n.ID, a))
		}
		root.Children = append(root.Children, line)
	}
	if !hasDefault {
		root.Action = fmt.Sprintf("notify:%d:dismiss", n.ID)
	}
	if n.InlineReply {
		root.Children = append(root.Children, &ui.Node{
			Kind: ui.KindTextField,
			Name: "Reply", Role: "text", Focusable: true,
			Action: fmt.Sprintf("notify:%d:reply", n.ID),
		})
	}
	return root
}

func actionPill(id uint32, a protocol.Action) *ui.Node {
	return &ui.Node{
		Kind: ui.KindButton, Text: a.Label, TextRole: theme.RoleLabel, Fill: ui.FillContainerHighest,
		Shape: ui.ShapeMedium, Padding: theme.MarginXS,
		Action: fmt.Sprintf("notify:%d:action:%s", id, a.Key),
		Name:   a.Label, Role: "button", Focusable: true,
	}
}

// actionRows packs action pills into rows no wider than width, keeping their
// order. A pill that fits nowhere beside another takes a row alone, which the
// card lays out as a full-width button, so no label can push layout past the
// card. A nil measure cannot size a pill, so every action takes its own row.
func actionRows(actions []protocol.Action, width int, measure ui.MeasureText) [][]protocol.Action {
	var rows [][]protocol.Action
	used := 0
	for _, a := range actions {
		w := width + 1
		if measure != nil {
			tw, _ := measure(a.Label, ui.TextAttrs{Role: theme.RoleLabel})
			w = tw + 2*theme.MarginXS
		}
		if len(rows) == 0 || used+cardGap+w > width {
			rows = append(rows, []protocol.Action{a})
			used = w
			continue
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], a)
		used += cardGap + w
	}
	return rows
}
```

`HistoryCard` and `ActiveGroupCard` keep their bodies; their `wrapNotifyCard(…)` calls now get the stroke instead of the chip. `Run` is the type `ParseBody` returns (`internal/shell/notifytext.go:49`).

- [ ] **Step 4: Update the callers**

In `toasthost.go`, replace `cardFor` and `wrapBody`:

```go
// cardFor projects one active record. A record that has gone between the
// placement and the paint yields nothing rather than an empty card.
func (h *toastHost) cardFor(id uint32) *ui.Node {
	s := h.r.notify
	s.mu.Lock()
	notification, ok := s.active[id]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	icon := h.r.notifyIcon(notification.AppIcon, notification.DesktopEntry)
	if h.expanded[id] {
		return ExpandedNotificationCard(notification, icon, h.r.linksAllowed(), h.measureText(), h.wrapBody)
	}
	return NotificationCard(notification, icon, h.r.linksAllowed(), h.measureText())
}

func (h *toastHost) wrapBody(s string) []string {
	measure := h.measureText()
	width := toastCardWidth - 2*cardPadding - cardIconSize - cardLeadGap
	return wrapLines(s, width, func(text string) int {
		w, _ := measure(text, ui.TextAttrs{})
		return w
	}, 8)
}
```

In `popout_notifications.go`, delete `cloneLifetime` (lines 275–283) after `grep -rn cloneLifetime internal/` shows no other caller, and change `icons.Square(name, cardIconSize)` to `icons.Square(name, notifyIconRaster)`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `gofmt -l internal/shell; go vet ./internal/shell/ && go test -p 2 ./internal/shell/`
Expected: no gofmt output, `ok`. If a centre layout test fails on width, the fix belongs in the tree (lead 32 plus gap 10 is narrower than the old 56 plus 6, so widths only shrink).

- [ ] **Step 6: Commit**

```bash
git add internal/shell/notifycard.go internal/shell/toasthost.go internal/shell/popout_notifications.go internal/shell/*_test.go
git -c core.hooksPath=/dev/null commit -F <msgfile>
```
Message: `feat(notify): layout C cards for toasts and the centre` with a body naming the 32 px lead, pinned time, one body line, packed action pills, dropped timeout line, and `Refs #30`.

---

### Task 2: See-through toast ground, rim and content height

**Files:**
- Modify: `internal/shell/toasthost.go` (`toastCard`, `rebuild`, `paintCard`, `cardHeight`, new `glass`, `cardStyle`, `critical`)
- Modify: `internal/shell/toastlayout.go:81-97` (`toastCardHeight`)
- Test: `internal/shell/toasthost_test.go`, `internal/shell/toastlayout_test.go:25-43`

**Interfaces:**
- Consumes: Task 1's chrome-less `NotificationCard` tree.
- Produces: `func (h *toastHost) glass() bool` (caller holds `r.mu`), used by Task 3; `toastCard.critical bool`; `toastCardHeight(root *ui.Node, width int, measure ui.MeasureText) int`.

- [ ] **Step 1: Write the failing tests**

In `toastlayout_test.go`, change the call at line 40 to `toastCardHeight(root, toastCardWidth, measure)` and assert equality:

```go
	if got := toastCardHeight(root, toastCardWidth, measure); got != h {
		t.Fatalf("card height = %d, want exactly the content height %d", got, h)
	}
```

Append to `toasthost_test.go`:

```go
// glassToasts opens one output with a single configured toast and paints it.
func glassToasts(t *testing.T, blur bool, notes ...protocol.Notification) (*Registry, *toastHost, []byte, int) {
	t.Helper()
	cfg := config.Default()
	cfg.Theme.BlurBehind = true
	// Presets set panel and overlay opacity equal; separate them so the
	// ground tests can tell which style painted.
	cfg.Theme.PanelOpacity = 65
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	r.mu.Lock()
	r.caps.Blur = blur
	r.mu.Unlock()
	h := newToastHost(r, &hostHarness{})
	r.outputsForTest([]string{"eDP-1"})
	h.syncOutputs(map[string]uint32{"eDP-1": 5})
	r.applyNotify(snap(1, notes...))
	const width, height = 1200, 800
	cb := h.harness().opens[0].Callbacks
	if err := cb.Configure(width, height, 120); err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, width*4*height)
	for range 2 {
		if err := cb.Render(pixels, width, height, width*4); err != nil {
			t.Fatal(err)
		}
	}
	return r, h, pixels, width * 4
}

func TestToastCardIsItsContentHeight(t *testing.T) {
	r, h, _, _ := glassToasts(t, true, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	want, err := ui.ContentHeight(h.cardFor(1), toastCardWidth, h.measureText())
	if err != nil {
		t.Fatal(err)
	}
	if got := h.cards["eDP-1"][0].rect.H; got != want {
		t.Fatalf("card height = %d, want content height %d", got, want)
	}
}

// groundAlpha is the alpha just inside a card's lower-left corner, clear of
// the rim and of any content.
func groundAlpha(h *toastHost, pixels []byte, stride int) byte {
	rect := h.cards["eDP-1"][0].rect
	return pixels[(rect.Y+rect.H-6)*stride+(rect.X+6)*4+3]
}

func TestToastGroundFollowsPanelOpacityUnderCompositorBlur(t *testing.T) {
	r, h, pixels, stride := glassToasts(t, true, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	want := r.surfaceTheme().PanelStyle().RootFill().A
	if want == r.surfaceTheme().OverlayStyle().RootFill().A {
		t.Fatal("panel and overlay grounds match; the test cannot tell them apart")
	}
	if got := groundAlpha(h, pixels, stride); got != want {
		t.Fatalf("ground alpha = %d, want the panel's %d", got, want)
	}
}

func TestToastWithoutCompositorBlurKeepsTheOverlayGround(t *testing.T) {
	r, h, pixels, stride := glassToasts(t, false, note(1, "hello"))
	r.mu.Lock()
	defer r.mu.Unlock()
	want := r.surfaceTheme().OverlayStyle().RootFill().A
	if got := groundAlpha(h, pixels, stride); got != want {
		t.Fatalf("ground alpha = %d, want the overlay's %d", got, want)
	}
}

func TestToastCriticalCardStrokesTheErrorRim(t *testing.T) {
	critical := note(1, "battery")
	critical.Urgency = protocol.UrgencyCritical
	r, h, pixels, stride := glassToasts(t, true, critical)
	r.mu.Lock()
	defer r.mu.Unlock()
	rect := h.cards["eDP-1"][0].rect
	if !h.cards["eDP-1"][0].critical {
		t.Fatal("critical card not marked")
	}
	o := (rect.Y+rect.H/2)*stride + rect.X*4
	px := render.Color{B: pixels[o], G: pixels[o+1], R: pixels[o+2]}
	errC, rim := h.style.Error, h.style.Rim
	dist := func(a, b render.Color) int {
		d := func(x, y uint8) int { v := int(x) - int(y); return v * v }
		return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
	}
	if dist(px, errC) >= dist(px, rim) {
		t.Fatalf("edge pixel %+v is nearer the outline %+v than the error %+v", px, rim, errC)
	}
}
```

Add `"github.com/Nomadcxx/sysc-shell/internal/render"` to the test imports if missing.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test -p 2 -run 'ToastCard|ToastGround|ToastWithout|ToastCritical|CardHeight' ./internal/shell/`
Expected: build failure (`too many arguments to toastCardHeight`, `critical undefined`).

- [ ] **Step 3: Implement**

`toastlayout.go`:

```go
// toastCardHeight is the tree's intrinsic height. The card is the surface
// ground itself, so nothing needs room around the tree. A missing tree or
// measure falls back to 96 so the card still places.
func toastCardHeight(root *ui.Node, width int, measure ui.MeasureText) int {
	if root == nil || measure == nil {
		return 96
	}
	ht, err := ui.ContentHeight(root, width, measure)
	if err != nil || ht <= 0 {
		return 96
	}
	return ht
}
```

`toasthost.go`: `cardHeight` becomes `return toastCardHeight(h.cardFor(id), toastCardWidth, h.measureText())`. Add a field and helpers:

```go
type toastCard struct {
	root     *ui.Node
	rect     ui.Rect
	critical bool
}

// glass reports whether toast cards take the see-through panel ground: only
// when blur-behind is on and the compositor blurs, the condition panels use.
// Caller holds r.mu.
func (h *toastHost) glass() bool { return h.r.cfg.Theme.BlurBehind && h.r.caps.Blur }

// cardStyle is the ground toast cards paint on. Under compositor blur a card
// is a small floating panel: the panel's opacity, which follows the global
// panel-opacity setting. Without blur it keeps the overlay's higher floor,
// chosen for a surface with nothing behind it. Either way it strokes the
// panel rim. Caller holds r.mu.
func (h *toastHost) cardStyle() render.Style {
	t := h.r.surfaceTheme()
	s := t.OverlayStyle()
	if h.glass() {
		s = t.PanelStyle()
	}
	s.Rim = t.Outline
	return s
}

// critical reports whether an active record is critical, which strokes its
// card's rim in the error colour.
func (h *toastHost) critical(id uint32) bool {
	s := h.r.notify
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[id].Urgency == protocol.UrgencyCritical
}
```

In `rebuild`, replace `h.style = h.r.surfaceTheme().OverlayStyle()` with `h.style = h.cardStyle()` (keep the GitHub #29 comment) and build cards as `toastCard{root: root, rect: rects[i], critical: h.critical(id)}`. In `paintCard`, after `cardStyle.Body = …` add:

```go
	if card.critical {
		cardStyle.Rim = style.Error
	}
```

Check `t.Outline` is a `render.Color` field on the shell `Theme` (`grep -n 'Outline ' internal/shell/theme.go`); `panelhost.go:1072` uses it the same way.

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -l internal/shell; go test -p 2 ./internal/shell/`
Expected: `ok`. `TestToastPaintLeavesTheGapsTransparent` must still pass: cards no longer carry a band, so the gap assertions are unchanged.

- [ ] **Step 5: Commit**

Message: `feat(toast): see-through cards at panel opacity` with a body naming the rim, the critical error rim, the overlay fallback without compositor blur, the removed `2*radius` band, and `Refs #28 #30`.

---

### Task 3: Compositor blur behind each card

**Files:**
- Modify: `internal/shell/toasthost.go` (`spec` gains `BlurShape`, its D13 comment is rewritten; new `blurShape`)
- Test: `internal/shell/toasthost_test.go`

**Interfaces:**
- Consumes: `h.glass()`, `toastCard`, `h.displayRect(connector, id, target)` (existing), `ui.BlurStrips(ui.SurfaceShape) []ui.Rect`.
- Produces: `func (h *toastHost) blurShape(connector string) []ui.Rect` (caller holds `r.mu`).

- [ ] **Step 1: Write the failing tests**

```go
func TestToastBlurShapeCoversEachCard(t *testing.T) {
	r, h, _, _ := glassToasts(t, true, note(1, "first"), note(2, "second"))
	shape := h.harness().opens[0].Callbacks.BlurShape()
	r.mu.Lock()
	cards := append([]toastCard(nil), h.cards["eDP-1"]...)
	r.mu.Unlock()
	if len(cards) != 2 || len(shape) == 0 {
		t.Fatalf("cards %d, strips %d", len(cards), len(shape))
	}
	area := map[int]int{}
	for _, s := range shape {
		inside := -1
		for i, c := range cards {
			if s.X >= c.rect.X && s.Y >= c.rect.Y && s.X+s.W <= c.rect.X+c.rect.W && s.Y+s.H <= c.rect.Y+c.rect.H {
				inside = i
			}
		}
		if inside < 0 {
			t.Fatalf("strip %+v lies outside every card", s)
		}
		area[inside] += s.W * s.H
	}
	for i, c := range cards {
		if full := c.rect.W * c.rect.H; area[i] < full*9/10 {
			t.Fatalf("card %d blur covers %d of %d px", i, area[i], full)
		}
	}
}

func TestToastBlurShapeIsEmptyWithoutCompositorBlur(t *testing.T) {
	_, h, _, _ := glassToasts(t, false, note(1, "first"))
	if shape := h.harness().opens[0].Callbacks.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur without compositor blur: %+v", shape)
	}
}

func TestToastBlurShapeIsEmptyWithNoCards(t *testing.T) {
	_, h, _, _ := glassToasts(t, true)
	if shape := h.harness().opens[0].Callbacks.BlurShape(); len(shape) != 0 {
		t.Fatalf("blur with no cards: %+v", shape)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test -p 2 -run 'ToastBlurShape' ./internal/shell/`
Expected: panic, nil `BlurShape` callback.

- [ ] **Step 3: Implement**

Replace the `spec` doc comment's last three lines (`blur-exempt: design D13 …`) with:

```go
// Each card blurs through the compositor: BlurShape hands it the cards'
// silhouettes, so nothing is captured and only the cards blur, not the
// output the surface spans (the cost design D13 exempted toasts over).
```

Add to `Callbacks` in `spec`:

```go
			BlurShape: func() []ui.Rect {
				h.r.mu.Lock()
				defer h.r.mu.Unlock()
				return h.blurShape(connector)
			},
```

And the method:

```go
// blurShape is the region the compositor blurs behind this output's toasts,
// in surface coordinates: each card's rounded silhouette where it draws this
// frame, so the blur follows a card as it slides. Without compositor blur the
// cards keep the overlay ground and nothing blurs. Caller holds r.mu.
func (h *toastHost) blurShape(connector string) []ui.Rect {
	if !h.glass() {
		return nil
	}
	var out []ui.Rect
	for _, card := range h.cards[connector] {
		body := card.rect
		if id, ok := cardID(card.root); ok {
			body = h.displayRect(connector, id, card.rect)
		}
		out = append(out, ui.BlurStrips(ui.SurfaceShape{Body: body, Radius: h.style.Radius})...)
	}
	return out
}
```

- [ ] **Step 4: Run to see them pass**

Run: `gofmt -l internal/shell; go vet ./internal/shell/ && go test -p 2 ./internal/shell/ && go test -race -p 2 -run 'Toast|Notify' ./internal/shell/`
Expected: `ok` for all three.

- [ ] **Step 5: Commit**

Message: `feat(toast): blur behind each card through the compositor` with `Fixes #28`.

---

### Task 4: Proof, tracker and landing

**Files:**
- Delete: `internal/shell/zz_toastshot_test.go` (throwaway render harness, never committed)
- Tracker: `.beads/issues.jsonl` (sysc-635, sysc-636), edited from origin's copy

- [ ] **Step 1: Render the result through the painter**

Before deleting the throwaway, point its `TestZZToastShot` at the real host (it already drives `toastHost`), set `r.caps.Blur = true` and `cfg.Theme.BlurBehind = true`, run `TOAST_SHOT_DIR=<scratch>/shots go test -p 2 -run TestZZToastShot ./internal/shell/`, composite with `compose.sh`/`compose2.sh`, and compare with the approved glass renders. Add an "After" row to the decision artifact. Then `rm internal/shell/zz_toastshot_test.go`.

- [ ] **Step 2: Full gates**

Run: `go build -p 2 ./... && go vet ./internal/shell/ ./internal/ui/ && go test -p 2 ./internal/shell/ ./internal/ui/ ./internal/render/ ./internal/platform/wayland/`
Expected: all `ok`.

- [ ] **Step 3: Tracker**

From `/home/nomadx/sysc-shell`: `bd update sysc-635 --title "Blur behind toast cards"`, then set sysc-636's description to "Toast density and typography: layout C (gh#30)." and close both with reasons citing the commits. Splice only those two records into `git show origin/main:.beads/issues.jsonl` (keep every other line byte for byte; confirm with a `diff` that excludes the two ids and a 530-line count) and commit on the branch.

- [ ] **Step 4: Live check**

Ask the owner before deploying. The desktop has no notification daemon (sysc-notify has been removed since 2026-09-22); the laptop runs one. Deploy only through `scripts/deploy --host laptop` from this branch's clean worktree, send a normal, an action and a critical notification with `notify-send`, screenshot with `grim`, and check the journal for `closing surface`, `does not fit` or a panic. After the branch lands, redeploy a clean origin/main build.

- [ ] **Step 5: Land**

Push `feature/toast-redesign` and open a PR titled `Toast and notification cards: layout C, see-through, compositor blur` with `Fixes #28`, `Fixes #30`, the before/after renders, and the test output.
