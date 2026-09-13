# Panel List Row Shape Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render Wi-Fi access-point buttons as small-radius rectangles without changing other shell buttons.

**Architecture:** `networkAPRow` owns the visual role because it constructs each access-point button. It will opt into the existing `ui.ShapeSmall` role; the renderer, theme shape ladder, and antialiased mask remain unchanged.

**Tech Stack:** Go, retained `ui.Node` tree, native Go renderer, standard `testing` package.

---

### Task 1: Opt access-point rows into the panel-list shape

**Files:**

- Modify: `internal/shell/popout_network_test.go`
- Modify: `internal/shell/popout_network.go:247`

**Step 1: Write the failing test**

Add this test beside the existing access-point row tests:

```go
func TestAccessPointRowUsesSmallCornerShape(t *testing.T) {
	row := networkAPRow(services.AccessPoint{SSID: "Test AP", Strength: 50}, standardMetrics())
	if row.Shape != ui.ShapeSmall {
		t.Fatalf("access-point row shape = %v, want ShapeSmall", row.Shape)
	}
}
```

**Step 2: Run the test and confirm the current stadium default fails**

Run:

```bash
GOMAXPROCS=4 go test -p 4 ./internal/shell -run '^TestAccessPointRowUsesSmallCornerShape$'
```

Expected: FAIL because `networkAPRow` leaves `Shape` as `ShapeInherit`.

**Step 3: Apply the existing shape role**

Add `Shape: ui.ShapeSmall` to the access-point button:

```go
return &ui.Node{
	Kind: ui.KindButton, Action: "network-ap:" + ap.SSID, Name: ap.SSID,
	Role: "button", Focusable: true, Height: m.StandardControl,
	Shape: ui.ShapeSmall, Children: []*ui.Node{content},
}
```

Do not change the global `KindButton` painter or create a list-row helper.

**Step 4: Run the focused checks**

Run:

```bash
gofmt -w internal/shell/popout_network.go internal/shell/popout_network_test.go
GOMAXPROCS=4 go test -p 4 ./internal/shell -run '^(TestAccessPointRowUsesSmallCornerShape|TestAccessPointRowsKeepLeadingAndTrailingIconsAligned|TestConnectedRowCarriesCheckWithoutFilledHighlight)$'
```

Expected: PASS. The shape assertion, icon alignment, and connected-row emphasis remain intact.

**Step 5: Run the repository gates**

Run outside the socket-restricted sandbox:

```bash
test -z "$(gofmt -l .)"
go vet ./...
GOMAXPROCS=4 go test -p 4 -count=1 ./...
GOMAXPROCS=4 go test -race -p 4 -count=1 ./internal/ui ./internal/render ./internal/shell ./internal/services
git diff --exit-code -- go.mod go.sum
```

Expected: every command exits zero. Keep the race run package-scoped to avoid the machine's repo-wide race-build limit.

**Step 6: Commit the code and test**

```bash
git add internal/shell/popout_network.go internal/shell/popout_network_test.go
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "fix(ui): square network list rows"
```

### Task 2: Deploy the exact build for visual confirmation

**Files:** None.

**Step 1: Build and identify the candidate**

```bash
GOMAXPROCS=4 go build -p 4 -o /tmp/sysc-shell-panel-list-row ./cmd/sysc-shell
sha256sum /tmp/sysc-shell-panel-list-row
```

Expected: the build succeeds and prints one SHA-256 digest.

**Step 2: Copy and restart on the laptop**

```bash
scp -P 7777 /tmp/sysc-shell-panel-list-row nomadx@192.168.0.64:/tmp/sysc-shell-panel-list-row
ssh -p 7777 nomadx@192.168.0.64 'cp ~/.local/bin/sysc-shell /tmp/sysc-shell.before-panel-list-row && mv /tmp/sysc-shell-panel-list-row ~/.local/bin/sysc-shell && systemctl --user restart sysc-shell.service && systemctl --user is-active sysc-shell.service'
```

Expected: `active`. Do not toggle laptop Wi-Fi because the SSH session depends on it.

**Step 3: Inspect the open network panel**

Confirm on the laptop that each access-point row has small rounded corners, the signal glyph sits inside the left edge, and lock/check glyphs sit inside the right edge. The header well, tabs, surrounding card, hover/press feedback, labels, and backdrop blur must match the previously accepted build.

If the shape fails the visual gate, restore `/tmp/sysc-shell.before-panel-list-row` and restart the service before changing the design.
