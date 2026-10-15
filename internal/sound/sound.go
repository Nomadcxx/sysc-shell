// Package sound is the shell's output path: one mixer, one stream opened on
// the first cue and closed after silence, decoders and tone synthesis.
//
// A cue never blocks its caller and never reaches the user as an error. An
// absent server, a refused stream, a decode failure and a full queue all end
// in silence; the caller may read the reason from the Voice, and nothing else
// does. With the subsystem disabled no goroutine starts, and while silent no
// timer wakes the shell.
package sound

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// The mixer format: 48 kHz, two channels, 16-bit signed, interleaved.
const (
	SampleRate = 48000
	Channels   = 2
)

// Bounds. Nothing the subsystem holds grows past these.
const (
	// MaxVoices is how many cues sound at once; a cue past it is dropped,
	// never queued.
	MaxVoices = 8
	// MaxSeconds is the longest sound decoded.
	MaxSeconds = 10
	// maxFileBytes bounds a file read for decoding.
	maxFileBytes = 16 << 20
	// queueDepth bounds cues waiting for the mixer.
	queueDepth = 8
	// maxCached bounds decoded sounds kept for reuse.
	maxCached = 16
	// defaultIdle is how long the stream stays open after the last sound.
	defaultIdle = 4 * time.Second
)

var (
	ErrDisabled      = errors.New("sound: disabled")
	ErrClosed        = errors.New("sound: closed")
	ErrBusy          = errors.New("sound: too many cues waiting")
	ErrTooManyVoices = errors.New("sound: too many cues sounding")
	ErrUnavailable   = errors.New("sound: no audio server")
)

// Endpoint opens a playback stream in the mixer format. The stream pulls its
// samples from fill, on a goroutine of the endpoint's own.
type Endpoint interface {
	Open(fill func([]int16) int) (Stream, error)
}

// Stream is one open playback stream.
type Stream interface {
	Close()
	// Lost reports a stream the server dropped.
	Lost() bool
}

// Options configures a Player.
type Options struct {
	// Enabled false means no goroutine, no stream and no wake, ever.
	Enabled  bool
	Endpoint Endpoint
	// IdleTimeout is how long the stream stays open after the last sound;
	// zero is four seconds.
	IdleTimeout time.Duration
}

// Tone is a synthesised cue: a sine sliding from Hz to To (To zero holds Hz)
// with a short rise and fall so it starts and ends without a click.
type Tone struct {
	Hz, To, Seconds float64
}

// Spec is a sound file, WAV or Ogg Vorbis, at a gain from 0 to 1.
type Spec struct {
	Path string
	Gain float64
}

// Voice is one cue. Done closes when it has finished, been stopped, or been
// refused, and Err then says which.
type Voice struct {
	done    chan struct{}
	once    sync.Once
	err     error
	pcm     []int16
	pos     int
	gain    float64
	stopped bool
}

func newVoice() *Voice { return &Voice{done: make(chan struct{})} }

func (v *Voice) Done() <-chan struct{} { return v.done }

// Err is why the cue made no sound, or nil once it played out.
func (v *Voice) Err() error {
	select {
	case <-v.done:
		return v.err
	default:
		return nil
	}
}

func (v *Voice) finish(err error) {
	v.once.Do(func() {
		v.err = err
		close(v.done)
	})
}

type request struct {
	tone *Tone
	spec *Spec
	v    *Voice
}

// Player is the subsystem. The zero value is not usable; call New.
type Player struct {
	opts  Options
	reqs  chan request
	quit  chan struct{}
	ended chan struct{}
	start sync.Once
	stop  sync.Once

	mu        sync.Mutex
	voices    []*Voice
	gain      float64
	closed    bool
	running   bool
	tick      bool
	lastSound time.Time
	cache     map[Tone][]int16

	// beforeDecode runs on the mixer goroutine before each decode, so a test
	// can prove the decode is not on the caller's.
	beforeDecode func()
}

// New returns a player. Nothing starts until the first cue.
func New(opts Options) *Player {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdle
	}
	return &Player{
		opts: opts, reqs: make(chan request, queueDepth),
		quit: make(chan struct{}), ended: make(chan struct{}),
		gain: 1, cache: map[Tone][]int16{},
	}
}

// Enabled reports whether cues can sound at all. It is a cheap read for the
// cue path.
func (p *Player) Enabled() bool { return p.opts.Enabled && p.opts.Endpoint != nil }

// Tone queues a synthesised cue and returns at once.
func (p *Player) Tone(t Tone) (*Voice, error) { return p.queue(request{tone: &t}) }

// Play queues a sound file and returns at once.
func (p *Player) Play(s Spec) (*Voice, error) { return p.queue(request{spec: &s}) }

func (p *Player) queue(r request) (*Voice, error) {
	if !p.Enabled() {
		return nil, ErrDisabled
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrClosed
	}
	p.running = true
	p.mu.Unlock()
	p.start.Do(func() { go p.run() })
	r.v = newVoice()
	select {
	case p.reqs <- r:
		return r.v, nil
	default:
		return nil, ErrBusy
	}
}

