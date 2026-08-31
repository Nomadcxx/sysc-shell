package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAudioParsesWpctlVolume(t *testing.T) {
	t.Parallel()
	level, muted, err := parseWpctlVolume("Volume: 0.42")
	if err != nil || level != 42 || muted {
		t.Fatalf("0.42 -> %d muted=%v err=%v", level, muted, err)
	}
	level, muted, err = parseWpctlVolume("Volume: 1.00 [MUTED]")
	if err != nil || level != 100 || !muted {
		t.Fatalf("muted -> %d muted=%v err=%v", level, muted, err)
	}
}

func TestAudioChangeEventsIncludeExternal(t *testing.T) {
	t.Parallel()
	fake := fakeWpctl(t, "0.40")
	a := NewAudio(10*time.Millisecond, fake.path)
	t.Cleanup(a.Close)
	l, err := a.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	fake.set("0.55")
	ev := waitForAudio(t, a)
	if ev.Level != 55 {
		t.Fatalf("level = %d, want 55", ev.Level)
	}
	l.Release()
}

func TestAudioUnavailableWithoutWpctl(t *testing.T) {
	t.Parallel()
	a := NewAudio(time.Second, "/nonexistent/wpctl")
	if a.Available() {
		t.Fatal("must be unavailable")
	}
}

func TestAudioSetInvokesWpctl(t *testing.T) {
	t.Parallel()
	fake := fakeWpctl(t, "0.30")
	a := NewAudio(time.Second, fake.path)
	if err := a.Set(30); err != nil {
		t.Fatal(err)
	}
	if err := a.SetMute(true); err != nil {
		t.Fatal(err)
	}
	fake.expect(t, "set-volume", "@DEFAULT_AUDIO_SINK@", "30%")
	fake.expect(t, "set-volume", "@DEFAULT_AUDIO_SINK@", "mute")
}

type fakeCmd struct {
	path string
	dir  string
}

func fakeWpctl(t *testing.T, vol string) *fakeCmd {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vol"), []byte(vol+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
log="` + dir + `/log"
volf="` + dir + `/vol"
cmd="$1"
shift
echo "$cmd $*" >> "$log"
if [ "$cmd" = get-volume ]; then
  printf 'Volume: %s\n' "$(cat "$volf")"
fi
`
	path := filepath.Join(dir, "wpctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeCmd{path: path, dir: dir}
}

func (f *fakeCmd) set(vol string) {
	_ = os.WriteFile(filepath.Join(f.dir, "vol"), []byte(vol+"\n"), 0o600)
}

func (f *fakeCmd) expect(t *testing.T, args ...string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "log"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(args, " ")
	if !strings.Contains(string(raw), want) {
		t.Fatalf("log %q does not contain %q", raw, want)
	}
}

func waitForAudio(t *testing.T, a *Audio) AudioState {
	t.Helper()
	select {
	case ev := <-a.Changes():
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for audio change")
		return AudioState{}
	}
}
