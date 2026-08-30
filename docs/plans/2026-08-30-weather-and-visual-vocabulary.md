# Weather and Visual Vocabulary (Milestone 3, Tranche 3D) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a weather widget on every configured output from Open-Meteo, eight project-owned icons as
a font face, an error tone for failure text, and a hover tooltip on its own surface.

**Architecture:** A third concrete service, `services.Weather`, reuses the `leaseSet` Tranche 3B
extracts and fetches on a bounded, backing-off schedule that never discards its last good reading.
Icons resolve through the existing per-rune font fallback, so an icon is shaped, masked and tinted by
the same path as text. Tasks 9–11 establish the shared auxiliary-surface seam: Wayland owns surfaces and
buffers, shell callbacks own content, and one generation-safe controller owns tooltip dwell.

**Tech Stack:** Go 1.26, standard library only. `net/http` and `encoding/json` cover the fetch. No new
module dependency.

**Spec:** `docs/plans/2026-08-30-weather-and-visual-vocabulary-design.md`

## Global Constraints

Every task's requirements implicitly include this section.

- Linux and Niri are the only platform contract.
- **One goroutine owns the Wayland connection and every proxy.** The dwell timer sends a generation-tagged
  message and never calls a proxy.
- No new module dependency, no `replace` directive. `net/http` and `encoding/json` are standard library.
- No runtime SVG decoder, CGO library, or external conversion process.
- No geocoding, no automatic location, and no second remote host. Open-Meteo is the only one.
- No interface over `Clock`, `Metrics` and `Weather`. They share `leaseSet`, a struct.
- No dismiss shield and no keyboard focus for the tooltip. Those belong to Tranche 4A's panels.
- No forecast panel or popout. Those belong to Milestone 4.
- Widget instances are keyed by `wl_registry` global name (`uint32`). The connector is an attribute.
- All new goroutines must stop under cancellation, and `go test -race` must report no data race.
- Fetch tests run against an `httptest` server. No test contacts the live API.
- Test fixtures use connectors `DP-9` and `HDMI-A-9`, and coordinates `0.0, 0.0`. No real coordinates,
  place names or machine values enter Git.
- Commit messages must not contain any of `claude`, `anthropic`, `chatgpt`, `openai`, `copilot`,
  `cursor`, `cody`, `tabnine`, `codex`, `gemini`, `bard`, `llm`, `bot`, `agent` as a case-insensitive
  substring; a repository hook rejects them. Note that this rejects innocent words containing them, such
  as "both" and "robot".
- Commits from a worktree need `BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit …`, or the
  bd pre-commit hook aborts and blocks every commit on the branch.

## File Structure

**Created**

| Path | Responsibility |
|---|---|
| `internal/services/weather.go` | The `Weather` service: leases, fetch schedule, backoff, `Reading`. |
| `internal/services/weather_test.go` | Lifetime, timeout, staleness, backoff, fetch floor. |
| `internal/render/icons/` | Authored SVG sources, the generated font, and both licences. |
| `internal/render/iconfont.go` | Embed the font; resolve the private-use range. |
| `internal/render/iconfont_test.go` | Icon runes resolve to the project face, not a system one. |
| `internal/shell/weatherwidget.go` | Weather widget construction and formatting. |
| `internal/shell/weatherwidget_test.go` | Formatting, staleness, error tone. |
| `internal/platform/wayland/aux.go` | Basic auxiliary-surface open, replace, and close path. |
| `internal/platform/wayland/aux_test.go` | Fake-compositor creation, callbacks, empty input region, and teardown. |
| `internal/shell/tooltip.go` | Dwell timing and tooltip content. |
| `internal/shell/tooltip_test.go` | Generation-safe dwell, four-edge placement, updates, and cleanup. |

**Modified**

| Path | Change |
|---|---|
| `internal/render/fontmap.go` | Resolve the icon face ahead of the system query. |
| `internal/render/paint.go` | `ProofStyle.Error`; tone selects the text colour. |
| `internal/ui/tree.go` | `Node.Tone`, `Tone` type. |
| `internal/config/config.go`, `load.go` | The `weather` block, the `weather` item, the cross-section check. |
| `internal/shell/theme.go` | Carry `Error` into `ProofStyle`. |
| `internal/shell/widget.go`, `registry.go` | Weather widget, its lease, `UpdateWeather`. |
| `internal/platform/wayland/client.go` | Tooltip hooks on `Callbacks`; owner-side surface handling. |
| `cmd/sysc-shell/main.go` | Weather pump. |
| `tests/integration/README.md` | Tranche 3D live matrix. |

## Lanes

Tasks 1–3 (service), Task 4 (icons) and Task 5 (tone) touch disjoint packages and may run in parallel.
Tasks 6–8 are the integration lane and are serial. Tasks 9–11 are the tooltip and are **cuttable as a
unit**: cutting them leaves weather, icons and tone shipped and the tranche coherent.

---

### Task 1: The weather service lifetime

**Files:**
- Create: `internal/services/weather.go`, `internal/services/weather_test.go`

**Interfaces:**
- Consumes: `leaseSet` and `Lease` from Tranche 3B Task 1. If 3B has not landed, execute that task here
  first.
- Produces: `Unit` with `UnitCelsius` and `UnitFahrenheit`; `Reading`;
  `NewWeather(latitude, longitude float64, unit Unit) *Weather`;
  `(*Weather).Acquire(time.Duration) (*Lease, error)`; `(*Weather).Updates() <-chan Reading`;
  `(*Weather).Close()`; `(*Weather).Running() bool`; `(*Weather).Starts() int`. Tasks 2, 3 and 7 use
  these.

- [ ] **Step 1: Write the failing test**

Create `internal/services/weather_test.go`:

```go
package services

import (
	"testing"
	"time"
)

func TestTheFirstWeatherLeaseStartsAndTheLastStops(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	if w.Running() {
		t.Fatal("a service with no lease is running")
	}

	first, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !w.Running() {
		t.Fatal("the first lease did not start the service")
	}

	second, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := w.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; a second consumer must share the goroutine", got)
	}

	first.Release()
	if !w.Running() {
		t.Fatal("releasing one of two leases stopped the service")
	}
	second.Release()
	if w.Running() {
		t.Fatal("releasing the last lease left the service running")
	}
}

func TestANonPositiveWeatherIntervalIsRejected(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	if _, err := w.Acquire(0); err == nil {
		t.Fatal("a zero interval was accepted")
	}
	if w.Running() {
		t.Fatal("a rejected acquire started the service")
	}
}

// A reload acquires the replacement lease before releasing the outgoing one,
// so a service in continuous use must never restart.
func TestAcquireBeforeReleaseDoesNotRestartTheWeatherService(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	t.Cleanup(w.Close)

	outgoing, err := w.Acquire(15 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	incoming, err := w.Acquire(10 * time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	outgoing.Release()

	if got := w.Starts(); got != 1 {
		t.Fatalf("starts = %d, want 1; the service restarted across a reload", got)
	}
	incoming.Release()
}

func TestClosingTheWeatherServiceStopsTheGoroutine(t *testing.T) {
	t.Parallel()
	w := NewWeather(0, 0, UnitCelsius)
	if _, err := w.Acquire(time.Minute); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	w.Close()

	if w.Running() {
		t.Fatal("Close left the service running")
	}
	// Close must be safe to call twice; shutdown paths may each reach it.
	w.Close()
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run Weather -v`
Expected: FAIL to compile — `NewWeather`, `UnitCelsius` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/services/weather.go`:

```go
package services

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Unit is the temperature unit the API is asked for. The shell does not
// convert; it requests the unit the user configured.
type Unit uint8

const (
	UnitCelsius Unit = iota
	UnitFahrenheit
)

// Reading is the newest observation and the state of the fetch that produced
// it.
//
// Observed is false until the first success. A failure after that leaves the
// observation intact and only advances FailedSince, which is what makes a
// stale reading distinguishable from no reading at all: a reading with an age
// is still information, an empty widget is not.
type Reading struct {
	Observed bool
	// Temperature is in Unit, not always Celsius: the request asks the API for
	// the configured unit rather than converting here, so the reading has to
	// carry which one it is.
	Temperature float64
	Unit        Unit
	Code        int // WMO weather code
	FetchedAt   time.Time
	FailedSince time.Time // zero while healthy
}

// Stale reports whether the reading is an observation whose fetch has since
// begun failing.
func (r Reading) Stale() bool { return r.Observed && !r.FailedSince.IsZero() }

