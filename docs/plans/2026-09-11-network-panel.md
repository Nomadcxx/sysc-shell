# Network Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `PanelNetwork` — a 460×560 bar-attached panel with Wi-Fi and
Ethernet tabs in the Direction B status-first composition — plus the `wifi` bar
widget that owns it, an event-driven NetworkManager service, and a
NetworkManager secret export so joining a secured network prompts in our panel.

**Architecture:** One new first-party panel composed from shipped `internal/ui`
primitives (`KindSegmented`, `KindToggle`, `KindScroll`, `KindIcon`,
`KindTextField`). The client is a pinned third-party binding
(`Wifx/gonetworkmanager/v2`) behind an unexported `backend` interface that
exists so tests use a fake rather than a live system bus. Only the credential
export is hand-written on `godbus`, because the binding has none. Reads arrive
by D-Bus signal, not polling; writes go off-owner through a `scheduleControl`
seam. One new `ui` node field (`Masked`), nine new Material glyphs, no new
node kinds.

**Tech Stack:** Go, the native retained renderer, `internal/ui` nodes,
`internal/theme` tokens, `internal/services`, `internal/ipc`,
`github.com/Wifx/gonetworkmanager/v2`, `github.com/godbus/dbus/v5`,
NetworkManager 1.58.1.

**Spec:** `docs/plans/2026-09-11-network-panel-design.md` (D1–D18). Read it
before Task 1; every task argues from it.

**Mock:** `https://claude.ai/code/artifact/c85bd466-d929-47bd-8c9f-ae9f41b56a49`
— owner-approved 2026-09-11: Direction B, status first (D7). Directions A and C
are retained there as the rejected alternatives. The mock is the reference for
anything the design leaves unstated.

**Tracker:** `sysc-157`, re-sliced per D16 in Task 0. Discovered work goes to
bd, not to this file.

## Global Constraints

- Go and the native retained renderer are mandatory. No C++, Rust, Lua, Qt,
  QML, Quickshell, CGO, compositor, lock screen, or runtime SVG loader.
- **Never `go build` or `go test ./...` with `-race`.** This box is zram-only
  swap with 16-way linking; a repo-wide race build hard-locks it. Cap `-p 4`
  and `GOMAXPROCS=4`, and test per package.
- `go test ./internal/shell` used to run `loginctl terminate-session self` for
  real and log the owner out. **Fixed 2026-09-11**: `runArgvDefault`
  (`popout_session.go:256`) refuses under `testing.Testing()`. Run the package
  normally; no `PATH` shadowing is needed. Later tasks still show
  `PATH=/tmp/shellstubs:$PATH` on their test commands — that prefix is now
  vestigial and harmless, and can be dropped.
- `sysc-shell` runs from systemd. Redeploy is `go build -o <tmp>
  ./cmd/sysc-shell`, `mv` over `~/.local/bin/sysc-shell`, `systemctl --user
  restart sysc-shell.service`. Never spawn it beside the running one. Never
  `pkill -f` your own binary name.
- Run `bd` only from `/home/nomadx/sysc-shell`, never from a worktree: the
  SQLite database is gitignored and exists only in the primary checkout.
- **Every commit in this plan needs
  `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db` in the environment.** This
  plan is executed in a worktree under `.worktrees/`, and the beads pre-commit
  hook runs `bd sync --flush-only`, which cannot find the database from there
  and fails the commit.
- Worktrees go in `.worktrees/`, which is gitignored (`.gitignore:1`), matching
  every recent sibling branch. `.claude/worktrees/` is **not** ignored here;
  do not put one there.
- **The commit-msg hook rejects `agent` as a bare substring.** This feature's
  central component is a secret agent, so the natural commit message for half
  these tasks is rejected. Say "credential export", "secret export" or "the
  prompt path" instead. The hook also rejects `b-o-t-h` and `l-l-m`; check every
  message against `/home/nomadx/.git-hooks/commit-msg` before committing. No
  `Co-Authored-By` trailer.
- The beads pre-commit hook auto-stages `.beads/issues.jsonl`, sweeping in
  unrelated pre-existing changes even under a pathspec-limited commit. Expect
  it; do not fight it.
- Panel `configure`, `render` and `handle` take `Registry.mu`. **No D-Bus call,
  command or filesystem I/O may run while `Registry.mu` or the Wayland owner is
  held.** All writes route through the `scheduleControl` seam.
- Keep one Wayland dispatch goroutine. Signal delivery gets its own.
- Add no new `ui` node kinds. `Masked` is a field on an existing kind.
- **The passphrase never reaches `errLabel`, the node tree, or a log line**
  (D5). `errLabel` is painted.
- Dependency resolution is offline: `GOPROXY=off`. The binding's `go.mod` names
  `google/uuid v1.3.0`, which is not cached (v1.6.0 is), and its `go 1.12`
  declaration means the full module graph loads. See Task 0 for the exact
  incantation.
- This checkout is shared. `git status` before you start; never `git checkout
  --`, `git stash` or `git add -A` over work you did not put there.

---

## Slice 1 — Service and bar widget (D15.1)

### Task 0: Reconcile, arm the stubs, fix the tracker, add the dependency

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `/tmp/shellstubs/loginctl`, `/tmp/shellstubs/systemctl`

**Interfaces:**
- Produces: a resolvable `github.com/Wifx/gonetworkmanager/v2` import for every
  later task.

- [ ] **Step 1: Confirm the tree is clean and current**

```bash
cd /home/nomadx/sysc-shell
git status --porcelain
git log --oneline -3
```

