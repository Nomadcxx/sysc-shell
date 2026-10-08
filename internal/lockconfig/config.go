// Package lockconfig edits the locker-owned presentation file; idle policy stays in shell config.
package lockconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"unicode/utf8"

	"github.com/Nomadcxx/sysc-terminal/renderer"
)

const MaxBytes = 64 << 10

const EffectNone = "none"

// atomicReplace follows shell config writing, with a test seam for rename failure.
var atomicReplace = os.Rename

type Config struct {
	Header             string  `json:"header"`
	TextEffect         string  `json:"text_effect"`
	Effect             string  `json:"effect"`
	Palette            string  `json:"palette"`
	ClockStyle         string  `json:"clock_style"`
	ReducedMotion      bool    `json:"reduced_motion"`
	Clock24h           bool    `json:"clock_24h"`
	EffectFPS          int     `json:"effect_fps"`
	Blur               *bool   `json:"blur_backdrop"`
	EffectBackend      *string `json:"effect_backend"`
	EffectGpuPowerSave *bool   `json:"effect_gpu_power_save"`
}

func Default() Config {
	return Config{Header: "ascii_1", TextEffect: "none", Effect: EffectNone, Palette: "nord", ClockStyle: DefaultClockStyle, EffectFPS: 20}
}

// Effects includes the locker's static presentation alongside renderer effects.
func Effects() []string { return append([]string{EffectNone}, renderer.Effects()...) }

func TextEffects() []string { return append([]string{EffectNone}, renderer.TextEffects()...) }

func (c Config) Validate() error {
	if err := Validate(c.Effect, c.Palette); err != nil {
		return err
	}
	if !slices.Contains(TextEffects(), c.TextEffect) {
		return fmt.Errorf("unknown text effect %q", c.TextEffect)
	}
	return nil
}

// Validate follows the locker: none needs a valid palette, without an effect.
func Validate(effect, palette string) error {
	if effect == EffectNone {
		if !slices.Contains(renderer.Palettes(), palette) {
			return fmt.Errorf("unknown palette %q", palette)
		}
		return nil
	}
	return renderer.Validate(effect, palette)
}

// DefaultClockStyle and ClockStyles mirror sysc-lock internal/art, whose glyph
// tables the shell cannot import; the locker owns the names.
const DefaultClockStyle = "kompaktblk"

var clockStyles = []string{DefaultClockStyle, "phm_blocky_reverse", "phmvga", "phm_slanted", "plain"}

// ClockStyles returns the clock styles the locker can render.
func ClockStyles() []string { return append([]string(nil), clockStyles...) }

// ClockStyle resolves a name the way the locker's art.Lookup does: an unknown
// or empty name falls back to the default rather than failing.
func ClockStyle(name string) string {
	for _, style := range clockStyles {
		if style == name {
			return name
		}
	}
	return DefaultClockStyle
}

// Backend, GpuPowerSave and BlurBackdrop mirror the sysc-lock accessors:
// unset means the locker default, never a shell opinion.
func (c Config) Backend() string {
	if c.EffectBackend == nil {
		return "auto"
	}
	switch *c.EffectBackend {
	case "cpu", "gpu", "auto":
		return *c.EffectBackend
	}
	return "auto"
}

func (c Config) GpuPowerSave() bool { return c.EffectGpuPowerSave == nil || *c.EffectGpuPowerSave }

func (c Config) BlurBackdrop() bool { return c.Blur == nil || *c.Blur }
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "sysc-lock", "config.json")
}
func HeadersPath() string {
	path := Path()
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "headers.conf")
}

// ReadHeaders transports literal bounded artwork; only sysc-lock parses it.
func ReadHeaders(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() > MaxBytes {
		return "", fmt.Errorf("headers.conf exceeds regular file budget")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return "", fmt.Errorf("headers.conf exceeds byte budget")
	}
	return string(data), nil
}

func read(path string) (map[string]json.RawMessage, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if os.IsNotExist(err) {
		return make(map[string]json.RawMessage), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return nil, fmt.Errorf("lock config exceeds file budget")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("lock config exceeds file budget")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("lock config must be an object")
	}
	return fields, nil
}
func Load(path string) (Config, error) {
	c := Default()
	fields, err := read(path)
	if err != nil {
		return c, err
	}
	for _, key := range []string{"effect", "palette", "reduced_motion"} {
		if value, ok := fields[key]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return c, fmt.Errorf("null lock setting %s", key)
		}
	}
	data, _ := json.Marshal(fields)
	if err = json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	// ponytail: 20/10/120 mirror sysc-lock internal/config; the locker owns
	// the single source, revisit if these two files ever drift.
	if fields["effect_fps"] == nil || c.EffectFPS == 0 {
		c.EffectFPS = 20
	} else {
		c.EffectFPS = max(10, min(120, c.EffectFPS))
	}
	c.ClockStyle = ClockStyle(c.ClockStyle)
	return c, c.Validate()
}
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	fields, err := read(path)
	if err != nil {
		return err
	}
	known, _ := json.Marshal(c)
	var patch map[string]json.RawMessage
	_ = json.Unmarshal(known, &patch)
	for key, value := range patch {
		fields[key] = value
	}
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > MaxBytes {
		return fmt.Errorf("lock config exceeds file budget")
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".config-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = atomicReplace(f.Name(), path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