const (
	// connectAndReadBudget bounds one whole fetch. A weather request must
	// never outlive it: the shell has to keep painting.
	connectAndReadBudget = 6 * time.Second
	// minFetchInterval throttles fetches regardless of what asks, so a
	// pathological reload loop cannot hammer the API.
	minFetchInterval = 30 * time.Second
	// retryDelay is the wait after each of the first attempts.
	retryDelay = 30 * time.Second
	// maxRetryAttempts is how many quick retries precede the backoff.
	maxRetryAttempts = 3
	// backoffBase and backoffCap bound the persistent retry schedule.
	backoffBase = 60 * time.Second
	backoffCap  = 300 * time.Second
	// maxResponseBytes caps the body. The current-weather response is a few
	// hundred bytes; anything near this is not the API answering.
	maxResponseBytes = 64 << 10
)

// Weather fetches the current observation on one goroutine.
type Weather struct {
	mu     sync.Mutex
	leases leaseSet
	stop   chan struct{}
	done   chan struct{}
	starts int

	rearm   chan struct{}
	updates chan Reading

	latitude, longitude float64
	unit                Unit

	client *http.Client
	// endpoint is overridden by tests to point at an httptest server.
	endpoint string
}

// openMeteoEndpoint is the only remote host this shell contacts.
const openMeteoEndpoint = "https://api.open-meteo.com/v1/forecast"

func NewWeather(latitude, longitude float64, unit Unit) *Weather {
	return &Weather{
		rearm:     make(chan struct{}, 1),
		updates:   make(chan Reading, 1),
		latitude:  latitude,
		longitude: longitude,
		unit:      unit,
		client:    &http.Client{Timeout: connectAndReadBudget},
		endpoint:  openMeteoEndpoint,
	}
}

// Updates carries the newest reading. The channel is created once and never
// closed, so it survives stop and start cycles.
func (w *Weather) Updates() <-chan Reading { return w.updates }

// Acquire registers a consumer wanting a reading at least every interval.
func (w *Weather) Acquire(interval time.Duration) (*Lease, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("services: weather interval %v is not positive", interval)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	lease := &Lease{weather: w, boundary: interval}
	previous, current := w.leases.add(lease)

	switch {
	case w.stop == nil:
		w.startLocked()
	case previous != 0 && current < previous:
		select {
		case w.rearm <- struct{}{}:
		default:
		}
	}
	return lease, nil
}

// Close releases every lease and stops the goroutine. It is safe to call twice.
func (w *Weather) Close() {
	w.mu.Lock()
	for _, l := range w.leases.clear() {
		l.weather = nil
	}
	done := w.stopIfUnusedLocked()
	w.mu.Unlock()

	if done != nil {
		<-done
	}
}

func (w *Weather) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stop != nil
}

func (w *Weather) Starts() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.starts
}

func (w *Weather) startLocked() {
	w.stop, w.done = make(chan struct{}), make(chan struct{})
	w.starts++
	go w.run(w.stop, w.done)
}

func (w *Weather) stopIfUnusedLocked() chan struct{} {
	if w.leases.len() > 0 || w.stop == nil {
		return nil
	}
	close(w.stop)
	done := w.done
	w.stop, w.done = nil, nil
	return done
}

// releaseWeather drops one lease, stopping the goroutine when it was the last.
func (w *Weather) releaseWeather(l *Lease) {
	w.mu.Lock()
	if !w.leases.remove(l) {
		w.mu.Unlock()
		return
	}
	done := w.stopIfUnusedLocked()
	w.mu.Unlock()

	if done != nil {
		<-done
	}
}
```

Extend `Lease` in `internal/services/clock.go` with the third owner, and add the dispatch arm:

```go
type Lease struct {
	clock    *Clock
	metrics  *Metrics
	weather  *Weather
	source   Source
	boundary time.Duration
}
```

```go
	case l.weather != nil:
		w := l.weather
		l.weather = nil
		w.releaseWeather(l)
```

Add a temporary `run` stub at the bottom of `weather.go`, deleted in Task 2:

```go
func (w *Weather) run(stop, done chan struct{}) { <-stop; close(done) }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS, including every pre-existing clock and metrics test.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services && go vet ./internal/services/
git add internal/services/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(services): add the leased weather service lifetime"
```

---

### Task 2: Fetching, with bounds

Replaces Task 1's `run` stub. Every bound here exists because an unbounded weather fetch is a hang in a
shell that must keep painting.

**Files:**
- Modify: `internal/services/weather.go`, `internal/services/weather_test.go`

**Interfaces:**
- Consumes: `Weather`, `Reading` from Task 1.
- Produces: a populated `Reading` on `Updates()`. Task 7 consumes it.

- [ ] **Step 1: Write the failing test**

Append to `internal/services/weather_test.go`:

```go
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// currentWeatherBody is the shape Open-Meteo returns for the requested fields.
const currentWeatherBody = `{"current":{"temperature_2m":18.4,"weather_code":3}}`

// weatherAt points a service at a test server and leases it at a short
// interval so one fetch happens promptly.
func weatherAt(t *testing.T, handler http.Handler) (*Weather, *Lease) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = server.URL
	t.Cleanup(w.Close)

	lease, err := w.Acquire(50 * time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(lease.Release)
	return w, lease
}

func TestASuccessfulFetchPublishesAnObservation(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, currentWeatherBody)
	}))

	select {
	case reading := <-w.Updates():
		if !reading.Observed {
			t.Fatal("a successful fetch published an unobserved reading")
		}
		if reading.Temperature != 18.4 || reading.Code != 3 {
			t.Fatalf("reading = %+v, want 18.4 and code 3", reading)
		}
		if !reading.FailedSince.IsZero() {
			t.Fatal("a successful fetch reported a failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reading arrived within three seconds")
	}
}

// The request must carry the configured coordinates and unit, and ask for
// only the fields the bar renders.
func TestTheRequestAsksForOnlyWhatTheBarRenders(t *testing.T) {
	t.Parallel()
	queries := make(chan string, 1)
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case queries <- r.URL.RawQuery:
		default:
		}
		fmt.Fprint(rw, currentWeatherBody)
	}))
	<-w.Updates()

	query := <-queries
	for _, want := range []string{"latitude=", "longitude=", "current=temperature_2m,weather_code"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query %q is missing %q", query, want)
		}
	}
	// Fields with no bar consumer cost bytes and parsing on every fetch.
	for _, unwanted := range []string{"daily=", "forecast_days", "relative_humidity", "wind_speed"} {
		if strings.Contains(query, unwanted) {
			t.Fatalf("query %q requests %q, which no widget renders", query, unwanted)
		}
	}
}

// A failure after a success must keep the observation and only mark it stale.
func TestAFailurePreservesTheLastGoodReading(t *testing.T) {
	t.Parallel()
	var calls int
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(rw, currentWeatherBody)
			return
		}
		http.Error(rw, "unavailable", http.StatusServiceUnavailable)
	}))

	first := <-w.Updates()
	if !first.Observed {
		t.Fatal("the first fetch did not observe")
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case reading := <-w.Updates():
			if !reading.Stale() {
				continue
			}
			if reading.Temperature != 18.4 {
				t.Fatalf("a stale reading lost its observation: %+v", reading)
			}
			return
		case <-deadline:
			t.Fatal("no stale reading arrived after the server began failing")
		}
	}
}

// A server that never responds must not outlive the budget.
func TestAStalledServerFailsWithinTheBudget(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		<-release
	}))

	select {
	case reading := <-w.Updates():
		if reading.Observed {
			t.Fatal("a stalled server produced an observation")
		}
		if reading.FailedSince.IsZero() {
			t.Fatal("a stalled fetch did not report a failure")
		}
	case <-time.After(connectAndReadBudget + 4*time.Second):
		t.Fatal("a stalled fetch outlived its budget")
	}
}

// An oversized body must be rejected rather than buffered.
func TestAnOversizedResponseIsRejected(t *testing.T) {
	t.Parallel()
	w, _ := weatherAt(t, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`{"current":{"temperature_2m":1,"weather_code":0,"pad":"`))
		chunk := strings.Repeat("x", 4096)
		for written := 0; written < maxResponseBytes+8192; written += len(chunk) {
			rw.Write([]byte(chunk))
		}
	}))

	select {
	case reading := <-w.Updates():
		if reading.Observed {
			t.Fatal("an oversized response produced an observation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reading arrived for an oversized response")
	}
}