Expect `.beads/issues.jsonl` to be dirty or recently swept. Leave it alone.

- [ ] **Step 2: Confirm the session guard is in the tree**

```bash
grep -n "testing.Testing()" internal/shell/popout_session.go
```

Expected: a hit around line 270. `runArgvDefault` refuses to launch a session
action from a test binary, so `go test ./internal/shell` no longer terminates
the developer's login session. Stubs on `PATH` are no longer needed; if that
grep finds nothing, stop and ask rather than running the package.

- [ ] **Step 3: Fix the tracker (D16)**

Run these from `/home/nomadx/sysc-shell`, never from the worktree.

```bash
bd update sysc-157 --title "Network service, panel and wifi widget"
bd dep remove sysc-157 sysc-154
bd dep remove sysc-157 sysc-253
```

The subcommand is `dep remove`, taking the issue and the dependency as two
positional arguments. There is no `dep rm`.

`sysc-157` depended on the control-centre spine through both `sysc-154` and its
duplicate `sysc-253`. The command centre is a separate product (D17); neither
dependency is real. If `bd dep rm` rejects an edge that is not present, move on
— the duplicate pair is being resolved separately and is not this plan's job.

- [ ] **Step 4: Confirm the dependency is available, but do not pin it yet**

```bash
ls ~/go/pkg/mod/cache/download/github.com/\!wifx/gonetworkmanager/v2/@v/ | grep zip
ls ~/go/pkg/mod/cache/download/github.com/godbus/dbus/v5/@v/ | grep zip
```

Expected: `v2.2.0.zip` and a `v5.2.2.zip`. Both must be present, because
resolution runs with `GOPROXY=off`.

**The pin belongs to Task 3, not here.** `go mod edit -require` followed by
`go mod tidy` reverts silently at this point: tidy prunes any require that no
Go file imports, and nothing imports the binding until
`internal/services/networkbackend.go` exists. Pinning early looks like it
worked and leaves `go.mod` unchanged.

- [ ] **Step 5: Verify the baseline still builds**

```bash
GOMAXPROCS=4 go build -p 4 ./...
```

Expected: build succeeds. There is no dependency change to see yet.

- [ ] **Step 6: Nothing to commit**

Task 0 changes the tracker and the shell environment, neither of which is
version-controlled here. Skip the commit rather than inventing one.

---

### Task 1: Network state types and the backend seam

**Files:**
- Create: `internal/services/network.go`
- Create: `internal/services/network_test.go`

**Interfaces:**
- Produces: `services.NetworkState`, `services.AccessPoint`,
  `services.Connectivity` (`ConnUnknown`, `ConnNone`, `ConnWired`,
  `ConnWireless`), the unexported `backend` interface, and `SignalBand(uint8) int`.

- [ ] **Step 1: Write the failing test**

```go
package services

import "testing"

func TestSignalBandMatchesReferenceThresholds(t *testing.T) {
	cases := []struct {
		signal uint8
		band   int
	}{{100, 4}, {80, 4}, {79, 3}, {60, 3}, {59, 2}, {35, 2}, {34, 1}, {15, 1}, {14, 0}, {0, 0}}
	for _, c := range cases {
		if got := SignalBand(c.signal); got != c.band {
			t.Errorf("SignalBand(%d) = %d, want %d", c.signal, got, c.band)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/services -run TestSignalBand -v`
Expected: FAIL — `undefined: SignalBand`.

- [ ] **Step 3: Write the types and the band function**

```go
package services

// Connectivity names what the active connection runs over.
type Connectivity uint8

const (
	ConnUnknown Connectivity = iota
	ConnNone
	ConnWired
	ConnWireless
)

// NetworkState is the whole of what the bar widget and the panel header need.
type NetworkState struct {
	Kind            Connectivity
	Connected       bool
	Resolving       bool
	WirelessEnabled bool
	Scanning        bool
	SSID            string
	IPv4            string
	Interface       string
	Strength        uint8
}

// AccessPoint is one scanned network. Saved is ours, not NetworkManager's: the
// panel must know whether tapping a row connects or opens a password prompt
// before it calls anything.
type AccessPoint struct {
	Path       string
	DevicePath string
	SSID       string
	Strength   uint8
	Secured    bool
	Active     bool
	Saved      bool
}

// SignalBand buckets 0..100 into the five bands the glyphs draw. Sorting and
// glyph choice both use the band rather than the raw percent, which jitters on
// every scan and would reorder the list under the user's finger.
func SignalBand(signal uint8) int {
	switch {
	case signal >= 80:
		return 4
	case signal >= 60:
		return 3
	case signal >= 35:
		return 2
	case signal >= 15:
		return 1
	default:
		return 0
	}
}

// backend is the seam tests replace. Its only production implementation wraps
// the pinned gonetworkmanager binding. It exists so a test uses a fake instead
// of a live system bus; if that stops being true, delete it rather than
// populate it.
type backend interface {
	State() (NetworkState, error)
	AccessPoints() ([]AccessPoint, error)
	Scan() error
	SetWirelessEnabled(bool) error
	Activate(ap AccessPoint) error
	Forget(ssid string) error
	Watch(chan<- struct{}, <-chan struct{}) error
	Close() error
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/services -run TestSignalBand -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/network.go internal/services/network_test.go
git commit -m "feat(services): network state types and signal bands"
```

---

### Task 2: The service, its lease lifecycle, and sorted access points

**Files:**
- Modify: `internal/services/network.go`
- Modify: `internal/services/network_test.go`

