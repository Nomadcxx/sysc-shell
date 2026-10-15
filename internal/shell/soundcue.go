package shell

import (
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/sound"
)

// cuePlayer is the part of sound.Player the shell uses, so a test can count
// cues without an audio server.
type cuePlayer interface {
	Tone(sound.Tone) (*sound.Voice, error)
	SetVolume(percent int)
	Close() error
}

func newSystemSoundPlayer() cuePlayer {
	return sound.New(sound.Options{Enabled: true, Endpoint: sound.PulseEndpoint{}})
}

// cueNotificationLocked plays the one earcon the shell has: a notification
// arriving while sounds are on and do not disturb is off. The player is made
// on the first cue, so a shell with sounds off never has one, and a cue
// queues and returns at once, so the arrival path never waits on audio. A cue
// that cannot play is silent; nothing is shown. Caller holds r.mu.
func (r *Registry) cueNotificationLocked() {
	if !r.cfg.Sounds.Notification {
		return
	}
	if _, dnd := r.notify.dndState(time.Now()); dnd {
		return
	}
	if r.sound == nil {
		r.sound = r.newSoundPlayer()
	}
	r.sound.SetVolume(r.cfg.Sounds.Volume)
	_, _ = r.sound.Tone(sound.NotificationCue)
}

// syncSoundLocked lets go of the player when sounds are turned off, so off
// holds no goroutine and no stream. Its close runs off the lock. Caller holds
// r.mu.
func (r *Registry) syncSoundLocked() {
	if r.sound == nil || r.cfg.Sounds.Notification {
		return
	}
	player := r.sound
	r.sound = nil
	go func() { _ = player.Close() }()
}
