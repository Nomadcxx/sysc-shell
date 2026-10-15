package sound

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEndpoint stands in for the audio server. No test opens a real one, and
// nothing here makes a sound: samples are pulled by the test.
type fakeEndpoint struct {
	mu      sync.Mutex
	opens   int
	closes  int
	fill    func([]int16) int
	openErr error
	block   chan struct{}
	lost    bool
}

func (f *fakeEndpoint) Open(fill func([]int16) int) (Stream, error) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	if f.openErr != nil {
		return nil, f.openErr
	}
	f.fill = fill
	return &fakeStream{f}, nil
}

func (f *fakeEndpoint) counts() (opens, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens, f.closes
}

// pull asks the mixer for n stereo frames, as the server's stream would.
func (f *fakeEndpoint) pull(n int) []int16 {
	f.mu.Lock()
	fill := f.fill
	f.mu.Unlock()
	buf := make([]int16, 2*n)
	if fill != nil {
		fill(buf)
	}
	return buf
}

type fakeStream struct{ f *fakeEndpoint }

func (s *fakeStream) Close() {
	s.f.mu.Lock()
	s.f.closes++
	s.f.mu.Unlock()
}

func (s *fakeStream) Lost() bool {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	return s.f.lost
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func peak(buf []int16) int {
	m := 0
	for _, s := range buf {
		m = max(m, int(math.Abs(float64(s))))
	}
	return m
}

var cue = Tone{Hz: 880, Seconds: 0.05}

func TestADisabledPlayerStartsNothing(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: false, Endpoint: ep})
	defer p.Close()
	if _, err := p.Tone(cue); !errors.Is(err, ErrDisabled) {
		t.Fatalf("tone on a disabled player: %v", err)
	}
	if p.started() {
		t.Fatal("a disabled player started its mixer")
	}
	if opens, _ := ep.counts(); opens != 0 {
		t.Fatal("a disabled player opened a stream")
	}
}

func TestTheStreamOpensOnTheFirstCueAndClosesWhenIdle(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep, IdleTimeout: 30 * time.Millisecond})
	defer p.Close()
	if p.started() {
		t.Fatal("an enabled player started its mixer before any cue")
	}
	if opens, _ := ep.counts(); opens != 0 {
		t.Fatal("a stream was opened before any cue")
	}
	v, err := p.Tone(cue)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stream opened", func() bool { o, _ := ep.counts(); return o == 1 })
	eventually(t, "the voice sounding", func() bool { return peak(ep.pull(480)) > 0 })
	ep.pull(48000) // play the rest out
	<-v.Done()
	eventually(t, "the idle stream closed", func() bool { _, c := ep.counts(); return c == 1 })
	eventually(t, "the mixer stopped waking", func() bool { return !p.ticking() })
}

func TestASecondCueMixesIntoTheSameStream(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep})
	defer p.Close()
	long := Tone{Hz: 440, Seconds: 1}
	if _, err := p.Tone(long); err != nil {
		t.Fatal(err)
	}
	eventually(t, "one voice", func() bool { return p.voiceCount() == 1 })
	one := peak(ep.pull(4800))
	if _, err := p.Tone(long); err != nil {
		t.Fatal(err)
	}
	eventually(t, "two voices", func() bool { return p.voiceCount() == 2 })
	if opens, _ := ep.counts(); opens != 1 {
		t.Fatalf("opened %d streams for two cues", opens)
	}
	if two := peak(ep.pull(4800)); two <= one {
		t.Fatalf("two voices peak %d, one voice %d: the second did not mix", two, one)
	}
}

func TestVoicesAreCappedNotQueued(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep})
	defer p.Close()
	var voices []*Voice
	for i := 0; i < MaxVoices+1; i++ {
		v, err := p.Tone(Tone{Hz: 440, Seconds: 2})
		if err != nil {
			t.Fatal(err)
		}
		voices = append(voices, v)
		// One at a time, so this measures the voice cap, not the queue.
		eventually(t, "the cue reached the mixer", func() bool { return p.voiceCount() == min(i+1, MaxVoices) || v.Err() != nil })
	}
	last := voices[MaxVoices]
	<-last.Done()
	if !errors.Is(last.Err(), ErrTooManyVoices) {
		t.Fatalf("the cue past the cap: %v", last.Err())
	}
	if n := p.voiceCount(); n != MaxVoices {
		t.Fatalf("%d voices sounding, cap is %d", n, MaxVoices)
	}
}