**Interfaces:**
- Consumes: `backend`, `NetworkState`, `AccessPoint`, `SignalBand` (Task 1).
- Produces: `NewNetwork(b backend) *Network`, and on `*Network`:
  `Available() bool`, `State() NetworkState`, `CachedState() NetworkState`,
  `AccessPoints() []AccessPoint`, `Changes() <-chan NetworkState`,
  `Acquire() (*Lease, error)`, `Scan() error`, `SetWirelessEnabled(bool) error`,
  `Activate(AccessPoint) error`, `Forget(string) error`.

- [ ] **Step 1: Write the failing tests**

```go
type fakeBackend struct {
	state  NetworkState
	aps    []AccessPoint
	scans  int
	wakeCh chan<- struct{}
}

func (f *fakeBackend) State() (NetworkState, error)         { return f.state, nil }
func (f *fakeBackend) AccessPoints() ([]AccessPoint, error) { return f.aps, nil }
func (f *fakeBackend) Scan() error                          { f.scans++; return nil }
func (f *fakeBackend) SetWirelessEnabled(on bool) error     { f.state.WirelessEnabled = on; return nil }
func (f *fakeBackend) Activate(ap AccessPoint) error        { return nil }
func (f *fakeBackend) Forget(ssid string) error             { return nil }
func (f *fakeBackend) Close() error                         { return nil }
func (f *fakeBackend) Watch(wake chan<- struct{}, stop <-chan struct{}) error {
	f.wakeCh = wake
	return nil
}

func TestAccessPointsSortByBandNotPercent(t *testing.T) {
	f := &fakeBackend{aps: []AccessPoint{
		{SSID: "weak", Strength: 20},
		{SSID: "beta", Strength: 81},
		{SSID: "alpha", Strength: 95}, // same band as beta; SSID breaks the tie
	}}
	n := NewNetwork(f)
	got := n.AccessPoints()
	want := []string{"alpha", "beta", "weak"}
	for i, w := range want {
		if got[i].SSID != w {
			t.Fatalf("position %d = %q, want %q", i, got[i].SSID, w)
		}
	}
}

func TestChangesPublishesOnBackendWake(t *testing.T) {
	f := &fakeBackend{state: NetworkState{WirelessEnabled: true}}
	n := NewNetwork(f)
	lease, err := n.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	f.state.SSID = "LukeAP"
	f.wakeCh <- struct{}{}
	select {
	case st := <-n.Changes():
		if st.SSID != "LukeAP" {
			t.Fatalf("SSID = %q, want LukeAP", st.SSID)
		}
	case <-time.After(time.Second):
		t.Fatal("no change published")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/services -run "TestAccessPoints|TestChanges" -v`
Expected: FAIL — `undefined: NewNetwork`.

- [ ] **Step 3: Implement the service**

```go
// Network is a peer of Audio: same public shape, different delivery. Audio
// polls on a lease interval; this service is pushed, so the lease is lifecycle
// only and leaseSet's interval is deliberately ignored.
type Network struct {
	mu      sync.Mutex
	leases  leaseSet
	be      backend
	last    NetworkState
	aps     []AccessPoint
	ok      bool
	stop    chan struct{}
	wake    chan struct{}
	changes chan NetworkState
}

func NewNetwork(b backend) *Network {
	return &Network{be: b, ok: b != nil, changes: make(chan NetworkState, 1), wake: make(chan struct{}, 1)}
}

func (n *Network) Changes() <-chan NetworkState { return n.changes }

func (n *Network) Available() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ok
}

// CachedState never touches the bus. Callers holding Registry.mu or the
// Wayland owner must use it.
func (n *Network) CachedState() NetworkState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.last
}

func (n *Network) State() NetworkState {
	st, err := n.be.State()
	if err != nil {
		return n.CachedState()
	}
	n.mu.Lock()
	n.last = st
	n.mu.Unlock()
	return st
}

// AccessPoints returns the scan sorted by band then SSID.
func (n *Network) AccessPoints() []AccessPoint {
	aps, err := n.be.AccessPoints()
	if err != nil {
		n.mu.Lock()
		defer n.mu.Unlock()
		return slices.Clone(n.aps)
	}
	slices.SortFunc(aps, func(a, b AccessPoint) int {
		if d := SignalBand(b.Strength) - SignalBand(a.Strength); d != 0 {
			return d
		}
		return strings.Compare(a.SSID, b.SSID)
	})
	n.mu.Lock()
	n.aps = aps
	n.mu.Unlock()
	return aps
}

func (n *Network) Acquire() (*Lease, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	lease := &Lease{network: n}
	n.leases.add(lease)
	if n.stop == nil {
		n.stop = make(chan struct{})
		if err := n.be.Watch(n.wake, n.stop); err != nil {
			return nil, err
		}
		go n.run(n.stop)
	}
	return lease, nil
}

// run coalesces a burst of signals into a cap-1 channel, so a noisy scan
// cannot outrun one paint.
func (n *Network) run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-n.wake:
			st, err := n.be.State()
			if err != nil {
				continue
			}
			n.mu.Lock()
			n.last = st
			n.mu.Unlock()
			select {
			case n.changes <- st:
			default:
			}
		}
	}
}

func (n *Network) Scan() error                      { return n.be.Scan() }
func (n *Network) SetWirelessEnabled(on bool) error { return n.be.SetWirelessEnabled(on) }
func (n *Network) Activate(ap AccessPoint) error    { return n.be.Activate(ap) }
func (n *Network) Forget(ssid string) error         { return n.be.Forget(ssid) }
```

