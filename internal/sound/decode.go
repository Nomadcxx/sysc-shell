package sound

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/jfreymuth/oggvorbis"
)

// synthesise renders a tone in the mixer format with a short linear rise and
// fall, so it starts and stops without a click.
func synthesise(t Tone) []int16 {
	frames := int(math.Round(t.Seconds * SampleRate))
	frames = min(frames, MaxSeconds*SampleRate)
	if frames <= 0 || t.Hz <= 0 {
		return nil
	}
	to := t.To
	if to <= 0 {
		to = t.Hz
	}
	ramp := max(1, min(frames/4, SampleRate/200)) // at most 5 ms each way
	out := make([]int16, frames*Channels)
	phase := 0.0
	for i := 0; i < frames; i++ {
		hz := t.Hz + (to-t.Hz)*float64(i)/float64(frames)
		phase += 2 * math.Pi * hz / SampleRate
		env := 1.0
		if i < ramp {
			env = float64(i) / float64(ramp)
		} else if frames-1-i < ramp {
			env = float64(frames-1-i) / float64(ramp)
		}
		s := int16(math.Sin(phase) * env * 0.5 * 32767)
		out[2*i], out[2*i+1] = s, s
	}
	return out
}

// decodeFile reads a WAV or Ogg Vorbis file into the mixer format.
func decodeFile(path string) ([]int16, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sound: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > maxFileBytes {
		return nil, fmt.Errorf("sound: %s is larger than %d bytes", path, maxFileBytes)
	}
	r := bufio.NewReader(io.LimitReader(f, maxFileBytes))
	magic, err := r.Peek(4)
	if err != nil {
		return nil, fmt.Errorf("sound: %s: %w", path, err)
	}
	switch string(magic) {
	case "RIFF":
		return decodeWAV(r)
	case "OggS":
		return decodeVorbis(r)
	}
	return nil, fmt.Errorf("sound: %s is neither WAV nor Ogg Vorbis", path)
}

// decodeWAV reads PCM (8, 16, 24 or 32 bit) or 32-bit float WAV, mono or
// stereo, at any rate.
func decodeWAV(r io.Reader) ([]int16, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("sound: not a WAVE file")
	}
	var format, channels, bits int
	var rate int
	var samples []byte
	for pos := 12; pos+8 <= len(data); {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := data[pos+8:]
		if size > len(body) {
			return nil, fmt.Errorf("sound: WAV chunk %q runs past the file", id)
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("sound: short WAV format chunk")
			}
			format = int(binary.LittleEndian.Uint16(body[0:]))
			channels = int(binary.LittleEndian.Uint16(body[2:]))
			rate = int(binary.LittleEndian.Uint32(body[4:]))
			bits = int(binary.LittleEndian.Uint16(body[14:]))
		case "data":
			samples = body
		}
		pos += 8 + size + size%2
	}
	if format == 0 || samples == nil {
		return nil, errors.New("sound: WAV has no format or no data")
	}
	if channels < 1 || channels > 2 || rate <= 0 {
		return nil, fmt.Errorf("sound: WAV with %d channels at %d Hz is not supported", channels, rate)
	}
	width := bits / 8
	if (format != 1 && format != 3) || (format == 3 && bits != 32) || (format == 1 && (width < 1 || width > 4)) {
		return nil, fmt.Errorf("sound: WAV format %d at %d bits is not supported", format, bits)
	}
	frames := len(samples) / (width * channels)
	if frames > MaxSeconds*rate {
		return nil, fmt.Errorf("sound: WAV is longer than %d seconds", MaxSeconds)
	}
	pcm := make([]float32, frames*channels)
	for i := range pcm {
		b := samples[i*width:]
		switch {
		case format == 3:
			pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(b))
		case width == 1:
			pcm[i] = (float32(b[0]) - 128) / 128
		case width == 2:
			pcm[i] = float32(int16(binary.LittleEndian.Uint16(b))) / 32768
		case width == 3:
			v := int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16
			pcm[i] = float32(v) / 8388608
		case width == 4:
			pcm[i] = float32(int32(binary.LittleEndian.Uint32(b))) / 2147483648
		}
	}
	return toMixer(pcm, channels, rate), nil
}

// decodeVorbis reads Ogg Vorbis through the pinned pure-Go decoder.
func decodeVorbis(r io.Reader) ([]int16, error) {
	pcm, format, err := oggvorbis.ReadAll(io.LimitReader(r, maxFileBytes))
	if err != nil {
		return nil, fmt.Errorf("sound: vorbis: %w", err)
	}
	if format.Channels < 1 || format.Channels > 2 || format.SampleRate <= 0 {
		return nil, fmt.Errorf("sound: vorbis with %d channels at %d Hz is not supported", format.Channels, format.SampleRate)
	}
	if len(pcm)/format.Channels > MaxSeconds*format.SampleRate {
		return nil, fmt.Errorf("sound: vorbis is longer than %d seconds", MaxSeconds)
	}
	return toMixer(pcm, format.Channels, format.SampleRate), nil
}

// toMixer converts interleaved float samples to the mixer's stereo 48 kHz
// 16-bit format, resampling linearly.
func toMixer(pcm []float32, channels, rate int) []int16 {
	in := len(pcm) / channels
	out := int(int64(in) * SampleRate / int64(rate))
	dst := make([]int16, out*Channels)
	at := func(frame, ch int) float64 {
		frame = min(frame, in-1)
		return float64(pcm[frame*channels+min(ch, channels-1)])
	}
	for i := 0; i < out; i++ {
		src := float64(i) * float64(rate) / SampleRate
		j := int(src)
		frac := src - float64(j)
		for ch := 0; ch < Channels; ch++ {
			v := at(j, ch)*(1-frac) + at(j+1, ch)*frac
			dst[i*Channels+ch] = int16(max(-1, min(1, v)) * 32767)
		}
	}
	return dst
}
