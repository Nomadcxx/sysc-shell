package services

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

// Reconfigure points the service at a different request. Coordinates and unit
// are the request itself, so unlike an interval they cannot be a lease
// concern: a reload that changes city or unit has to reach the service.
//
// The no-op path matters because every accepted reload calls this, including
// the overwhelming majority that change something else entirely.
//
// The *Weather pointer is never replaced. Live leases hold it, and swapping it
// would strand them on a service nothing fetches for.
func (w *Weather) Reconfigure(latitude, longitude float64, unit Unit) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if latitude == w.latitude && longitude == w.longitude && unit == w.unit {
		return
	}
	w.latitude, w.longitude, w.unit = latitude, longitude, unit

	// Re-arm rather than restart, so the new request is issued at the next
	// opportunity instead of up to a full interval later, and Starts() stays
	// at 1 for a service that is still in use.
	select {
	case w.rearm <- struct{}{}:
	default:
	}
}

func (w *Weather) requestURL() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.requestURLLocked()
}

func (w *Weather) requestURLLocked() string {
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

func (w *Weather) run(stop, done chan struct{}) { <-stop; close(done) }
