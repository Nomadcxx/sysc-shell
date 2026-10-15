package sound

import (
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// The stream's identity, so a user can find the shell's sounds in any mixer
// and mute them, and so nothing mistakes them for media.
const (
	ApplicationName = "sysc-shell"
	MediaName       = "sysc-shell event sounds"
	// MediaRole marks the stream as event sounds: the role desktop mixers
	// group notification sounds under, never a media player.
	MediaRole = "event"
	iconName  = "preferences-desktop-notification"
)

// PulseEndpoint opens streams on the session's PulseAudio-protocol server,
// which PipeWire provides through pipewire-pulse. It connects on Open and
// disconnects on Close, so nothing stays connected while the shell is silent.
type PulseEndpoint struct{}

func (PulseEndpoint) Open(fill func([]int16) int) (Stream, error) {
	client, err := pulse.NewClient(
		pulse.ClientApplicationName(ApplicationName),
		pulse.ClientApplicationIconName(iconName),
		pulse.ClientTimeout(2*time.Second),
	)
	if err != nil {
		return nil, err
	}
	stream, err := client.NewPlayback(
		pulse.Int16Reader(func(buf []int16) (int, error) { return fill(buf), nil }),
		pulse.PlaybackStereo,
		pulse.PlaybackSampleRate(SampleRate),
		pulse.PlaybackLatency(0.05),
		pulse.PlaybackMediaName(MediaName),
		pulse.PlaybackMediaIconName(iconName),
		pulse.PlaybackRawOption(func(r *proto.CreatePlaybackStream) {
			r.Properties["media.role"] = proto.PropListString(MediaRole)
		}),
	)
	if err != nil {
		client.Close()
		return nil, err
	}
	stream.Start()
	return &pulseStream{client: client, stream: stream}, nil
}

type pulseStream struct {
	client *pulse.Client
	stream *pulse.PlaybackStream
}

func (s *pulseStream) Close() {
	s.stream.Close()
	s.client.Close()
}

func (s *pulseStream) Lost() bool { return s.stream.Closed() || s.stream.Error() != nil }

// NotificationCue is the one earcon the shell plays: a short rising tone,
// quiet enough to sit under whatever is already playing.
var NotificationCue = Tone{Hz: 880, To: 1320, Seconds: 0.12}