Add a `network *Network` field to `Lease` in `internal/services/leases.go`
beside the existing `audio` field, and release the subscription when the last
lease goes, mirroring `Audio.release`.

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/services -run "TestAccessPoints|TestChanges" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/network.go internal/services/network_test.go internal/services/leases.go
git commit -m "feat(services): pushed network service with lease lifecycle"
```

---

### Task 3: The NetworkManager backend

**Files:**
- Create: `internal/services/networkbackend.go`

**Interfaces:**
- Consumes: the `backend` interface (Task 1).
- Produces: `newNMBackend() (backend, error)`.

- [ ] **Step 1: Write the failing test**

```go
func TestNMBackendUnavailableWithoutBus(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/nonexistent")
	if _, err := newNMBackend(); err == nil {
		t.Fatal("expected an error when the system bus is unreachable")
	}
}
```

This is the only backend test. Everything else is covered through the fake:
a test that needs `NetworkManager` running fails on a build machine.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/services -run TestNMBackend -v`
Expected: FAIL — `undefined: newNMBackend`.

- [ ] **Step 3: Write the file first, then pin the binding**

Order matters. Write `internal/services/networkbackend.go` with its
`gonetworkmanager` import **before** running `tidy`, or `tidy` prunes the
require and `go.mod` comes back unchanged — the trap Task 0 Step 4 describes.

```bash
export GOFLAGS=-mod=mod GOPROXY=off GOSUMDB=off GOPRIVATE='*'
go mod edit -require=github.com/Wifx/gonetworkmanager/v2@v2.2.0 \
            -require=github.com/google/uuid@v1.6.0
GOMAXPROCS=4 go mod tidy
grep gonetworkmanager go.mod
```

The explicit `uuid` require is load-bearing: the binding is a `go 1.12` module,
so its full graph loads, and its own `go.mod` names `uuid v1.3.0`, which is not
in the cache (v1.6.0 is). Without the override `tidy` fails with a missing
`go.sum` entry. `tidy` then drops `uuid` from the final requires because no
compiled file imports it — expected, not a mistake.

```go
package services

import (
	"fmt"
	"strings"

	gnm "github.com/Wifx/gonetworkmanager/v2"
)

type nmBackend struct {
	nm       gnm.NetworkManager
	settings gnm.Settings
}

func newNMBackend() (backend, error) {
	nm, err := gnm.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("services: network manager: %w", err)
	}
	set, err := gnm.NewSettings()
	if err != nil {
		return nil, fmt.Errorf("services: network settings: %w", err)
	}
	return &nmBackend{nm: nm, settings: set}, nil
}
```

`State` reads `GetPropertyWirelessEnabled`, walks `GetDevices` for the first
`NmDeviceTypeWifi` and `NmDeviceTypeEthernet`, and fills `NetworkState` from
the active connection plus `IP4Config` for `IPv4`. `AccessPoints` calls
`GetAllAccessPoints` on the wireless device and cross-references
`settings.ListConnections` to set `Saved`. `Watch` calls `nm.Subscribe()` and
forwards every signal as an empty struct on the wake channel — the service
re-reads state rather than decoding the signal body, which keeps one decode
path instead of two.

`Activate` uses `ActivateConnection` when `ap.Saved`, and
`AddAndActivateConnection` otherwise; the second is what causes NetworkManager
to ask for a secret, which Task 10 answers.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/services -run TestNMBackend -v`
Expected: PASS.

- [ ] **Step 5: Verify against the live bus by hand**

```bash
GOMAXPROCS=4 go build -p 4 ./...
```

The service is not wired to the shell yet, so there is nothing to observe in
the UI. The spike in the design's "Verified during design" section is the
reference for what the live bus returns.

- [ ] **Step 6: Commit**

```bash
git add internal/services/networkbackend.go
git commit -m "feat(services): network backend over the pinned binding"
```

---

### Task 4: Nine glyphs, the `wifi` widget, and the registry wiring

**Files:**
- Modify: `internal/render/icons/material/build.py`
- Modify: `internal/render/icons/material/material-symbols-rounded.ttf`
- Modify: `internal/render/materialfont.go:23`
- Modify: `internal/render/materialfont_test.go:17`
- Create: `internal/shell/wifiwidget.go`
- Modify: `internal/shell/widget.go:39`, `internal/config/config.go:239`
- Modify: `internal/shell/registry.go:159`

**Interfaces:**
- Consumes: `services.NetworkState`, `services.SignalBand` (Tasks 1–2).
- Produces: `buildWifiWidget() textWidget`, `panelWifiAction`,
  `barView.Network`, the `"wifi"` config item.

- [ ] **Step 1: Re-cut the glyph subset**

The upstream pinned font is **not** vendored. It was found in `/tmp`, which is
volatile, and has been copied to
`~/.cache/sysc-shell/fonts/MaterialSymbolsRounded-upstream.ttf` — 15,090,976
bytes, SHA-256 `c4416e02739ed6865e3218c19dcd62c5a88fb97b8bcc445f24ae8017d11cc2d0`,
matching `SOURCE.md`. Pass that path to the script. The copy in
`~/.local/share/fonts` is a **different** cut and will fail the hash check.

Add to `ICONS` in `build.py`: `signal_wifi_0_bar`, `network_wifi_1_bar`,
`network_wifi_2_bar`, `network_wifi_3_bar`, `signal_wifi_4_bar`, `wifi_off`,
`lan`, `visibility`, `visibility_off`. Then run the script and add the same
nine names to `materialIcons` and to the test's inventory list. The two lists
are kept in step by hand and asserted by test; they change in the same commit.

`wifi_off` is the ninth: the bands cover signal strength, and none of them can
say the radio is off, which D10 requires to read differently from on-with-no-
association. All nine were verified present in the pinned upstream font before
the re-cut, with `fontTools` reading its glyph order.

- [ ] **Step 2: Run the font test to verify the subset carries them**

Run: `go test ./internal/render -run Material -v`
Expected: PASS, with the inventory assertion covering 34 names.

- [ ] **Step 3: Write the failing widget test**

```go
func TestWifiWidgetGlyphFollowsBand(t *testing.T) {
	icon := &ui.Node{Kind: ui.KindIcon}
	cases := []struct {
		state services.NetworkState
		want  string
	}{
		{services.NetworkState{WirelessEnabled: false}, "signal_wifi_0_bar"},
		{services.NetworkState{WirelessEnabled: true, Kind: services.ConnWired, Connected: true}, "lan"},
		{services.NetworkState{WirelessEnabled: true, Kind: services.ConnWireless, Connected: true, Strength: 92}, "signal_wifi_4_bar"},
		{services.NetworkState{WirelessEnabled: true, Kind: services.ConnWireless, Connected: true, Strength: 40}, "network_wifi_2_bar"},
	}
	for _, c := range cases {
		refreshWifiWidget(icon, barView{Network: c.state})
		if icon.Icon != c.want {
			t.Errorf("state %+v -> %q, want %q", c.state, icon.Icon, c.want)
		}
	}
}
```

- [ ] **Step 4: Run it to verify it fails**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestWifiWidget -v`
Expected: FAIL — `undefined: refreshWifiWidget`.

