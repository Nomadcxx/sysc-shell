package shell

import (
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-notify/protocol"
	"github.com/Nomadcxx/sysc-shell/internal/config"
	"github.com/Nomadcxx/sysc-shell/internal/sound"
)

// countingPlayer counts cues; it never makes a sound.
type countingPlayer struct {
	mu     sync.Mutex
	tones  []sound.Tone
	volume int
	closed bool
}

func (p *countingPlayer) Tone(t sound.Tone) (*sound.Voice, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tones = append(p.tones, t)
	return nil, nil
}

func (p *countingPlayer) SetVolume(v int) {
	p.mu.Lock()
	p.volume = v
	p.mu.Unlock()
}

func (p *countingPlayer) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return nil
}

func (p *countingPlayer) cues() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.tones)
}

func soundRegistry(t *testing.T, on bool) (*Registry, *countingPlayer, *int) {
	t.Helper()
	cfg := config.Default()
	cfg.Sounds = config.Sounds{Notification: on, Volume: 35}
	r := NewRegistry(cfg)
	t.Cleanup(r.Close)
	player := &countingPlayer{}
	made := 0
	r.newSoundPlayer = func() cuePlayer { made++; return player }
	r.applyNotify(snap(1))
	return r, player, &made
}

func added(id uint32) protocol.Delta {
	return protocol.Delta{Kind: protocol.DeltaAdded, Notification: ptr(note(id, "hello")),
		Lifetime: &protocol.Lifetime{ID: id, DurationMS: 5000, RemainingMS: 5000, Running: true}}
}

func TestANotificationArrivalPlaysExactlyOneCue(t *testing.T) {
	r, player, _ := soundRegistry(t, true)
	r.applyNotify(delta(1, 2, added(7)))
	if got := player.cues(); got != 1 {
		t.Fatalf("an arrival played %d cues, want 1", got)
	}
	if player.tones[0] != sound.NotificationCue || player.volume != 35 {
		t.Fatalf("cue %+v at %d, want the notification cue at the configured volume", player.tones[0], player.volume)
	}

	// Nothing else that changes the projection is an arrival.
	replacement := note(7, "hello again")
	for _, d := range []protocol.Delta{
		{Kind: protocol.DeltaReplaced, Notification: &replacement},
		{Kind: protocol.DeltaClosed, ID: 7},
		{Kind: protocol.DeltaHistoryCleared},
	} {
		r.applyNotify(delta(1, 3, d))
	}
	r.applyNotify(delta(99, 4, added(8))) // a stale generation
	r.applyNotify(snap(5, note(9, "from a snapshot")))
	if got := player.cues(); got != 1 {
		t.Fatalf("after replacing, closing, clearing, a stale delta and a snapshot: %d cues, want 1", got)
	}
}

func TestDoNotDisturbSilencesTheCue(t *testing.T) {
	r, player, _ := soundRegistry(t, true)
	r.setDND(true)
	r.applyNotify(delta(1, 2, added(7)))
	if got := player.cues(); got != 0 {
		t.Fatalf("do not disturb let %d cues through", got)
	}
}

func TestWithSoundsOffNoPlayerIsEverMade(t *testing.T) {
	r, player, made := soundRegistry(t, false)
	r.applyNotify(delta(1, 2, added(7)))
	if *made != 0 || player.cues() != 0 {
		t.Fatalf("sounds off: made %d players, played %d cues", *made, player.cues())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sound != nil {
		t.Fatal("sounds off left a player")
	}
}

func TestTurningSoundsOffClosesThePlayer(t *testing.T) {
	r, player, _ := soundRegistry(t, true)
	r.applyNotify(delta(1, 2, added(7)))
	r.mu.Lock()
	cfg := r.cfg
	cfg.Sounds.Notification = false
	r.cfg = cfg
	r.syncSoundLocked()
	gone := r.sound == nil
	r.mu.Unlock()
	if !gone {
		t.Fatal("turning sounds off kept the player")
	}
	eventuallyTrue(t, func() bool { player.mu.Lock(); defer player.mu.Unlock(); return player.closed })
}

func eventuallyTrue(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(asyncWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const asyncWait = 5 * time.Second
