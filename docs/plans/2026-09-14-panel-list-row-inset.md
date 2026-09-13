# Panel List Row Inset Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Give Wi-Fi access-point row glyphs a density-aware inset while preserving the scoped small-radius shape and all other shell controls.

**Architecture:** `networkAPRow` owns the row's visual composition, so it will pass the existing `theme.Metrics.ButtonPadding` through the `ui.Node` padding field. The fixed-height button layout already uses that field for horizontal content bounds and keeps vertical centring; no renderer or global button change is needed. A shared `panelListRow` constructor remains deferred until a second panel proves the same contract.

**Tech Stack:** Go, retained `ui.Node` tree, native Go renderer, standard `testing` package.

---

### Task 1: Enforce and apply the AP row edge inset

**Files:**

- Modify: `internal/shell/popout_network_test.go:307-345`
- Modify: `internal/shell/popout_network.go:247-251`

**Step 1: Strengthen the existing failing alignment test**

Change the current edge assertions so they require the existing button inset:

```go
if got, want := signal.Bounds.X, row.Bounds.X+m.ButtonPadding; got != want {
	t.Errorf("%s: signal X = %d, want %d", ap.SSID, got, want)
}
```

For secured and active rows, change the trailing assertion to:

```go
if got, want := trailing.Bounds.X+trailing.Bounds.W, row.Bounds.X+row.Bounds.W-m.ButtonPadding; got != want {
	t.Errorf("%s: trailing right edge = %d, want %d", ap.SSID, got, want)
}
```

Keep the stable-leading-X assertion and all existing icon-presence checks.

**Step 2: Run the alignment test and confirm RED**

Run:

```bash
GOMAXPROCS=4 go test -p 4 ./internal/shell -run '^TestAccessPointRowsKeepLeadingAndTrailingIconsAligned$'
```

Expected: FAIL because the current row has zero padding, so the signal starts at the row edge and the trailing glyph ends at it.

**Step 3: Apply the existing density-aware padding**

Add `Padding: m.ButtonPadding` to the AP button while retaining `Shape: ui.ShapeSmall`:

```go
return &ui.Node{
	Kind: ui.KindButton, Action: "network-ap:" + ap.SSID, Name: ap.SSID,
	Role: "button", Focusable: true, Height: m.StandardControl,
	Padding: m.ButtonPadding, Shape: ui.ShapeSmall,
	Children: []*ui.Node{content},
}
```

Do not modify `KindButton` defaults, `layoutButtonContent`, `pinRowEnd`, tabs, the header, the surrounding card, or any other panel.

**Step 4: Run the focused checks and formatting**

```bash
gofmt -w internal/shell/popout_network.go internal/shell/popout_network_test.go
GOMAXPROCS=4 go test -p 4 ./internal/shell -run '^(TestAccessPointRowUsesSmallCornerShape|TestAccessPointRowsKeepLeadingAndTrailingIconsAligned|TestConnectedRowCarriesCheckWithoutFilledHighlight)$'
```

Expected: PASS. Both edge icons sit one `ButtonPadding` inside the row, while shape and connected-row semantics remain intact.

**Step 5: Run the repository gates**

Run outside the socket-restricted sandbox:

```bash
test -z "$(gofmt -l .)"
go vet ./...
GOMAXPROCS=4 go test -p 4 -count=1 ./...
GOMAXPROCS=4 go test -race -p 4 -count=1 ./internal/ui ./internal/render ./internal/shell ./internal/services
git diff --exit-code -- go.mod go.sum
```

Expected: all commands exit zero. Keep the race run package-scoped to avoid the machine's repo-wide race-build limit.

**Step 6: Commit the code and test**

```bash
git add internal/shell/popout_network.go internal/shell/popout_network_test.go
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "fix(ui): inset network list icons"
```

### Task 2: Deploy the exact build for visual confirmation

**Files:** None.

**Step 1: Build and identify the candidate**

```bash
GOMAXPROCS=4 go build -p 4 -o /tmp/sysc-shell-panel-list-inset ./cmd/sysc-shell
sha256sum /tmp/sysc-shell-panel-list-inset
```

**Step 2: Copy and restart on the laptop**

```bash
scp -P 7777 /tmp/sysc-shell-panel-list-inset nomadx@192.168.0.64:/tmp/sysc-shell-panel-list-inset
ssh -p 7777 nomadx@192.168.0.64 'cp ~/.local/bin/sysc-shell /tmp/sysc-shell.before-panel-list-inset && mv /tmp/sysc-shell-panel-list-inset ~/.local/bin/sysc-shell && systemctl --user restart sysc-shell.service && systemctl --user is-active sysc-shell.service'
```

Expected: `active`. Do not toggle laptop Wi-Fi because the SSH session depends on it.

**Step 3: Inspect the open network panel**

Confirm that the signal glyph moves inward from the left edge and each lock/check glyph moves inward from the right edge. Confirm that labels, row height, small-radius corners, tabs, header well, hover/press feedback, and backdrop blur match the accepted build.

If the visual gate fails, restore `/tmp/sysc-shell.before-panel-list-inset` and restart the service before changing the contract.
