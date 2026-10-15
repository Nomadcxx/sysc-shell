package config

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestConfigWatchEventParsing(t *testing.T) {
	const wd = int32(7)
	for _, tt := range []struct {
		name       string
		records    [][]byte
		wantNotify bool
		wantResync bool
		wantLost   bool
		wantErr    bool
	}{
		{name: "close write", records: [][]byte{configWatchRecord(wd, unix.IN_CLOSE_WRITE, "config.json")}, wantNotify: true},
		{name: "atomic replace", records: [][]byte{configWatchRecord(wd, unix.IN_MOVED_TO, "config.json")}, wantNotify: true},
		{name: "delete", records: [][]byte{configWatchRecord(wd, unix.IN_DELETE, "config.json")}, wantNotify: true},
		{name: "move away", records: [][]byte{configWatchRecord(wd, unix.IN_MOVED_FROM, "config.json")}, wantNotify: true},
		{name: "irrelevant basename", records: [][]byte{configWatchRecord(wd, unix.IN_CLOSE_WRITE, ".editor-temp")}},
		{name: "overflow", records: [][]byte{configWatchRecord(-1, unix.IN_Q_OVERFLOW, "")}, wantNotify: true, wantResync: true},
		{name: "lost descriptor", records: [][]byte{configWatchRecord(wd, unix.IN_IGNORED, "")}, wantLost: true},
		{name: "truncated header", records: [][]byte{make([]byte, unix.SizeofInotifyEvent-1)}, wantErr: true},
		{name: "length exceeds remaining", records: [][]byte{configWatchInvalidLength(wd, unix.IN_CLOSE_WRITE, "config.json")}, wantErr: true},
		{name: "zero name length", records: [][]byte{configWatchRecord(wd, unix.IN_CLOSE_WRITE, "")}, wantErr: true},
		{name: "empty name", records: [][]byte{configWatchEmptyName(wd, unix.IN_CLOSE_WRITE)}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, record := range tt.records {
				events, err := parseConfigEvents(record, wd, "config.json")
				if (err != nil) != tt.wantErr {
					t.Fatalf("parse error = %v, wantErr %v", err, tt.wantErr)
				}
				if err != nil {
					return
				}
				if events.notify != tt.wantNotify || events.resync != tt.wantResync || events.lost != tt.wantLost {
					t.Fatalf("events = %+v", events)
				}
			}
		})
	}
}

func TestWatchCatchesAtomicReplaceAndStops(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notified := make(chan struct{}, 8)
	done := make(chan error, 1)
	watch, err := newConfigWatch(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() { done <- watch.run(ctx, func() { notified <- struct{}{} }) }()
	temp := filepath.Join(dir, ".editor-temp")
	if err := os.WriteFile(temp, []byte("{\"bar\":{\"height\":50}}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("rename-over save missed")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch shutdown blocked")
	}
}

func TestWatchDebouncesMatchingSavesAndIgnoresOtherNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notified := make(chan struct{}, 8)
	done := runConfigWatch(t, ctx, path, notified)
	if err := os.WriteFile(filepath.Join(dir, ".editor-temp"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notified:
		t.Fatal("unrelated temporary file triggered reload")
	case <-time.After(200 * time.Millisecond):
	}
	for i := range 5 {
		if err := os.WriteFile(path, []byte{byte('0' + i)}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("matching write burst did not trigger reload")
	}
	select {
	case <-notified:
		t.Fatal("matching write burst triggered more than one reload")
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	awaitConfigWatch(t, done)
}

func TestWatchCancellationDuringDebounceSkipsNotification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	notified := make(chan struct{}, 1)
	done := runConfigWatch(t, ctx, path, notified)
	if err := os.WriteFile(path, []byte("{\"bar\":{}}"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	awaitConfigWatch(t, done)
	select {
	case <-notified:
		t.Fatal("canceled pending debounce notified")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestWatchRejectsInvalidInputsAndLostDirectory(t *testing.T) {
	if err := Watch(context.Background(), "", func() {}); err == nil {
		t.Fatal("empty path accepted")
	}
	if err := Watch(context.Background(), "/tmp/config.json", nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	if _, err := newConfigWatch(filepath.Join(t.TempDir(), "missing", "config.json")); err == nil {
		t.Fatal("missing config directory accepted")
	}

	parent := t.TempDir()
	dir := filepath.Join(parent, "watched")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := newConfigWatch(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- watch.run(ctx, func() {}) }()
	if err := os.Rename(dir, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("moved watched directory returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("moved directory was not detected")
	}
}

func TestWatchCancellationDoesNotLeakDescriptors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	warmCtx, warmCancel := context.WithCancel(context.Background())
	warm, err := newConfigWatch(path)
	if err != nil {
		t.Fatal(err)
	}
	warmDone := make(chan error, 1)
	go func() { warmDone <- warm.run(warmCtx, func() {}) }()
	warmCancel()
	awaitConfigWatch(t, warmDone)
	baseline := configWatchFDCount(t)
	for range 25 {
		ctx, cancel := context.WithCancel(context.Background())
		watch, err := newConfigWatch(path)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- watch.run(ctx, func() {}) }()
		cancel()
		awaitConfigWatch(t, done)
	}
	if got := configWatchFDCount(t); got != baseline {
		t.Fatalf("fd count after watch cycles = %d, before = %d", got, baseline)
	}
}

func runConfigWatch(t *testing.T, ctx context.Context, path string, notified chan<- struct{}) <-chan error {
	t.Helper()
	watch, err := newConfigWatch(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- watch.run(ctx, func() { notified <- struct{}{} }) }()
	return done
}

func awaitConfigWatch(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch shutdown blocked")
	}
}

func configWatchFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func configWatchRecord(wd int32, mask uint32, name string) []byte {
	length := uint32(0)
	if name != "" {
		length = uint32((len(name) + 1 + 3) &^ 3)
	}
	data := make([]byte, unix.SizeofInotifyEvent+int(length))
	binary.NativeEndian.PutUint32(data[0:4], uint32(wd))
	binary.NativeEndian.PutUint32(data[4:8], mask)
	binary.NativeEndian.PutUint32(data[12:16], length)
	copy(data[unix.SizeofInotifyEvent:], name)
	return data
}

func configWatchInvalidLength(wd int32, mask uint32, name string) []byte {
	data := configWatchRecord(wd, mask, name)
	binary.NativeEndian.PutUint32(data[12:16], uint32(len(data)))
	return data
}

func configWatchEmptyName(wd int32, mask uint32) []byte {
	data := make([]byte, unix.SizeofInotifyEvent+4)
	binary.NativeEndian.PutUint32(data[0:4], uint32(wd))
	binary.NativeEndian.PutUint32(data[4:8], mask)
	binary.NativeEndian.PutUint32(data[12:16], 4)
	return data
}