func TestStopSilencesAVoice(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep})
	defer p.Close()
	v, _ := p.Tone(Tone{Hz: 440, Seconds: 2})
	eventually(t, "a voice", func() bool { return p.voiceCount() == 1 })
	p.Stop(v)
	if got := peak(ep.pull(4800)); got != 0 {
		t.Fatalf("a stopped voice still writes peak %d", got)
	}
	<-v.Done()
}

func TestACueNeverWaitsForTheServer(t *testing.T) {
	ep := &fakeEndpoint{block: make(chan struct{})}
	p := New(Options{Enabled: true, Endpoint: ep})
	defer func() { close(ep.block); p.Close() }()
	returned := make(chan struct{})
	go func() {
		_, _ = p.Tone(cue)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Tone blocked on a server that does not answer")
	}
	// With the mixer held in Open, the queue fills and then refuses.
	var err error
	for i := 0; i < queueDepth+2 && err == nil; i++ {
		_, err = p.Tone(cue)
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("a full queue answered %v, want %v", err, ErrBusy)
	}
}

func TestDecodingHappensOffTheCallersGoroutine(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep})
	defer p.Close()
	release := make(chan struct{})
	decoding := make(chan struct{}, 1)
	p.beforeDecode = func() {
		decoding <- struct{}{}
		<-release
	}
	if _, err := p.Tone(cue); err != nil {
		t.Fatal(err)
	}
	// Tone returned while the decode it caused is still held.
	select {
	case <-decoding:
	case <-time.After(5 * time.Second):
		t.Fatal("the cue was never decoded")
	}
	close(release)
}

func TestAMissingServerIsSilenceAndOneTryPerCue(t *testing.T) {
	ep := &fakeEndpoint{openErr: errors.New("connection refused")}
	p := New(Options{Enabled: true, Endpoint: ep})
	defer p.Close()
	for i := 1; i <= 2; i++ {
		v, err := p.Tone(cue)
		if err != nil {
			t.Fatal(err)
		}
		<-v.Done()
		if !errors.Is(v.Err(), ErrUnavailable) {
			t.Fatalf("cue %d: %v, want %v", i, v.Err(), ErrUnavailable)
		}
		if opens, _ := ep.counts(); opens != i {
			t.Fatalf("after %d cues the server was tried %d times", i, opens)
		}
	}
}

func TestALostStreamIsDroppedAndReopenedOnTheNextCue(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep, IdleTimeout: time.Hour})
	defer p.Close()
	if _, err := p.Tone(Tone{Hz: 440, Seconds: 2}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "opened", func() bool { o, _ := ep.counts(); return o == 1 })
	ep.mu.Lock()
	ep.lost = true
	ep.mu.Unlock()
	eventually(t, "the lost stream dropped", func() bool { _, c := ep.counts(); return c == 1 })
	ep.mu.Lock()
	ep.lost = false
	ep.mu.Unlock()
	if _, err := p.Tone(cue); err != nil {
		t.Fatal(err)
	}
	eventually(t, "reopened once", func() bool { o, _ := ep.counts(); return o == 2 })
}