func TestBackoffIsBounded(t *testing.T) {
	t.Parallel()
	for attempt, want := range map[int]time.Duration{
		0: retryDelay,
		1: retryDelay,
		2: retryDelay,
		3: backoffBase,
		4: 2 * backoffBase,
		9: backoffCap,
	} {
		if got := retryAfter(attempt); got != want {
			t.Fatalf("retryAfter(%d) = %v, want %v", attempt, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/ -run 'Fetch|Request|Stalled|Oversized|Backoff|Preserves' -v`
Expected: FAIL — the stub `run` never fetches, so the publishing tests time out, and `retryAfter` is
undefined.

- [ ] **Step 3: Write the implementation**

In `internal/services/weather.go`, delete the `run` stub and add:

```go
// retryAfter is the wait before the next attempt. The first few failures
// retry quickly; beyond that the delay doubles to a cap, so a permanently
// unreachable API costs one request every five minutes rather than one every
// thirty seconds forever.
func retryAfter(consecutiveFailures int) time.Duration {
	if consecutiveFailures < maxRetryAttempts {
		return retryDelay
	}
	delay := backoffBase << (consecutiveFailures - maxRetryAttempts)
	if delay > backoffCap || delay <= 0 {
		return backoffCap
	}
	return delay
}

func (w *Weather) run(stop, done chan struct{}) {
	defer close(done)

	var (
		reading  Reading
		failures int
		lastAt   time.Time
		failing  bool
	)

	for {
		w.mu.Lock()
		interval := w.leases.finest()
		w.mu.Unlock()
		if interval <= 0 {
			return
		}

		wait := interval
		if failures > 0 {
			wait = retryAfter(failures)
		}
		// The floor applies regardless of what asks, so a short lease interval
		// or a reload loop cannot hammer the API.
		if since := time.Since(lastAt); !lastAt.IsZero() && since < minFetchInterval {
			if remaining := minFetchInterval - since; remaining > wait {
				wait = remaining
			}
		}

		timer := time.NewTimer(wait)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-w.rearm:
			timer.Stop()
			continue
		case <-timer.C:
		}

		lastAt = time.Now()
		observation, err := w.fetch()
		if err != nil {
			failures++
			if !failing {
				failing = true
				fmt.Fprintf(os.Stderr, "sysc-shell: weather unavailable: %v\n", err)
			}
			if reading.FailedSince.IsZero() {
				reading.FailedSince = time.Now()
			}
		} else {
			if failing {
				failing = false
				fmt.Fprintln(os.Stderr, "sysc-shell: weather recovered")
			}
			failures = 0
			observation.FailedSince = time.Time{}
			reading = observation
		}
		sendReading(w.updates, reading)
	}
}

// wireCurrent is the subset of the Open-Meteo response the bar renders.
type wireCurrent struct {
	Current struct {
		Temperature *float64 `json:"temperature_2m"`
		Code        *int     `json:"weather_code"`
	} `json:"current"`
}

// fetch performs one request. The client carries an overall timeout and the
// request carries a deadline, so neither a stalled connect nor a slow body can
// outlive the budget.
func (w *Weather) fetch() (Reading, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectAndReadBudget)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.requestURL(), nil)
	if err != nil {
		return Reading{}, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return Reading{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Reading{}, fmt.Errorf("weather: status %s", resp.Status)
	}

	// One byte past the cap distinguishes "exactly at the cap" from "longer".
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Reading{}, err
	}
	if len(body) > maxResponseBytes {
		return Reading{}, fmt.Errorf("weather: response exceeds %d bytes", maxResponseBytes)
	}

	var wire wireCurrent
	if err := json.Unmarshal(body, &wire); err != nil {
		return Reading{}, fmt.Errorf("weather: decode: %w", err)
	}
	if wire.Current.Temperature == nil || wire.Current.Code == nil {
		return Reading{}, fmt.Errorf("weather: response carries no current observation")
	}

	return Reading{
		Observed:    true,
		Temperature: *wire.Current.Temperature,
		Unit:        w.unit,
		Code:        *wire.Current.Code,
		FetchedAt:   time.Now(),
	}, nil
}

// requestURL asks for only the two fields the bar renders. The reference
// shell additionally requests humidity, pressure, wind, sunrise and a
// seven-day block; none has a consumer on a 44-pixel bar, and every unused
// field is bytes and parsing on every fetch.
func (w *Weather) requestURL() string {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(w.latitude, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(w.longitude, 'f', -1, 64))
	q.Set("current", "temperature_2m,weather_code")
	q.Set("timezone", "auto")
	if w.unit == UnitFahrenheit {
		q.Set("temperature_unit", "fahrenheit")
	}
	return w.endpoint + "?" + q.Encode()
}

// sendReading publishes the newest reading, replacing one the consumer has not
// read. This goroutine is the only sender, so the retry always finds room.
func sendReading(updates chan Reading, reading Reading) {
	select {
	case updates <- reading:
		return
	default:
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- reading:
	default:
	}
}
```

Add `"context"`, `"encoding/json"`, `"io"`, `"net/url"`, `"os"` and `"strconv"` to the file's imports.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -race ./internal/services/ -v`
Expected: PASS. No test contacts the live API.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/services && go vet ./internal/services/
git add internal/services/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(services): fetch current weather within a bounded budget"
```

---

### Task 3: The minimum fetch floor

Task 2 implements the floor; this task proves it, because it is the one bound whose absence is invisible
until the API rate-limits the user.

**Files:**
- Modify: `internal/services/weather_test.go`

**Interfaces:**
- Consumes: `Weather` from Tasks 1–2.
- Produces: nothing.

- [ ] **Step 1: Write the test**

Append to `internal/services/weather_test.go`:

```go
// However short the lease interval, fetches cannot exceed the floor. Without
// this a one-second widget interval would issue sixty requests a minute.
func TestTheMinimumFetchFloorHolds(t *testing.T) {
	t.Parallel()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(rw, currentWeatherBody)
	}))
	t.Cleanup(server.Close)

	w := NewWeather(0, 0, UnitCelsius)
	w.endpoint = server.URL
	t.Cleanup(w.Close)

	lease, err := w.Acquire(time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()

	// Wait well past several lease intervals but far short of the floor.
	time.Sleep(2 * time.Second)

	if got := atomic.LoadInt32(&calls); got > 1 {
		t.Fatalf("the server was called %d times inside the fetch floor, want at most 1", got)
	}
}
```

Add `"sync/atomic"` to the file's imports.

- [ ] **Step 2: Run the test**

Run: `go test -race ./internal/services/ -run FetchFloor -v`
Expected: PASS. If it fails with more than one call, the floor in Task 2's `run` is not applied before
the first retry — fix it there rather than relaxing this test.

- [ ] **Step 3: Commit**

```bash
git add internal/services/weather_test.go
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "test(services): prove the weather fetch floor holds"
```

---

### Task 4: The icon font

**Files:**
- Create: `internal/render/icons/` with the SVG sources, the built font and both licences;
  `internal/render/iconfont.go`, `internal/render/iconfont_test.go`
- Modify: `internal/render/fontmap.go`

**Interfaces:**
- Consumes: `render.ParseFace`, `FontMap.Face`.
- Produces: `render.IconRune(code int) rune`; the private-use range constants. Task 6 emits these runes.

- [ ] **Step 1: Author and build the font**

Author eight symbols as SVGs under `internal/render/icons/svg/`, named for the conditions the whole WMO
code set reduces to: `clear-day`, `partly-cloudy`, `cloud`, `fog`, `rain`, `snow`, `heavy-snow`,
`thunderstorm`.

Build them into one font at `internal/render/icons/sysc-icons.ttf`, mapping them to consecutive
private-use codepoints from U+E000. Any font builder works — this runs once, at authoring time, and its
output is committed; nothing converts at build or run time.

Commit `internal/render/icons/LICENSE` recording the licence of the SVG sources and of the generated
font, as the charter's icon policy requires.

- [ ] **Step 2: Write the failing test**

Create `internal/render/iconfont_test.go`:

```go
package render

import "testing"

// Every WMO code the API can return must map to one of the eight symbols. An
// unmapped code renders the cloud rather than a missing glyph.
func TestEveryWeatherCodeMapsToAnIcon(t *testing.T) {
	t.Parallel()
	for code := 0; code <= 99; code++ {
		r := IconRune(code)
		if r < iconRuneFirst || r > iconRuneLast {
			t.Fatalf("code %d mapped to %U, outside the icon range", code, r)
		}
	}
}

func TestKnownCodesMapToTheExpectedSymbol(t *testing.T) {
	t.Parallel()
	cases := map[int]rune{
		0:  iconClearDay,
		2:  iconPartlyCloudy,
		3:  iconCloud,
		45: iconFog,
		61: iconRain,
		71: iconSnow,
		75: iconHeavySnow,
		95: iconThunderstorm,
	}
	for code, want := range cases {
		if got := IconRune(code); got != want {
			t.Fatalf("code %d mapped to %U, want %U", code, got, want)
		}
	}
}

