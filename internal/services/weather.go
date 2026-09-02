package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Nomadcxx/sysc-shell/weather"
)

// Unit is the temperature unit the API is asked for. The shell does not
// convert; it requests the unit the user configured.
type Unit = weather.Unit

const (
	UnitCelsius    = weather.UnitCelsius
	UnitFahrenheit = weather.UnitFahrenheit
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
	backoffBase      = 60 * time.Second
	backoffCap       = 300 * time.Second
	maxResponseBytes = weather.MaxResponseBytes
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
	// minInterval is the fetch floor. Zero disables it, which tests that need
	// two fetches in one deadline use; NewWeather sets the production floor.
	minInterval time.Duration
}

const openMeteoEndpoint = weather.DefaultEndpoint

func NewWeather(latitude, longitude float64, unit Unit) *Weather {
	return &Weather{
		rearm:       make(chan struct{}, 1),
		updates:     make(chan Reading, 1),
		latitude:    latitude,
		longitude:   longitude,
		unit:        unit,
		client:      &http.Client{Timeout: connectAndReadBudget},
		endpoint:    openMeteoEndpoint,
		minInterval: minFetchInterval,
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

// RequestURL is the current Open-Meteo request. Reload tests assert it moved
// without restarting the service; coordinates never surface any other way.
func (w *Weather) RequestURL() string { return w.requestURL() }

func (w *Weather) requestURLLocked() string {
	return weather.RequestURL(w.endpoint, weather.Query{
		Latitude: w.latitude, Longitude: w.longitude, Unit: w.unit,
	})
}

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
		floor := w.minInterval
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
		if since := time.Since(lastAt); !lastAt.IsZero() && floor > 0 && since < floor {
			if remaining := floor - since; remaining > wait {
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

func (w *Weather) fetch() (Reading, error) {
	w.mu.Lock()
	q := weather.Query{Latitude: w.latitude, Longitude: w.longitude, Unit: w.unit, Endpoint: w.endpoint}
	unit := w.unit
	w.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), connectAndReadBudget)
	defer cancel()
	fc, err := weather.Fetch(ctx, w.client, q)
	if err != nil {
		return Reading{}, err
	}
	return Reading{
		Observed:    true,
		Temperature: fc.Current.Temperature,
		Unit:        unit,
		Code:        fc.Current.Code,
		FetchedAt:   time.Now(),
	}, nil
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