- [ ] **Step 5: Implement the widget**

Model it on `internal/shell/notifywidget.go` exactly: a `textWidget` wrapping a
single `KindIcon` node carrying `Action: panelWifiAction`, plus a `refresh`
closure that returns whether anything changed. The glyph logic ports
`noctalia/src/dbus/network/network_glyphs.cpp`: wired when the active
connection is wired, `signal_wifi_0_bar` when the radio is off, otherwise the
band glyph.

Register `"wifi"` in `knownItems`. **It cannot be called `network`** — that
name is already bound as a rate source with `rx`/`tx` directions
(`config.go:239`). Add `Network services.NetworkState` to `barView` beside
`Audio` (`widget.go:39`), and construct the service in `Registry` beside
`r.setAudio(...)` (`registry.go:159`), tolerating a backend error by leaving
the service unavailable rather than failing startup.

- [ ] **Step 6: Run it to verify it passes**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestWifiWidget -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/render internal/shell/wifiwidget.go internal/shell/widget.go \
        internal/config/config.go internal/shell/registry.go
git commit -m "feat(shell): wifi bar widget and nine glyphs"
```

---

## Slice 2 — The panel (D15.2)

### Task 5: Panel identity, geometry and the bar trigger

**Files:**
- Modify: `internal/shell/panel.go:9`, `internal/shell/panelhost.go`
- Modify: `internal/shell/registry.go:632`
- Create: `internal/shell/popout_network.go`

**Interfaces:**
- Consumes: `panelWifiAction` (Task 4).
- Produces: `PanelNetwork`, `networkTree(r *Registry, h *PanelHost) *ui.Node`,
  `h.networkTab`.

- [ ] **Step 1: Write the failing test**

```go
func TestPanelNetworkTargetSizeAndDefaultTab(t *testing.T) {
	if got := panelTargetSize(PanelNetwork); got.W != 460 || got.H != 560 {
		t.Fatalf("panelTargetSize = %dx%d, want 460x560", got.W, got.H)
	}
	h := &PanelHost{id: PanelNetwork}
	_ = networkTree(&Registry{}, h)
	if h.networkTab != "wifi" {
		t.Fatalf("default tab = %q, want wifi", h.networkTab)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestPanelNetwork -v`
Expected: FAIL — `undefined: PanelNetwork`.

- [ ] **Step 3: Implement identity and geometry**

Add `PanelNetwork` to the `PanelID` enum and its `String()` case
(`panel.go:9`), a `panelTargetSize` case returning `ui.Rect{W: 460, H: 560}`,
a `panelTree` case calling `networkTree`, and the IPC name `network`. Seed
`h.networkTab = "wifi"` on open beside the existing `PanelSettings` seeding.
Bind the bar action in `bindBarPanelActionsLocked` (`registry.go:632`) copying
the audio case exactly, including the anchor:

```go
case action == panelWifiAction && (button == 0 || button == buttonLeft):
	trig.AnchorX = bar.actionCenterX(panelWifiAction)
	return r.TogglePanel(PanelNetwork, out, trig) == nil
case action == panelWifiAction && button == buttonRight:
	r.toggleWirelessAsync()
	return true
```

- [ ] **Step 4: Run it to verify it passes**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestPanelNetwork -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/panel.go internal/shell/panelhost.go \
        internal/shell/registry.go internal/shell/popout_network.go
git commit -m "feat(shell): network panel identity and bar trigger"
```

---

### Task 6: The status-first header (Direction B)

**Files:**
- Modify: `internal/shell/popout_network.go`
- Create: `internal/shell/popout_network_test.go`

**Interfaces:**
- Consumes: `networkTree`, `h.networkTab` (Task 5), `services.NetworkState`.
- Produces: `networkHeaderCard(h *PanelHost, st services.NetworkState, m theme.Metrics) *ui.Node`.

- [ ] **Step 1: Write the failing test**

Only `nodeText` (`widget_test.go:237`) exists already. These three helpers do
not; write them at the top of `popout_network_test.go` before the first test.
Registry construction follows `audio_test.go:203` — a bare `&Registry{}` with
the one service injected, not the full `NewRegistry(config.Default())` used by
the integration tests.

```go
// collectText flattens every text node in a tree, for order-independent
// assertions about what a card renders.
func collectText(n *ui.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Kind == ui.KindText && n.Text != "" {
		out = append(out, n.Text)
	}
	for _, c := range n.Children {
		out = append(out, collectText(c)...)
	}
	return out
}

// findRowBySSID returns the access-point row whose label matches, failing the
// test if there is none.
func findRowBySSID(t *testing.T, n *ui.Node, ssid string) *ui.Node {
	t.Helper()
	if n == nil {
		t.Fatalf("no row for %q", ssid)
	}
	if n.Kind == ui.KindButton && slices.Contains(collectText(n), ssid) {
		return n
	}
	for _, c := range n.Children {
		if c == nil {
			continue
		}
		if got := findRowIfPresent(c, ssid); got != nil {
			return got
		}
	}
	t.Fatalf("no row for %q", ssid)
	return nil
}

func findRowIfPresent(n *ui.Node, ssid string) *ui.Node {
	if n == nil {
		return nil
	}
	if n.Kind == ui.KindButton && slices.Contains(collectText(n), ssid) {
		return n
	}
	for _, c := range n.Children {
		if got := findRowIfPresent(c, ssid); got != nil {
			return got
		}
	}
	return nil
}

func hasIcon(n *ui.Node, name string) bool {
	if n == nil {
		return false
	}
	if n.Kind == ui.KindIcon && n.Icon == name {
		return true
	}
	for _, c := range n.Children {
		if hasIcon(c, name) {
			return true
		}
	}
	return false
}

func TestHeaderShowsDashForAbsentFiguresNotZero(t *testing.T) {
	m, ok := theme.MetricsFor(theme.DensityStandard)
	if !ok {
		t.Fatal("no metrics for the standard density")
	}
	st := services.NetworkState{WirelessEnabled: false}
	card := networkHeaderCard(&PanelHost{networkTab: "wifi"}, st, m)
	texts := collectText(card)
	for _, want := range []string{"IPv4", "Down", "Up", "—"} {
		if !slices.Contains(texts, want) {
			t.Fatalf("header missing %q; got %v", want, texts)
		}
	}
	if slices.Contains(texts, "0") {
		t.Error("absent figures must render as a dash, never zero")
	}
}
```

`theme.MetricsFor` returns `(Metrics, bool)`; discarding the second value is a
compile error, not a style choice.

- [ ] **Step 2: Run it to verify it fails**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestHeaderShows -v`
Expected: FAIL — `undefined: networkHeaderCard`.

- [ ] **Step 3: Implement the header**

One `KindCapsule` card, `Fill: ui.FillContainerHigh`, `Shape: ui.ShapeCard`,
`Padding: m.CardPadding`, containing a `KindColumn` with `Gap: 12`:

1. A status row: a `KindCapsule` icon well at `m.StandardControl` square with
   `Shape: ui.ShapeMedium` and `Fill: ui.FillContainerHighest`; a title column
   carrying the SSID at `theme.RoleTitle` over the interface and signal at
   `theme.RoleCaption`; then a `KindToggle` for the radio and the close button,
   `PinEnd: true`.
2. A `KindSeparator`.
3. A three-column figure row — IPv4, Down, Up — each a caption label over a
   value with `Tabular: true`. **An absent value is a dash and keeps its
   space**; it is never zero.
4. The segmented tabs (Task 7).

The Ethernet tab swaps the glyph to `lan`, the SSID line to the connection
name, and drops the radio toggle.

- [ ] **Step 4: Run it to verify it passes**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestHeaderShows -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_network.go internal/shell/popout_network_test.go
git commit -m "feat(shell): status-first network header"
```

---

### Task 7: Tabs, the access-point list, and the Ethernet tab

**Files:**
- Modify: `internal/shell/popout_network.go`, `internal/shell/popout_network_test.go`

**Interfaces:**
- Consumes: `networkHeaderCard` (Task 6), `services.AccessPoint`.
- Produces: `networkWifiTree`, `networkEthernetTree`, the
  `network-tab:<name>` action prefix.

- [ ] **Step 1: Write the failing test**

```go
func TestConnectedRowCarriesCheckWithoutFilledHighlight(t *testing.T) {
	aps := []services.AccessPoint{{SSID: "LukeAP", Strength: 92, Secured: true, Saved: true, Active: true}}
	tree := networkWifiTree(aps, &PanelHost{networkTab: "wifi"})
	row := findRowBySSID(t, tree, "LukeAP")
	if !hasIcon(row, "check") {
		t.Error("the active row must carry a trailing check")
	}
	if row.Fill == ui.FillAccent {
		t.Error("the filled highlight belongs to the status block, not the row (D9)")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestConnectedRow -v`
Expected: FAIL — `undefined: networkWifiTree`.

- [ ] **Step 3: Implement the tabs and lists**

Tabs are `ui.KindSegmented` with two `Role: "tab"` segments, `Gap: 2`,
`Height: m.StandardControl`, copying `audioSegment` (`popout_audio.go:71`) with
`network-tab:wifi` and `network-tab:ethernet` actions.

Each access-point row is a `KindButton` in a `KindScroll` list: the band glyph,
the SSID, a `lock` glyph when `Secured`, and a trailing `check` when `Active`.
Per D9 the connected row keeps the check but **not** the filled highlight — the
status block owns that. Rows arrive already sorted from
`services.Network.AccessPoints()`; the panel does not re-sort.

The Ethernet tab is a single row: the `lan` glyph, the connection name, and the
state with interface and throughput as tabular captions.

Render the five states the design names: radio off, hardware absent, scanning
with no results, scan complete and empty, and populated.

- [ ] **Step 4: Run it to verify it passes**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run "TestConnectedRow|TestNetwork" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_network.go internal/shell/popout_network_test.go
git commit -m "feat(shell): network tabs, access point list and wired tab"
```

---

### Task 8: Writes — radio toggle, connect to saved, forget

**Files:**
- Modify: `internal/shell/popout_network.go`, `internal/shell/registry.go`

**Interfaces:**
- Consumes: `Network.SetWirelessEnabled`, `Network.Activate`, `Network.Forget`
  (Task 2), `scheduleControl` (`popout_audio.go:440`).
- Produces: `(h *PanelHost) applyNetworkControl(r *Registry, n *ui.Node) bool`,
  `r.toggleWirelessAsync()`.

- [ ] **Step 1: Write the failing test**

```go
func TestWriteNeverRunsUnderRegistryLock(t *testing.T) {
	r := &Registry{}
	h := &PanelHost{id: PanelNetwork, networkTab: "wifi"}
	r.mu.Lock() // held exactly as configure/render/handle hold it
	done := make(chan struct{})
	r.scheduleControl(h, func() error { close(done); return nil })
	r.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("control never ran off the lock")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestWriteNever -v`
Expected: FAIL — no `applyNetworkControl` path exists yet.

- [ ] **Step 3: Implement the writes**

Every write goes through `scheduleControl`: run off `Registry.mu` in a
goroutine, re-lock, set `h.errLabel` on failure, rebuild the panel, publish.
Tapping a row with `Saved` calls `Activate`; tapping one without it also calls
`Activate`, which reaches `AddAndActivateConnection` and — once Task 10 lands —
causes NetworkManager to ask us for the secret. Until then an unsaved secured
network fails with NetworkManager's own error in `errLabel`, which is the
correct intermediate behaviour, not a defect.

A polkit denial is an error in `errLabel`, never a hang.

- [ ] **Step 4: Run it to verify it passes**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestWriteNever -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_network.go internal/shell/registry.go
git commit -m "feat(shell): off-owner network writes"
```

---

## Slice 3 — The credential path (D15.3)

### Task 9: Masked text fields

**Files:**
- Modify: `internal/ui/textfield.go`, `internal/ui/tree.go`,
  `internal/render/paint.go:285`
- Create: `internal/ui/textfield_mask_test.go`

**Interfaces:**
- Produces: `ui.Field.Masked bool`, `ui.Node.Masked bool`.

- [ ] **Step 1: Write the failing test**

```go
func TestMaskedFieldKeepsItsRealValue(t *testing.T) {
	f := NewField("")
	f.Masked = true
	f.Insert("hunter2")
	if f.Text != "hunter2" {
		t.Fatalf("Text = %q; masking is a render-time substitution only", f.Text)
	}
	if n := f.Node("Password"); !n.Masked {
		t.Error("Node must carry Masked so the renderer can disguise it")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ui -run TestMaskedField -v`
Expected: FAIL — `f.Masked undefined`.

- [ ] **Step 3: Implement masking**

Add `Masked bool` to `Field` and to `Node`, propagate it in `Field.Node`, and
branch in `paint.go:285` to draw one bullet per rune instead of the rune.
`Field.Text` keeps the real value, so editing, cursor movement and submit are
unchanged and exactly one code path knows about the disguise. **Measure width
on the masked string**, or the caret drifts from the glyphs.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/ui -run TestMaskedField -v && go test ./internal/render -run Text -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui internal/render/paint.go
git commit -m "feat(ui): masked text fields"
```

---

### Task 10: The NetworkManager secret export

**Files:**
- Create: `internal/services/networksecrets.go`,
  `internal/services/networksecrets_test.go`

**Interfaces:**
- Produces: `SecretRequest{SSID, SettingName string}`,
  `(n *Network) SecretRequests() <-chan SecretRequest`,
  `(n *Network) SubmitSecret(psk string)`, `(n *Network) CancelSecret()`.

**Read first:** `noctalia/src/dbus/network/network_secret_agent.h` — the
single-slot contract this ports.

- [ ] **Step 1: Write the failing tests**

```go
func TestSecondRequestIsRejectedWhileOneIsPending(t *testing.T) {
	s := newSecretSlot()
	first := make(chan reply, 1)
	if !s.begin(SecretRequest{SSID: "Orac 15A"}, first) {
		t.Fatal("first request must be accepted")
	}
	second := make(chan reply, 1)
	if s.begin(SecretRequest{SSID: "LukeAP"}, second) {
		t.Fatal("a second request must be refused while one is pending")
	}
	if got := <-second; !got.noSecrets {
		t.Error("the refusal must answer NoSecrets so NM falls back to its own store")
	}
}

func TestCancelAlwaysReplies(t *testing.T) {
	s := newSecretSlot()
	ch := make(chan reply, 1)
	s.begin(SecretRequest{SSID: "Orac 15A"}, ch)
	s.cancel()
	select {
	case got := <-ch:
		if !got.cancelled {
			t.Error("cancel must reply UserCanceled")
		}
	case <-time.After(time.Second):
		t.Fatal("a dangling reply hangs NetworkManager's activation until it times out")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/services -run "TestSecondRequest|TestCancelAlways" -v`
Expected: FAIL — `undefined: newSecretSlot`.

- [ ] **Step 3: Implement the slot and the export**

`newSecretSlot` is pure state with no D-Bus in it, which is why it is testable
without a bus. It holds at most one pending request and its reply channel;
`begin` refuses when occupied, answering `noSecrets`; `submit` and `cancel`
each reply exactly once and clear the slot.

The export wraps it: publish an object at
`/org/freedesktop/NetworkManager/SecretAgent` implementing `GetSecrets`,
`CancelGetSecrets`, `SaveSecrets` and `DeleteSecrets` on `godbus`, then call
`AgentManager.Register("one.archpcx.sysc-shell")`. `GetSecrets` answers PSK
requests only; an 802.1X request is answered `NoSecrets`.

**The passphrase never enters `errLabel`, the node tree, or a log line.** Clear
it on submit, on cancel and on panel close — not merely hide it.

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/services -run "TestSecondRequest|TestCancelAlways" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Mind the hook — `agent` is a rejected substring:

```bash
git add internal/services/networksecrets.go internal/services/networksecrets_test.go
git commit -m "feat(services): single-slot credential export for NetworkManager"
```

---

### Task 11: The password card

**Files:**
- Modify: `internal/shell/popout_network.go`, `internal/shell/panelhost.go`,
  `internal/shell/popout_network_test.go`

**Interfaces:**
- Consumes: `ui.Field.Masked` (Task 9), `Network.SecretRequests`,
  `SubmitSecret`, `CancelSecret` (Task 10).
- Produces: `networkPasswordCard(h *PanelHost) *ui.Node`.

- [ ] **Step 1: Write the failing test**

```go
func TestPasswordCardMasksAndNeverLeaksToTheErrorLabel(t *testing.T) {
	h := &PanelHost{id: PanelNetwork, pendingSSID: "Orac 15A"}
	h.password = ui.NewField("hunter2")
	h.password.Masked = true
	card := networkPasswordCard(h)
	if !slices.Contains(collectText(card), "Join Orac 15A") {
		t.Error("the prompt must name the network being joined")
	}
	h.errLabel = "activation failed"
	for _, s := range collectText(card) {
		if strings.Contains(s, "hunter2") {
			t.Fatal("the passphrase reached the painted tree")
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestPasswordCard -v`
Expected: FAIL — `undefined: networkPasswordCard`.

- [ ] **Step 3: Implement the card**

The card replaces the access-point list while a secret is pending: a title
naming the SSID, a masked `KindTextField`, a reveal button toggling
`Masked` and swapping `visibility`/`visibility_off`, then Cancel and Connect.
Closing the panel or cancelling calls `CancelSecret`, which must reply.

- [ ] **Step 4: Run it to verify it passes**

Run: `PATH=/tmp/shellstubs:$PATH go test ./internal/shell -run TestPasswordCard -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shell/popout_network.go internal/shell/panelhost.go \
        internal/shell/popout_network_test.go
git commit -m "feat(shell): password prompt card"
```

---

### Task 12: The live gate

**Files:** none changed unless a defect is found.

This is the only task that touches the running machine, and the only one that
can settle D18's open risk.

- [ ] **Step 1: Settle the `nm-applet` collision first**

`nm-applet` is running and probably holds a registered secret export. Which one
NetworkManager asks is not answerable by inspection — D-Bus refuses
introspection of its object path. Before trusting the password card:

```bash
systemctl --user status nm-applet 2>/dev/null || pgrep -a nm-applet
```

Register ours, tap an unsaved secured network, and watch which prompt appears.
If nm-applet wins, the options are to run without it, or to register with
`RegisterWithCapabilities`. Record the outcome in bd either way.

- [ ] **Step 2: Unblock the radio**

```bash
rfkill unblock wifi
nmcli -t -f DEVICE,TYPE,STATE device | grep wlan0
```

Expected: `wlan0:wifi:disconnected` rather than `unavailable`. Everything Wi-Fi
is untestable until this is done — the adapter is an RTL8821CE at
`/org/freedesktop/NetworkManager/Devices/3` and ships soft-blocked here.

- [ ] **Step 3: Redeploy**

```bash
GOMAXPROCS=4 go build -p 4 -o /tmp/sysc-shell ./cmd/sysc-shell
mv /tmp/sysc-shell ~/.local/bin/sysc-shell
systemctl --user restart sysc-shell.service
systemctl --user status sysc-shell.service --no-pager | head -5
```

Never spawn it beside the running one. Never `pkill -f sysc-shell`.

- [ ] **Step 4: Walk the matrix**

Left-click the widget opens the panel anchored under the glyph. Right-click
toggles the radio. Both tabs render. The list sorts by band and does not
reorder while scanning. Connecting to `LukeAP` or `Orac 15A` needs no
passphrase. An unsaved secured network raises the password card, and the
passphrase is accepted. Cancelling leaves NetworkManager idle rather than
hanging. Radio off renders the empty state.

A handler panic presents as a blank shell — `sysc-wayland` recovers it into an
error and the service still reads active with nothing painted. If the panel
comes up empty, read the service log before assuming a layout bug.

- [ ] **Step 5: Record and close**

Put observations and any defects in bd against `sysc-157`, not in this file.