// An icon rune must resolve to the project face, never to whatever system font
// happens to cover the private-use area.
func TestIconRunesResolveToTheProjectFace(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}

	face := m.Face(iconClearDay)
	if face == nil {
		t.Fatal("an icon rune resolved to no face")
	}
	if face == m.Primary() {
		t.Fatal("an icon rune resolved to the primary text face, not the icon face")
	}
}

// SplitRuns must isolate an icon rune so it shapes with the icon face while
// the surrounding text keeps the primary one.
func TestSplitRunsIsolatesAnIconRune(t *testing.T) {
	t.Parallel()
	m, err := NewSystemFontMap("sans-serif", "")
	if err != nil {
		t.Skipf("no system font available: %v", err)
	}

	runs := m.SplitRuns(string(iconClearDay) + " 18")
	if len(runs) < 2 {
		t.Fatalf("runs = %d, want the icon split from the text", len(runs))
	}
	if runs[0].Text != string(iconClearDay) {
		t.Fatalf("first run = %q, want the icon alone", runs[0].Text)
	}
	if runs[0].Face == runs[1].Face {
		t.Fatal("the icon and the text shaped with one face")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/render/ -run Icon -v`
Expected: FAIL to compile — `IconRune`, `iconClearDay` and the range constants undefined.

- [ ] **Step 4: Write the implementation**

Create `internal/render/iconfont.go`:

```go
package render

import (
	_ "embed"
	"sync"

	"github.com/go-text/typesetting/font"
)

// iconTTF is the project's own symbol font. It is committed rather than
// generated at build time: the charter forbids an external conversion process,
// and a font is deterministic once authored.
//
//go:embed icons/sysc-icons.ttf
var iconTTF []byte

// The eight symbols occupy consecutive private-use codepoints. A private-use
// range is chosen so an icon rune can never collide with real text.
const (
	iconClearDay rune = 0xE000 + iota
	iconPartlyCloudy
	iconCloud
	iconFog
	iconRain
	iconSnow
	iconHeavySnow
	iconThunderstorm

	iconRuneFirst = iconClearDay
	iconRuneLast  = iconThunderstorm
)

var (
	iconOnce sync.Once
	iconFace *font.Face
)

// loadIconFace parses the embedded font once. A font that fails to parse
// leaves the face nil, which falls the rune back to the system query and draws
// a notdef box: a broken icon must never fail a frame.
func loadIconFace() *font.Face {
	iconOnce.Do(func() {
		face, err := ParseFace(iconTTF)
		if err != nil {
			return
		}
		iconFace = face
	})
	return iconFace
}

// IconRune maps a WMO weather code to its symbol.
//
// The whole code set reduces to eight symbols, which is what both reference
// shells do. An unrecognised code renders the cloud rather than nothing, so a
// code the API adds later degrades instead of leaving a gap.
func IconRune(code int) rune {
	switch {
	case code == 0:
		return iconClearDay
	case code >= 1 && code <= 2:
		return iconPartlyCloudy
	case code == 3:
		return iconCloud
	case code >= 45 && code <= 48:
		return iconFog
	case code >= 51 && code <= 67, code >= 80 && code <= 82:
		return iconRain
	case code >= 71 && code <= 73, code == 85:
		return iconSnow
	case code == 75, code == 77, code == 86:
		return iconHeavySnow
	case code >= 95:
		return iconThunderstorm
	}
	return iconCloud
}
```

In `internal/render/fontmap.go`, resolve the icon face ahead of the system query inside `Face`:

```go
func (m *FontMap) Face(r rune) *font.Face {
	if face, ok := m.cache[r]; ok {
		return face
	}
	// The project face wins for its own range, so a system font that happens
	// to cover the private-use area can never take an icon rune.
	face := iconFaceFor(r)
	if face == nil {
		face = outlineFaceForRune(m.inner.ResolveFace(r), m.primary, r)
	}
	if len(m.order) >= faceCacheLimit {
		delete(m.cache, m.order[0])
		m.order = m.order[1:]
	}
	m.cache[r] = face
	m.order = append(m.order, r)
	return face
}

// iconFaceFor returns the project face for an icon rune, or nil.
func iconFaceFor(r rune) *font.Face {
	if r < iconRuneFirst || r > iconRuneLast {
		return nil
	}
	return loadIconFace()
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test -race ./internal/render/ -v`
Expected: PASS, including every pre-existing font and truncation test.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/render && go vet ./internal/render/
git add internal/render/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(render): resolve project symbols from an embedded face"
```

---

### Task 5: The error tone

**Files:**
- Modify: `internal/ui/tree.go`, `internal/render/paint.go`, `internal/shell/theme.go`
- Test: `internal/render/paint_test.go:` append

**Interfaces:**
- Consumes: nothing.
- Produces: `ui.Tone` with `ui.ToneNormal` and `ui.ToneError`; `ui.Node.Tone`;
  `render.ProofStyle.Error`. Task 6 sets the tone.

- [ ] **Step 1: Write the failing test**

Append to `internal/render/paint_test.go`:

```go
// Text reporting a failure paints in the error colour, not the foreground, so
// a failed reading is distinguishable at a glance.
func TestErrorToneTextPaintsInTheErrorColour(t *testing.T) {
	t.Parallel()

	canvas, style, r := newPaintFixture(t, 80, 20)
	style.Foreground = Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	style.Error = Color{R: 0xff, G: 0x40, B: 0x40, A: 0xff}

	root := &ui.Node{Kind: ui.KindRow, Children: []*ui.Node{{
		Kind:   ui.KindText,
		Text:   "-",
		Tone:   ui.ToneError,
		Bounds: ui.Rect{X: 0, Y: 0, W: 80, H: 20},
	}}}
	if err := Paint(canvas, root, r, style); err != nil {
		t.Fatalf("Paint: %v", err)
	}

	// The error colour has a low green channel; the foreground is white. Any
	// painted pixel must therefore be redder than it is green.
	var sawErrorPixel bool
	for i := 0; i+3 < len(canvas.pixels); i += 4 {
		red, green := canvas.pixels[i+2], canvas.pixels[i+1]
		if red > 0 && red > green {
			sawErrorPixel = true
			break
		}
	}
	if !sawErrorPixel {
		t.Fatal("error-tone text painted no pixel in the error colour")
	}
}
```

If `Canvas`'s pixel field is named other than `pixels`, use the real field — the test is in package
`render`. If `newPaintFixture` does not exist because Tranche 3B was not executed, add it as that plan's
Task 6 defines it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/render/ -run ErrorTone -v`
Expected: FAIL to compile — `ui.ToneError` and `ProofStyle.Error` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/ui/tree.go`:

```go
// Tone selects which theme colour paints a text node.
//
// Error is for text that reports a failure instead of a value. A stale value
// is still a value and stays normal, carrying its age in the text; the muted
// token measures 1.47:1 against the background and cannot carry text at all.
type Tone uint8

const (
	ToneNormal Tone = iota
	ToneError
)
```

and on `Node`:

```go
	// Tone selects the text colour. Zero is ToneNormal.
	Tone Tone
```

In `internal/render/paint.go`, add the field to `ProofStyle` beside `Foreground`:

```go
	// Error paints text that reports a failure. It is a distinct field rather
	// than reusing AccentOn, which the bar already uses for a toggled control.
	Error Color
```

and select the colour where text is painted:

```go
	blendMask(c, mask.Alpha, box.X, box.Y, textColor(style, tone))
```

with the helper:

```go
// textColor picks the colour a tone paints in.
func textColor(style ProofStyle, tone ui.Tone) Color {
	if tone == ui.ToneError {
		return style.Error
	}
	return style.Foreground
}
```

`paintText` gains a trailing `tone ui.Tone` parameter, and both call sites pass `n.Tone`.

In `internal/shell/theme.go`, carry the token into the style where `ProofStyle` is constructed — the
theme already parses `Error`:

```go
			Error: theme.Error,
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go build ./... && go test -race ./internal/render/ ./internal/ui/ ./internal/shell/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal && go vet ./...
git add internal/ui/ internal/render/ internal/shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(ui): paint failure text in the error token"
```

---

### Task 6: Configuration

Adds the `weather` block, the `weather` item, and the configuration's first cross-section check.

**Files:**
- Modify: `internal/config/config.go`, `internal/config/load.go`
- Test: `internal/config/config_test.go:` append

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `config.Weather{Latitude, Longitude float64; Unit string; Interval time.Duration;
  Configured bool}`; `config.Config.Weather`; `Item` gains `ShowCondition bool`. Tasks 7 and 8 read
  them.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestTheWeatherBlockResolves(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{
		"weather":{"latitude":0,"longitude":0,"unit":"fahrenheit","interval":"20m"},
		"bar":{"items":{"right":[{"id":"weather","max-width":160,"show-condition":true}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !cfg.Weather.Configured {
		t.Fatal("a supplied weather block did not resolve as configured")
	}
	if cfg.Weather.Unit != "fahrenheit" {
		t.Fatalf("unit = %q, want fahrenheit", cfg.Weather.Unit)
	}
	if cfg.Weather.Interval != 20*time.Minute {
		t.Fatalf("interval = %v, want 20m", cfg.Weather.Interval)
	}
	item := cfg.Bar.Right[0]
	if item.ID != "weather" || item.MaxWidth != 160 || !item.ShowCondition {
		t.Fatalf("item = %+v, want a weather widget with a cap and its condition", item)
	}
}

func TestTheWeatherBlockDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Parse([]byte(`{
		"weather":{"latitude":0,"longitude":0},
		"bar":{"items":{"right":[{"id":"weather"}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Weather.Unit != "celsius" {
		t.Fatalf("unit = %q, want the celsius default", cfg.Weather.Unit)
	}
	if cfg.Weather.Interval != 15*time.Minute {
		t.Fatalf("interval = %v, want the 15m default", cfg.Weather.Interval)
	}
	if cfg.Bar.Right[0].ShowCondition {
		t.Fatal("show-condition defaulted true, want false")
	}
}

// The configuration's first cross-section check. Without it the widget would
// render an error forever because of a block the user was never told about.
func TestAWeatherWidgetWithoutCoordinatesIsRejected(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`{"bar":{"items":{"right":[{"id":"weather"}]}}}`))
	if err == nil {
		t.Fatal("a weather widget with no weather block was accepted")
	}
	if !strings.Contains(err.Error(), "weather.latitude") {
		t.Fatalf("error %q does not name the missing field", err)
	}
}

// A weather block with no widget is harmless, not an error: a user may be
// mid-edit, and nothing renders wrong.
func TestAWeatherBlockWithoutAWidgetIsAccepted(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"weather":{"latitude":0,"longitude":0}}`)); err != nil {
		t.Fatalf("a weather block with no widget was rejected: %v", err)
	}
}

func TestOutOfRangeCoordinatesAreRejected(t *testing.T) {
	t.Parallel()
	cases := []struct{ body, want string }{
		{`{"weather":{"latitude":91,"longitude":0}}`, "weather.latitude"},
		{`{"weather":{"latitude":-91,"longitude":0}}`, "weather.latitude"},
		{`{"weather":{"latitude":0,"longitude":181}}`, "weather.longitude"},
		{`{"weather":{"latitude":0,"longitude":-181}}`, "weather.longitude"},
	}
	for _, c := range cases {
		err := errFromParse(t, c.body)
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("error %q does not name %q", err, c.want)
		}
	}
}

func TestAnInvalidWeatherUnitIsRejected(t *testing.T) {
	t.Parallel()
	err := errFromParse(t, `{"weather":{"latitude":0,"longitude":0,"unit":"kelvin"}}`)
	if !strings.Contains(err.Error(), "weather.unit") {
		t.Fatalf("error %q does not name the unit field", err)
	}
}

func TestANonPositiveWeatherIntervalIsRejected(t *testing.T) {
	t.Parallel()
	err := errFromParse(t, `{"weather":{"latitude":0,"longitude":0,"interval":"0s"}}`)
	if !strings.Contains(err.Error(), "weather.interval") {
		t.Fatalf("error %q does not name the interval field", err)
	}
}

// show-condition belongs to the weather widget alone.
func TestShowConditionOnTheWrongItemIsRejected(t *testing.T) {
	t.Parallel()
	body := `{"bar":{"items":{"right":[{"id":"clock","show-condition":true}]}}}`
	if _, err := Parse([]byte(body)); err == nil {
		t.Fatal("show-condition was accepted on a clock")
	}
}
```

If `errFromParse` does not exist because Tranche 3B was not executed, add it as that plan's Task 7
defines it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run Weather -v`
Expected: FAIL to compile — `Config.Weather` and `Item.ShowCondition` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`:

```go
// Weather is the process-wide weather source. Coordinates live here rather
// than on the item because one service serves every bar.
//
// Configured distinguishes a supplied block from the zero value, which is what
// lets a weather widget with no block fail with a useful message.
type Weather struct {
	Latitude   float64
	Longitude  float64
	Unit       string
	Interval   time.Duration
	Configured bool
}
```

```go
type Config struct {
	Bar     Bar
	Theme   Theme
	Weather Weather
	Outputs []OutputOverride
}
```

Add `"weather"` to `knownItems`, add `ShowCondition bool` to `Item`, and add the defaults:

```go
const (
	// defaultWeatherInterval matches the reference shell's fifteen minutes.
	defaultWeatherInterval = 15 * time.Minute
	defaultWeatherUnit     = "celsius"
)

// weatherUnits are the units the API accepts.
var weatherUnits = map[string]bool{"celsius": true, "fahrenheit": true}
```

In `internal/config/load.go`, add the wire type and decode it:

```go
type wireWeather struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Unit      *string  `json:"unit"`
	Interval  *string  `json:"interval"`
}
```

```go
type wireConfig struct {
	Bar     *wireBar     `json:"bar"`
	Theme   *wireTheme   `json:"theme"`
	Weather *wireWeather `json:"weather"`
	Outputs []wireOutput `json:"outputs"`
}
```

Add `ShowCondition *bool \`json:"show-condition"\`` to `wireItem`, and reject it off the weather widget
in `resolveItem` beside the other per-id option checks:

```go
	if w.ShowCondition != nil && w.ID != "weather" {
		return Item{}, pathErr(path+".show-condition",
			"is accepted only on a weather widget, not on %q", w.ID)
	}
```

and resolve it in the `switch w.ID`:

```go
	case "weather":
		if w.ShowCondition != nil {
			item.ShowCondition = *w.ShowCondition
		}
```

In `Parse`, resolve the block after the bar and before the outputs, then run the cross-section check:

```go
	if wire.Weather != nil {
		weather, err := applyWeather(*wire.Weather, "weather")
		if err != nil {
			return Config{}, err
		}
		cfg.Weather = weather
	}
	if err := requireWeatherWhenUsed(cfg); err != nil {
		return Config{}, err
	}
```

and add both functions:

```go
// applyWeather resolves and validates the weather block.
func applyWeather(w wireWeather, path string) (Weather, error) {
	out := Weather{
		Unit:       defaultWeatherUnit,
		Interval:   defaultWeatherInterval,
		Configured: true,
	}

	if w.Latitude == nil {
		return Weather{}, pathErr(path+".latitude", "is required")
	}
	if *w.Latitude < -90 || *w.Latitude > 90 {
		return Weather{}, pathErr(path+".latitude", "%v is outside -90 through 90", *w.Latitude)
	}
	out.Latitude = *w.Latitude

	if w.Longitude == nil {
		return Weather{}, pathErr(path+".longitude", "is required")
	}
	if *w.Longitude < -180 || *w.Longitude > 180 {
		return Weather{}, pathErr(path+".longitude", "%v is outside -180 through 180", *w.Longitude)
	}
	out.Longitude = *w.Longitude

	if w.Unit != nil {
		if !weatherUnits[*w.Unit] {
			return Weather{}, pathErr(path+".unit", "%q is not celsius or fahrenheit", *w.Unit)
		}
		out.Unit = *w.Unit
	}
	if w.Interval != nil {
		interval, err := time.ParseDuration(*w.Interval)
		if err != nil {
			return Weather{}, pathErr(path+".interval", "%q is not a duration such as 15m", *w.Interval)
		}
		if interval <= 0 {
			return Weather{}, pathErr(path+".interval", "%v is not positive", interval)
		}
		out.Interval = interval
	}
	return out, nil
}

// requireWeatherWhenUsed is the configuration's one cross-section rule: a
// weather widget needs coordinates, and they live in a different block.
//
// Without this the widget would render an error forever because of a block the
// user was never told to write. Every override is checked too, because an
// override can introduce the widget on one output alone.
func requireWeatherWhenUsed(cfg Config) error {
	if cfg.Weather.Configured {
		return nil
	}
	bars := []Bar{cfg.Bar}
	for _, o := range cfg.Outputs {
		bars = append(bars, o.Bar)
	}
	for _, bar := range bars {
		for _, section := range [][]Item{bar.Left, bar.Center, bar.Right} {
			for _, item := range section {
				if item.ID == "weather" {
					return pathErr("weather.latitude",
						"is required because a weather widget is configured")
				}
			}
		}
	}
	return nil
}
```

Add `"time"` to `load.go`'s imports if Tranche 3B has not already.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS, including every pre-existing configuration test.

- [ ] **Step 5: Commit**

```bash
go build ./... && gofmt -l internal/config && go vet ./internal/config/
git add internal/config/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(config): add the weather source and widget"
```

---

### Task 7: The weather widget

**Files:**
- Create: `internal/shell/weatherwidget.go`, `internal/shell/weatherwidget_test.go`
- Modify: `internal/shell/widget.go`, `internal/shell/registry.go`
- Test: `internal/shell/registry_test.go:` append

**Interfaces:**
- Consumes: `services.Weather` and `Reading` (Tasks 1–3), `render.IconRune` (Task 4), `ui.ToneError`
  (Task 5), `config.Weather` (Task 6).
- Produces: `formatWeather(config.Item, services.Reading) (string, ui.Tone)`; `barView` gains
  `Weather services.Reading`; `(*Registry).Weather() *services.Weather`;
  `(*Registry).UpdateWeather(services.Reading) []uint32`. Task 8 wires the pump.

- [ ] **Step 1: Write the failing test**

Create `internal/shell/weatherwidget_test.go`:

```go
package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

func TestAnObservedReadingRendersIconAndTemperature(t *testing.T) {
	t.Parallel()
	reading := services.Reading{
		Observed: true, Temperature: 18.4, Unit: services.UnitCelsius,
		Code: 0, FetchedAt: time.Now(),
	}

	text, tone := formatWeather(config.Item{ID: "weather"}, reading)
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal for an observed reading", tone)
	}
	if !strings.ContainsRune(text, render.IconRune(0)) {
		t.Fatalf("text %q carries no icon rune", text)
	}
	if !strings.Contains(text, "18") {
		t.Fatalf("text %q carries no temperature", text)
	}
}

// A reading that never arrived is an error, not a stale value.
func TestAnUnobservedReadingRendersTheErrorTone(t *testing.T) {
	t.Parallel()
	reading := services.Reading{FailedSince: time.Now().Add(-time.Minute)}

	text, tone := formatWeather(config.Item{ID: "weather"}, reading)
	if tone != ui.ToneError {
		t.Fatalf("tone = %v, want error for a reading that never arrived", tone)
	}
	if text == "" {
		t.Fatal("an error reading rendered nothing")
	}
}

// A stale reading is still a reading: it keeps the normal tone and shows its
// age, because an aged value is information and a blank widget is not.
func TestAStaleReadingKeepsItsValueAndTone(t *testing.T) {
	t.Parallel()
	reading := services.Reading{
		Observed: true, Temperature: 18.4, Unit: services.UnitCelsius, Code: 0,
		FetchedAt:   time.Now().Add(-90 * time.Minute),
		FailedSince: time.Now().Add(-30 * time.Minute),
	}

	text, tone := formatWeather(config.Item{ID: "weather"}, reading)
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal; a stale value is still a value", tone)
	}
	if !strings.Contains(text, "18") {
		t.Fatalf("a stale reading lost its temperature: %q", text)
	}
	if !strings.Contains(text, "1h") {
		t.Fatalf("a stale reading %q does not show its age", text)
	}
}

// Before the first fetch there is nothing to report and nothing has failed.
func TestAReadingBeforeTheFirstFetchRendersThePlaceholder(t *testing.T) {
	t.Parallel()
	text, tone := formatWeather(config.Item{ID: "weather"}, services.Reading{})
	if tone != ui.ToneNormal {
		t.Fatalf("tone = %v, want normal before the first fetch", tone)
	}
	if text != noWorkspace {
		t.Fatalf("text = %q, want the placeholder", text)
	}
}

func TestTheUnitSuffixFollowsTheReading(t *testing.T) {
	t.Parallel()
	celsius := services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius}
	fahrenheit := services.Reading{Observed: true, Temperature: 65, Unit: services.UnitFahrenheit}

	if text, _ := formatWeather(config.Item{ID: "weather"}, celsius); !strings.Contains(text, "°C") {
		t.Fatalf("celsius reading rendered %q", text)
	}
	if text, _ := formatWeather(config.Item{ID: "weather"}, fahrenheit); !strings.Contains(text, "°F") {
		t.Fatalf("fahrenheit reading rendered %q", text)
	}
}

func TestShowConditionAppendsTheConditionWord(t *testing.T) {
	t.Parallel()
	reading := services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius, Code: 95}

	plain, _ := formatWeather(config.Item{ID: "weather"}, reading)
	withWord, _ := formatWeather(config.Item{ID: "weather", ShowCondition: true}, reading)

	if len(withWord) <= len(plain) {
		t.Fatalf("show-condition rendered %q, no longer than %q", withWord, plain)
	}
	if !strings.Contains(strings.ToLower(withWord), "thunder") {
		t.Fatalf("condition text %q does not name the condition", withWord)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/shell/ -run Weather -v`
Expected: FAIL to compile — `formatWeather` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/shell/weatherwidget.go`:

```go
package shell

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/render"
	"github.com/Nomadcxx/sysc-shell/internal/services"
	"github.com/Nomadcxx/sysc-shell/internal/ui"
)

// conditionWords name the eight symbols for the optional condition text.
var conditionWords = map[rune]string{
	render.IconRune(0):  "Clear",
	render.IconRune(2):  "Partly cloudy",
	render.IconRune(3):  "Cloudy",
	render.IconRune(45): "Fog",
	render.IconRune(61): "Rain",
	render.IconRune(71): "Snow",
	render.IconRune(75): "Heavy snow",
	render.IconRune(95): "Thunderstorm",
}

// formatWeather renders one reading and the tone it should paint in.
//
// The three states are deliberately distinct. Nothing fetched yet renders the
// placeholder in the normal tone, because nothing has gone wrong. A fetch that
// has never succeeded renders an error. A reading whose fetch later began
// failing keeps its value and its normal tone, and shows its age: an aged
// temperature is information, a blank widget is not.
func formatWeather(item config.Item, reading services.Reading) (string, ui.Tone) {
	if !reading.Observed {
		if reading.FailedSince.IsZero() {
			return noWorkspace, ui.ToneNormal
		}
		return "weather unavailable", ui.ToneError
	}

	icon := render.IconRune(reading.Code)
	text := fmt.Sprintf("%c %.0f%s", icon, reading.Temperature, unitSuffix(reading.Unit))
	if item.ShowCondition {
		if word, ok := conditionWords[icon]; ok {
			text += " " + word
		}
	}
	if reading.Stale() {
		text += " (" + humaniseAge(time.Since(reading.FetchedAt)) + ")"
	}
	return text, ui.ToneNormal
}

func unitSuffix(u services.Unit) string {
	if u == services.UnitFahrenheit {
		return "°F"
	}
	return "°C"
}

// humaniseAge renders an age at one significant unit. A bar has no room for
// "1h32m14s", and the reader only needs to know roughly how old this is.
func humaniseAge(age time.Duration) string {
	switch {
	case age >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours())/24)
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return "now"
}
```

In `internal/shell/widget.go`, add the field to `barView`:

```go
	// Weather is the newest reading. Its zero value renders the placeholder.
	Weather services.Reading
```

and the case to `buildWidgets`:

```go
		case "weather":
			node := &ui.Node{Kind: ui.KindText, MaxWidth: item.MaxWidth}
			out = append(out, textWidget{
				node: node,
				format: func(v barView) string {
					text, tone := formatWeather(item, v.Weather)
					node.Tone = tone
					return text
				},
			})
```

In `internal/shell/registry.go`, construct the service from the resolved configuration, acquire a lease
per weather item, and add the update path:

```go
	weather *services.Weather
```

```go
	reg := &Registry{
		...
		weather: services.NewWeather(
			cfg.Weather.Latitude, cfg.Weather.Longitude, weatherUnit(cfg.Weather.Unit)),
	}
```

```go
// weatherUnit maps the validated configuration string to the service unit.
func weatherUnit(name string) services.Unit {
	if name == "fahrenheit" {
		return services.UnitFahrenheit
	}
	return services.UnitCelsius
}

// Weather is the shared weather service. The process pumps its updates into
// UpdateWeather.
func (r *Registry) Weather() *services.Weather { return r.weather }

// UpdateWeather applies a reading to every bar and reports the globals whose
// text actually changed.
func (r *Registry) UpdateWeather(reading services.Reading) []uint32 {
	r.mu.Lock()
	r.reading = reading
	var changed []uint32
	for global, bar := range r.bars {
		if bar.apply(r.viewLocked(bar.connector())) {
			changed = append(changed, global)
		}
	}
	r.mu.Unlock()

	r.publish(changed)
	return changed
}
```

Add `reading services.Reading` to the struct, carry it in `viewLocked` as `Weather: r.reading`, acquire
the lease in `buildBar` beside the clock and metric leases:

```go
	for _, item := range allItems(policy) {
		if item.ID != "weather" {
			continue
		}
		lease, err := r.weather.Acquire(cfg.Weather.Interval)
		if err != nil {
			releaseAll(leases)
			return nil, nil, wayland.HostCallbacks{}, err
		}
		leases = append(leases, lease)
	}
```

and close it in `Registry.Close` beside the others:

```go
	r.weather.Close()
```

If `allItems` does not exist because Tranche 3B was not executed, add it as that plan's Task 9 defines
it.

Append to `internal/shell/registry_test.go`:

```go
func weatherConfig() config.Config {
	cfg := config.Default()
	cfg.Weather = config.Weather{
		Latitude: 0, Longitude: 0, Unit: "celsius",
		Interval: 15 * time.Minute, Configured: true,
	}
	cfg.Bar.Left = []config.Item{{ID: "weather", MaxWidth: 160}}
	cfg.Bar.Center, cfg.Bar.Right = nil, nil
	return cfg
}

func TestTwoBarsShareOneWeatherServiceAndOneReading(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9", 2: "HDMI-A-9"})

	if got := reg.Weather().Starts(); got != 1 {
		t.Fatalf("weather starts = %d, want 1 shared start for two bars", got)
	}
	changed := reg.UpdateWeather(services.Reading{
		Observed: true, Temperature: 18, Unit: services.UnitCelsius,
	})
	if len(changed) != 2 {
		t.Fatalf("one reading changed %d bars, want 2", len(changed))
	}
}

func TestAnUnchangedReadingChangesNothing(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(weatherConfig())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	reading := services.Reading{Observed: true, Temperature: 18, Unit: services.UnitCelsius}
	if changed := reg.UpdateWeather(reading); len(changed) != 1 {
		t.Fatalf("first reading changed %v, want global 1", changed)
	}
	if changed := reg.UpdateWeather(reading); len(changed) != 0 {
		t.Fatalf("an identical reading changed %v", changed)
	}
}

func TestAConfigWithNoWeatherWidgetLeavesTheServiceStopped(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(config.Default())
	t.Cleanup(reg.Close)
	newHosts(t, reg, map[uint32]string{1: "DP-9"})

	if reg.Weather().Running() {
		t.Fatal("a configuration with no weather widget started the service")
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go build ./... && go test -race ./internal/shell/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/shell && go vet ./internal/shell/
git add internal/shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(shell): render the weather widget from a reading"
```

---

### Task 8: Process wiring

**Files:**
- Modify: `cmd/sysc-shell/main.go`

**Interfaces:**
- Consumes: `Registry.Weather`, `Registry.UpdateWeather` from Task 7.
- Produces: nothing.

- [ ] **Step 1: Write the implementation**

There is no new observable behaviour at this layer: `run` opens Wayland, which no unit test can do. The
regression net is the existing `TestRunRequiresNiriSocket`.

In `cmd/sysc-shell/main.go`, add a pump beside the clock one:

```go
	// The weather service publishes on its own goroutine; this pump turns each
	// reading into per-bar text and hands the changed outputs to the Wayland
	// owner. One reading serves every bar.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case reading := <-registry.Weather().Updates():
				registry.UpdateWeather(reading)
			}
		}
	}()
```

`registry.Close()` already runs on the deferred path and now closes the weather service too.

- [ ] **Step 2: Run the regression test and the suite**

```bash
go test ./cmd/sysc-shell/ -v
go build -o /tmp/sysc-shell-tranche3d ./cmd/sysc-shell
go test -race -count=1 ./... 2>&1 | tail -20
```

Expected: PASS everywhere and a successful build.

- [ ] **Step 3: Commit**

```bash
gofmt -l cmd
git add cmd/sysc-shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(shell): pump weather readings into the bar registry"
```

**Weather, icons and the error tone are complete at this point.** Tasks 9 through 11 add the tooltip and
may be cut as a unit without stranding anything above.

---

### Task 9: Basic auxiliary-surface host

**Files:**
- Modify: `internal/platform/wayland/host.go`, `internal/platform/wayland/client.go`
- Create: `internal/platform/wayland/aux.go`
- Test: `internal/platform/wayland/aux_test.go`

**Interfaces:**
- Produces: `surfaceUnit`, `AuxSpec`, `AuxRequest`, and `Callbacks.Aux`.
- Preserves: one Wayland owner goroutine; the application still supplies `HostCallbacks`.

- [ ] **Step 1: Record the platform baseline**

Run:

```bash
go test -count=1 ./internal/platform/wayland/
```

Expected: PASS. Record the package test count for comparison after the extraction.

- [ ] **Step 2: Extract `surfaceUnit` without behavior change**

Move the bar's surface, layer, scale, viewport, scheduler, buffer generations, frame callback, cleanup,
and `HostCallbacks` fields from `OutputHost` into:

```go
// surfaceUnit owns one layer surface and its render-buffer lifecycle.
// The Wayland owner is the only goroutine that mutates it.
type surfaceUnit struct {
	id string

	surface  *client.Surface
	layer    *layershell.ZwlrLayerSurfaceV1
	scale    *fractionalscale.WpFractionalScaleV1
	viewport *viewporter.WpViewport

	ss    *surfaceState
	sched *render.Scheduler

	current  *generation
	retiring []*generation
	genID    int

	frameCallback *client.Callback
	cleanup       cleanupStack
	app           HostCallbacks
}
```

`OutputHost` retains output identity and policy and gains `bar *surfaceUnit` and
`aux map[string]*surfaceUnit`. Route configure, scale, frame, buffer release, render, close, and
cleanup through the unit. Keep the existing bar behavior byte-for-byte.

Run:

```bash
go test -count=1 ./internal/platform/wayland/ && go test -count=1 ./...
```

Expected: PASS with the same platform test count.

Commit:

```bash
git add internal/platform/wayland/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "refactor(wayland): extract surface unit"
```

- [ ] **Step 3: Write failing lifecycle tests**

Use the existing fake-compositor harness. Cover real protocol creation and destruction, not placement
arithmetic alone:

```go
func TestAuxOpensConfiguresRendersAndCloses(t *testing.T) {
	// Start one fake output and map its bar.
	// Open an Overlay aux unit with application Configure/Render callbacks.
	// Ack configure, release its buffer, then close it.
	// Assert callbacks ran, layer and wl_surface were destroyed once,
	// and the bar stayed mapped and rendered again.
}

func TestAuxEmptyInputRegionIsCommitted(t *testing.T) {
	empty := []ui.Rect{}
	// Open with InputRegion: &empty.
	// Assert wl_surface.set_input_region receives an empty region before commit.
}

func TestAuxReplaceDestroysOldGenerationFirst(t *testing.T) {
	// Open the same ID twice with different callbacks and size.
	// Assert one live unit remains and the old proxy/buffers were cleaned.
}

func TestOutputLossDropsEveryAuxUnit(t *testing.T) { /* DropAux once per id */ }
func TestShutdownDrainsAndDestroysAuxUnits(t *testing.T) { /* no proxy leak */ }
```

Run: `go test ./internal/platform/wayland/ -run Aux -v`

Expected: FAIL because the auxiliary types and owner path do not exist.

- [ ] **Step 4: Implement the basic open-close path**

Add:

```go
// AuxSpec describes one auxiliary layer surface. A non-nil InputRegion
// replaces the compositor default; a pointer to an empty slice makes the
// surface pointer-transparent.
type AuxSpec struct {
	ID        string
	Namespace string
	Layer     layershell.ZwlrLayerShellV1Layer
	Anchor    uint32

	MarginTop, MarginBottom, MarginLeft, MarginRight int32
	Width, Height                                    int32
	ExclusiveZone                                   int32
	Keyboard                                        uint32
	InputRegion                                     *[]ui.Rect
	Callbacks                                       HostCallbacks
}

// AuxRequest opens or replaces Open when non-nil. Otherwise it closes ID.
type AuxRequest struct {
	Output uint32
	ID     string
	Open   *AuxSpec
}
```

Add optional callbacks:

```go
type Callbacks struct {
	// existing fields...
	Aux     <-chan AuxRequest
	DropAux func(output uint32, id string)
}
```

The owner must:

1. validate a non-empty bounded ID and namespace, positive fitted size, valid layer enum, margins, and
   input rectangles before creating proxies;
2. create the `surfaceUnit` through the same scale, viewport, scheduler, and buffer path as the bar;
3. apply geometry, keyboard mode, and a supplied input region before the initial commit;
4. replace an existing ID only after destroying its old unit;
5. close unknown IDs as a no-op;
6. call `DropAux` once on requested close, compositor close, output loss, replacement, or shutdown.

A nil `Aux` channel disables the path. Reload does not destroy auxiliary units by itself; the shell
owns transient-versus-persistent policy.

Run:

```bash
go test -race -count=1 ./internal/platform/wayland/ && go test -count=1 ./...
```

Expected: PASS with no race or leaked fake-compositor object.

Commit:

```bash
gofmt -l internal/platform/wayland && go vet ./internal/platform/wayland/
git add internal/platform/wayland/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(wayland): host auxiliary surfaces"
```

---

### Task 10: Shared tooltip model and process-wide dwell

**Files:**
- Modify: `internal/ui/tree.go`
- Modify: `internal/shell/bar.go`, `internal/shell/weatherwidget.go`, `internal/shell/registry.go`
- Create: `internal/shell/tooltip.go`
- Test: `internal/ui/tree_test.go`, `internal/shell/tooltip_test.go`

**Interfaces:**
- Consumes: Task 9's `AuxRequest`.
- Produces: `ui.Node.Tooltip`, `ui.TooltipAt`, and one process-wide tooltip controller.

- [ ] **Step 1: Write the failing node and content tests**

```go
func TestTooltipAtUsesReversePaintOrderAndDeepestNode(t *testing.T) {
	back := &Node{Bounds: Rect{W: 100, H: 40}, Tooltip: "back"}
	frontChild := &Node{Bounds: Rect{X: 10, Y: 5, W: 20, H: 20}, Tooltip: "front child"}
	root := &Node{Bounds: Rect{W: 100, H: 40}, Children: []*Node{
		back,
		{Bounds: Rect{W: 100, H: 40}, Tooltip: "front", Children: []*Node{frontChild}},
	}}
	got, ok := TooltipAt(root, 15, 10)
	if !ok || got != frontChild {
		t.Fatalf("got %#v, want deepest front node", got)
	}
}

func TestWeatherNodeHasBoundedTooltipFromExistingReading(t *testing.T) {
	// Apply a stale reading. Assert node.Tooltip contains condition,
	// formatted temperature, and staleness, and is no more than 2 KiB.
}
```

`TooltipAt` walks children from last to first, then considers the parent. It ignores nodes outside
their bounds and nodes with empty tooltip text. Add `Tooltip string` to `ui.Node`; do not add it to
`textWidget`.

Run: `go test ./internal/ui/ ./internal/shell/ -run Tooltip -v`

Expected: FAIL because the node field and lookup do not exist.

- [ ] **Step 2: Implement node-owned tooltip content**

Weather builds its tooltip from data already in `services.Reading`: condition, temperature, and
staleness or current status. Do not extend the Open-Meteo request. Normalize whitespace and truncate at
a UTF-8 boundary to 2 KiB.

Run: `go test ./internal/ui/ ./internal/shell/ -run Tooltip -v`

Expected: PASS.

- [ ] **Step 3: Write failing dwell, placement, and cleanup tests**

Use a fake clock or injected timer function so tests do not depend on scheduler timing:

```go
func TestDwellPublishesOnlyCurrentGeneration(t *testing.T) {
	// Enter A, capture timer A; enter B, capture timer B.
	// Fire A then B. Only B may open.
}

func TestLeaveBeforeDwellCancelsWithoutOpening(t *testing.T) { /* generation advances */ }
func TestVisibleTooltipRefreshReplacesContent(t *testing.T) { /* same source, new text */ }
func TestPressClosesBeforeNormalBarPressHandling(t *testing.T) { /* click still delivered */ }
func TestTooltipTerminalPathsCloseOnce(t *testing.T) {
	// Table: leave, root opening, reload, output loss, source removal, shutdown.
}
func TestTooltipPlacementForEveryBarEdge(t *testing.T) {
	// Table top/bottom/left/right, both output extremes, oversized content.
	// Assert the fitted rectangle stays within logical output bounds and lies
	// outside the bar on the expected side where space permits.
}
```

Run: `go test ./internal/shell/ -run 'Dwell|TooltipPlacement|TooltipTerminal' -v`

Expected: FAIL.

- [ ] **Step 4: Implement one process-wide controller**

Keep the controller under `Registry.mu`. It stores the current output, source node identity, anchor,
bar edge, text, visible state, and a monotonically increasing generation. Every enter, leave, press,
content refresh, reload, root opening, output loss, source removal, and shutdown advances generation.
A timer callback carries the captured generation and sends a plain message; the registry ignores it
unless it still matches.

On a current dwell:

1. build a small `ui.Node` tree from the bounded text;
2. measure and wrap it with the shell text functions at a fixed maximum width;
3. compute logical placement from the source rectangle and bar edge;
4. send `AuxRequest{Open: ...}` with ID `"tooltip"`, keyboard none, and a pointer to an empty input
   region;
5. supply `Configure` and `Render` callbacks that lay out and paint the node through the normal
   shell renderer.

Wayland owns no text measurement or paint logic. A visible content change sends a replacement
`AuxRequest` for the same ID so size and callbacks stay current. Closing sends
`AuxRequest{ID: "tooltip"}`; `DropAux` clears visible state without reopening it.

Bar pointer handling calls `ui.TooltipAt` after layout. Enter/motion starts or replaces the candidate.
Leave and any press close first, then preserve existing press/release behavior. Add
`Registry.CloseTooltipForRootOpen()`; Tranche 4A's root coordinator must call it before opening a root.

Run:

```bash
go test -race -count=1 ./internal/ui/ ./internal/shell/
```

Expected: PASS with deterministic timer tests and no race.

Commit:

```bash
gofmt -l internal/ui internal/shell && go vet ./internal/ui ./internal/shell/
git add internal/ui/ internal/shell/
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "feat(shell): show node tooltips after dwell"
```

---

### Task 11: Wiring and tranche evidence

**Files:**
- Modify: `cmd/sysc-shell/main.go`, `internal/shell/registry.go`
- Modify: `tests/integration/README.md`
- Create: `internal/shell/tranche3d_test.go`

- [ ] **Step 1: Wire the auxiliary channel and drop callback**

Pass `Registry.AuxRequests()` and `Registry.DropAux` into `wayland.Callbacks`. The registry closes
the tooltip during an accepted reload, before output removal, and during `Close`. Verify the owner
still receives every request through its wake path and no shell goroutine calls a Wayland proxy.

- [ ] **Step 2: Add cross-cutting tests**

Retain the weather lifetime, accepted-reload, and stale-reading tests. Add:

```go
func TestTooltipLifecycleThroughFakeCompositor(t *testing.T) {
	// Hover a weather node, fire the injected dwell, and run the request through
	// the fake compositor. Assert configure and shell render occur, the input
	// region is empty, then leave and assert every aux proxy/buffer is gone.
}

func TestReloadAndOutputLossLeaveNoTooltip(t *testing.T) { /* both paths */ }
func TestClosingRegistryStopsWeatherAndDwellWork(t *testing.T) { /* no late request */ }
```

Run:

```bash
go build ./... && go test -race -count=1 ./...
```

Expected: every package reports `ok`; no race report.

- [ ] **Step 3: Record and execute the live matrix**

Append the Tranche 3D matrix to `tests/integration/README.md`:

1. Run on one output, then two; both render the same service reading independently.
2. Disconnect and reconnect the network; the last good reading becomes stale and later recovers.
3. Reload coordinates, units, and interval; the service remains one live instance.
4. Hover weather on top, bottom, left, and right bars; the tooltip opens after dwell on the correct
   side and stays inside each logical output.
5. Move across overlapping eligible nodes; reverse paint order selects the visible front node and no
   stale timer opens the old tooltip.
6. Change the weather reading while open; content updates.
7. Leave, press, reload, open an interactive root, unplug the output, and stop the shell; each closes
   the tooltip with no flash, input capture, or leaked surface.
8. Run the 60-minute idle CPU and wakeup comparison against Tranche 3B.

Record observations and defects in the completion handover. Do not put local coordinates or machine
measurements in Git.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./...
git add cmd/sysc-shell/ internal/shell/ tests/integration/README.md
BEADS_DB=/home/nomadx/sysc-shell/.beads/beads.db git commit -m "test(shell): qualify weather tooltips"
```

Write `docs/plans/2026-08-30-weather-and-visual-vocabulary-completion-handover.md` with commit hashes,
fresh gate output, live observations, defects, and the next unblocked bead.

---

## Deviations to report

Stop and return to the owner rather than improvising if any of these occur:

- The tooltip appears to need a dismiss shield, keyboard focus, or a second surface.
- The dwell timer appears to need to call a Wayland proxy.
- Open-Meteo appears to need an API key, a second host, or geocoding.
- The icon font cannot be authored without a runtime decoder or an external conversion step.
- Tranche 4A has already introduced a conflicting auxiliary host. Reconcile ownership before either
  tranche writes overlapping Wayland code.