func TestCloseIsIdempotentAndClosesTheStream(t *testing.T) {
	ep := &fakeEndpoint{}
	p := New(Options{Enabled: true, Endpoint: ep, IdleTimeout: time.Hour})
	if _, err := p.Tone(Tone{Hz: 440, Seconds: 2}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "opened", func() bool { o, _ := ep.counts(); return o == 1 })
	done := make(chan struct{})
	go func() { p.Close(); p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	if _, c := ep.counts(); c != 1 {
		t.Fatalf("closed the stream %d times", c)
	}
	if _, err := p.Tone(cue); !errors.Is(err, ErrClosed) {
		t.Fatalf("a cue after Close: %v", err)
	}
}

func TestToneHasTheExpectedLengthAndEnvelope(t *testing.T) {
	pcm := synthesise(Tone{Hz: 880, Seconds: 0.02})
	if frames := len(pcm) / 2; frames != 960 {
		t.Fatalf("frames = %d, want 960", frames)
	}
	mid := peak(pcm[len(pcm)/2-100 : len(pcm)/2+100])
	if head, tail := peak(pcm[:8]), peak(pcm[len(pcm)-8:]); head*4 >= mid || tail*4 >= mid {
		t.Fatalf("envelope: head %d, middle %d, tail %d; it must rise and fall", head, mid, tail)
	}
}

// wavFile builds a WAV in memory: format 1 (PCM) or 3 (float).
func wavFile(format, channels, rate, bits int, samples []float64) []byte {
	var data bytes.Buffer
	for _, s := range samples {
		switch {
		case format == 3:
			binary.Write(&data, binary.LittleEndian, float32(s))
		case bits == 8:
			data.WriteByte(byte(int(s*127) + 128))
		case bits == 16:
			binary.Write(&data, binary.LittleEndian, int16(s*32767))
		case bits == 24:
			v := int32(s * 8388607)
			data.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16)})
		case bits == 32:
			binary.Write(&data, binary.LittleEndian, int32(s*2147483647))
		}
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(format))
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*channels*bits/8))
	binary.Write(&b, binary.LittleEndian, uint16(channels*bits/8))
	binary.Write(&b, binary.LittleEndian, uint16(bits))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data.Len()))
	b.Write(data.Bytes())
	return b.Bytes()
}

func TestWAVDecodesEveryDepthToTheMixerFormat(t *testing.T) {
	half := []float64{0.5, -0.5, 0.5, -0.5}
	for _, tc := range []struct {
		name                         string
		format, channels, rate, bits int
		frames                       int
	}{
		{"8-bit mono", 1, 1, 48000, 8, 4},
		{"16-bit stereo", 1, 2, 48000, 16, 2},
		{"24-bit mono", 1, 1, 48000, 24, 4},
		{"32-bit mono", 1, 1, 48000, 32, 4},
		{"float mono", 3, 1, 48000, 32, 4},
		{"16-bit mono at 24 kHz", 1, 1, 24000, 16, 8},
	} {
		pcm, err := decodeWAV(bytes.NewReader(wavFile(tc.format, tc.channels, tc.rate, tc.bits, half)))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := len(pcm) / 2; got != tc.frames {
			t.Errorf("%s: %d stereo frames, want %d", tc.name, got, tc.frames)
		}
		if p := peak(pcm); p < 15000 || p > 17000 {
			t.Errorf("%s: peak %d, want about half scale", tc.name, p)
		}
	}
}

func TestWAVRefusesWhatItCannotTrust(t *testing.T) {
	good := wavFile(1, 1, 48000, 16, []float64{0.1})
	if _, err := decodeWAV(bytes.NewReader(good[:20])); err == nil {
		t.Error("a truncated header decoded")
	}
	if _, err := decodeWAV(bytes.NewReader([]byte("RIFF\x00\x00\x00\x00AVI LIST"))); err == nil {
		t.Error("a non-WAVE file decoded")
	}
	long := make([]float64, (MaxSeconds+1)*8000)
	if _, err := decodeWAV(bytes.NewReader(wavFile(1, 1, 8000, 16, long))); err == nil || !strings.Contains(err.Error(), "longer") {
		t.Errorf("an over-long file: %v", err)
	}
}

func TestVorbisDecodesTheFixture(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "tone-440-100ms.ogg"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pcm, err := decodeVorbis(f)
	if err != nil {
		t.Fatal(err)
	}
	// 0.1 s at 48 kHz; an encoder may pad the final packet slightly.
	if frames := len(pcm) / 2; frames < 4800 || frames > 4800+1024 {
		t.Fatalf("%d frames, want about 4800", frames)
	}
	if peak(pcm) == 0 {
		t.Fatal("decoded silence")
	}
}

func TestThePackageHasNoUserVisibleFailurePath(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"internal/shell", "notifyclient", "osd", "slog.Error"} {
			if bytes.Contains(src, []byte(forbidden)) {
				t.Errorf("%s mentions %q: a failed cue must be silent", e.Name(), forbidden)
			}
		}
	}
}