// Stop silences a voice.
func (p *Player) Stop(v *Voice) {
	if v == nil {
		return
	}
	p.mu.Lock()
	v.stopped = true
	p.mu.Unlock()
}

// SetVolume is the subsystem's own gain, 0 to 100, under the sink's volume.
func (p *Player) SetVolume(percent int) {
	p.mu.Lock()
	p.gain = float64(min(100, max(0, percent))) / 100
	p.mu.Unlock()
}

// Close stops the mixer and closes the stream. It is safe to call twice.
func (p *Player) Close() error {
	p.stop.Do(func() {
		p.mu.Lock()
		p.closed = true
		running := p.running
		p.mu.Unlock()
		close(p.quit)
		if running {
			<-p.ended
		}
	})
	return nil
}

// run is the mixer goroutine: it decodes, opens and closes the stream, and
// adds voices. The stream's own goroutine pulls the mix through fill.
func (p *Player) run() {
	defer close(p.ended)
	var stream Stream
	var ticker *time.Ticker
	var tick <-chan time.Time
	closeStream := func(err error) {
		if stream != nil {
			stream.Close()
			stream = nil
		}
		if ticker != nil {
			ticker.Stop()
			ticker, tick = nil, nil
		}
		p.mu.Lock()
		p.tick = false
		voices := p.voices
		p.voices = nil
		p.mu.Unlock()
		for _, v := range voices {
			v.finish(err)
		}
	}
	for {
		select {
		case <-p.quit:
			closeStream(ErrClosed)
			for {
				select {
				case r := <-p.reqs:
					r.v.finish(ErrClosed)
				default:
					return
				}
			}
		case r := <-p.reqs:
			if p.beforeDecode != nil {
				p.beforeDecode()
			}
			pcm, err := p.decode(r)
			if err != nil {
				r.v.finish(err)
				continue
			}
			if stream == nil {
				s, err := p.opts.Endpoint.Open(p.fill)
				if err != nil {
					r.v.finish(fmt.Errorf("%w: %v", ErrUnavailable, err))
					continue
				}
				stream = s
				ticker = time.NewTicker(min(p.opts.IdleTimeout/2, 250*time.Millisecond))
				tick = ticker.C
			}
			gain := 1.0
			if r.spec != nil && r.spec.Gain > 0 {
				gain = min(1, r.spec.Gain)
			}
			r.v.pcm, r.v.gain = pcm, gain
			p.mu.Lock()
			p.tick = true
			if len(p.voices) >= MaxVoices {
				p.mu.Unlock()
				r.v.finish(ErrTooManyVoices)
				continue
			}
			p.voices = append(p.voices, r.v)
			p.lastSound = time.Now()
			p.mu.Unlock()
		case <-tick:
			if stream.Lost() {
				closeStream(ErrUnavailable)
				continue
			}
			p.mu.Lock()
			idle := len(p.voices) == 0 && time.Since(p.lastSound) >= p.opts.IdleTimeout
			p.mu.Unlock()
			if idle {
				closeStream(nil)
			}
		}
	}
}

// fill mixes every sounding voice into buf. It runs on the stream's
// goroutine and never blocks on anything but the voice list.
func (p *Player) fill(buf []int16) int {
	mix := make([]int32, len(buf))
	p.mu.Lock()
	kept := p.voices[:0]
	for _, v := range p.voices {
		if v.stopped {
			v.finish(nil)
			continue
		}
		n := min(len(buf), len(v.pcm)-v.pos)
		for i := 0; i < n; i++ {
			mix[i] += int32(float64(v.pcm[v.pos+i]) * v.gain * p.gain)
		}
		v.pos += n
		if v.pos >= len(v.pcm) {
			v.finish(nil)
			continue
		}
		kept = append(kept, v)
	}
	p.voices = kept
	if len(kept) > 0 {
		p.lastSound = time.Now()
	}
	p.mu.Unlock()
	for i, s := range mix {
		buf[i] = int16(min(32767, max(-32768, s)))
	}
	return len(buf)
}

func (p *Player) decode(r request) ([]int16, error) {
	if r.tone != nil {
		p.mu.Lock()
		pcm, ok := p.cache[*r.tone]
		p.mu.Unlock()
		if ok {
			return pcm, nil
		}
		pcm = synthesise(*r.tone)
		p.mu.Lock()
		if len(p.cache) >= maxCached {
			clear(p.cache)
		}
		p.cache[*r.tone] = pcm
		p.mu.Unlock()
		return pcm, nil
	}
	return decodeFile(r.spec.Path)
}

// started reports whether the mixer goroutine was ever started.
func (p *Player) started() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// ticking reports whether the mixer still wakes on a timer.
func (p *Player) ticking() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tick
}

func (p *Player) voiceCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.voices)
}
