package weather

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	owm "github.com/Nomadcxx/sysc-shell/weather"
)

const (
	connectAndReadBudget = 6 * time.Second
	defaultInterval      = 15 * time.Minute
	minFetchInterval     = 30 * time.Second
	retryDelay           = 30 * time.Second
	maxRetryAttempts     = 3
	backoffBase          = 60 * time.Second
	backoffCap           = 300 * time.Second
)

// Config is the Weather process request and schedule.
type Config struct {
	Latitude, Longitude float64
	Unit                owm.Unit
	Interval            time.Duration
	Enabled             bool
}

// Snapshot is the newest forecast and the state of the fetch that produced it.
type Snapshot struct {
	Forecast    owm.Forecast
	Observed    bool
	FetchedAt   time.Time
	FailedSince time.Time
	Disabled    bool
}

func (s Snapshot) Stale() bool { return s.Observed && !s.FailedSince.IsZero() }

func ParseConfig(values map[string]any) (Config, error) {
	cfg := Config{Enabled: true, Interval: defaultInterval}
	if values == nil {
		values = map[string]any{}
	}
	if v, ok := values["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	if v, ok := values["unit"].(string); ok {
		switch v {
		case "", "celsius":
			cfg.Unit = owm.UnitCelsius
		case "fahrenheit":
			cfg.Unit = owm.UnitFahrenheit
		default:
			return Config{}, fmt.Errorf("weather: unit %q is not celsius or fahrenheit", v)
		}
	}
	if v, ok := values["interval"].(string); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("weather: interval %q is not a duration such as 15m", v)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("weather: interval %v is not positive", d)
		}
		cfg.Interval = d
	}
	lat, hasLat := asFloat(values["latitude"])
	lon, hasLon := asFloat(values["longitude"])
	if hasLat {
		if lat < -90 || lat > 90 {
			return Config{}, fmt.Errorf("weather: latitude %v is outside -90 through 90", lat)
		}
		cfg.Latitude = lat
	}
	if hasLon {
		if lon < -180 || lon > 180 {
			return Config{}, fmt.Errorf("weather: longitude %v is outside -180 through 180", lon)
		}
		cfg.Longitude = lon
	}
	return cfg, nil
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func normalize(cfg Config) Config {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	return cfg
}

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

// Service fetches a seven-day forecast on one goroutine.
type Service struct {
	mu     sync.Mutex
	cfg    Config
	snap   Snapshot
	cancel context.CancelFunc
	closed bool
	starts int

	rearm   chan struct{}
	updates chan Snapshot
	stop    chan struct{}
	done    chan struct{}

	client      *http.Client
	endpoint    string
	minInterval time.Duration
}

func newService(cfg Config) *Service {
	cfg = normalize(cfg)
	return &Service{
		cfg:         cfg,
		snap:        Snapshot{Disabled: !cfg.Enabled},
		rearm:       make(chan struct{}, 1),
		updates:     make(chan Snapshot, 1),
		client:      &http.Client{Timeout: connectAndReadBudget},
		endpoint:    owm.DefaultEndpoint,
		minInterval: minFetchInterval,
	}
}

func New(cfg Config) *Service {
	s := newService(cfg)
	s.start()
	return s
}

func (s *Service) start() {
	s.mu.Lock()
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	s.starts++
	stop, done := s.stop, s.done
	s.mu.Unlock()
	go s.run(stop, done)
}

func (s *Service) Updates() <-chan Snapshot { return s.updates }

func (s *Service) Starts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts
}

func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

func (s *Service) Reconfigure(cfg Config) {
	cfg = normalize(cfg)
	s.mu.Lock()
	if s.cfg == cfg {
		s.mu.Unlock()
		return
	}
	s.cfg = cfg
	s.mu.Unlock()
	select {
	case s.rearm <- struct{}{}:
	default:
	}
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	stop, done, cancel := s.stop, s.done, s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stop != nil {
		close(stop)
		<-done
	}
}

func (s *Service) run(stop, done chan struct{}) {
	defer close(done)

	var (
		snap     Snapshot
		failures int
		lastAt   time.Time
		first    = true
	)

	for {
		s.mu.Lock()
		cfg := s.cfg
		floor := s.minInterval
		s.mu.Unlock()

		if !cfg.Enabled {
			snap.Disabled = true
			s.publish(snap)
			select {
			case <-stop:
				return
			case <-s.rearm:
				continue
			}
		}
		snap.Disabled = false

		wait := cfg.Interval
		if first {
			wait = 0
			first = false
		}
		if failures > 0 {
			wait = retryAfter(failures)
		}
		if since := time.Since(lastAt); !lastAt.IsZero() && floor > 0 && since < floor {
			if remaining := floor - since; remaining > wait {
				wait = remaining
			}
		}

		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-s.rearm:
				timer.Stop()
				continue
			case <-timer.C:
			}
		} else {
			select {
			case <-stop:
				return
			default:
			}
		}

		lastAt = time.Now()
		fc, err := s.fetch(stop)
		if err != nil {
			select {
			case <-stop:
				return
			default:
			}
			failures++
			if snap.FailedSince.IsZero() {
				snap.FailedSince = time.Now()
			}
		} else {
			failures = 0
			snap = Snapshot{
				Forecast:  fc,
				Observed:  true,
				FetchedAt: time.Now(),
			}
		}
		s.publish(snap)
	}
}

func (s *Service) fetch(stop chan struct{}) (owm.Forecast, error) {
	s.mu.Lock()
	q := owm.Query{
		Latitude: s.cfg.Latitude, Longitude: s.cfg.Longitude, Unit: s.cfg.Unit,
		Daily: true, Endpoint: s.endpoint,
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), connectAndReadBudget)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel = nil
		}
		s.mu.Unlock()
	}()

	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	return owm.Fetch(ctx, s.client, q)
}

func (s *Service) publish(snap Snapshot) {
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	select {
	case s.updates <- snap:
		return
	default:
	}
	select {
	case <-s.updates:
	default:
	}
	select {
	case s.updates <- snap:
	default:
	}
}
