package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// StateFileName is the single JSON object holding one plugin's persistent
// state. One file per plugin, rather than one per key, is what makes a write
// atomic: a plugin that stores two related values can never be restarted into
// a state where one landed and the other did not.
const StateFileName = "state.json"

// The persistent-state ceilings.
const (
	// MaxStateValueBytes bounds one encoded value.
	MaxStateValueBytes = 256 << 10
	// MaxStateTotalBytes bounds one plugin's whole file.
	MaxStateTotalBytes = 4 << 20
	// MaxStateKeys bounds how many keys one plugin may hold. Size alone would
	// not stop a plugin from filling the file with empty keys.
	MaxStateKeys = 256
)

// Store is one plugin's namespaced persistent key/value store.
//
// It is for state that must survive a restart: the zones a user chose, a
// running countdown's deadline. It is deliberately small and slow. A plugin
// keeps transient high-rate state in its own memory, because every write here
// costs a file replacement and a sync.
//
// The whole document is held in memory, so a read needs no I/O and cannot
// block the caller that is answering a plugin.
type Store struct {
	path string

	mu     sync.Mutex
	values map[string]json.RawMessage
}

// StateRoot is $XDG_STATE_HOME/sysc-shell/plugins, falling back to
// $HOME/.local/state as the specification requires.
func StateRoot() string {
	base := os.Getenv("XDG_STATE_HOME")
	// The specification requires an absolute path; a relative one would
	// resolve against whatever directory the shell happened to start in.
	if !filepath.IsAbs(base) {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "sysc-shell", "plugins")
}

// OpenStore loads one plugin's store from under root.
//
// A missing store is empty rather than an error: a plugin's first run has
// nothing saved. A store that exists but cannot be read is an error, because
// silently starting a plugin with a blank slate would let one corrupt file
// destroy state the user still has on disk.
func OpenStore(root, pluginID string) (*Store, error) {
	if !v1.ValidPluginID(pluginID) {
		return nil, fmt.Errorf("plugin: %q is not a plugin id", pluginID)
	}
	s := &Store{
		path:   filepath.Join(root, pluginID, StateFileName),
		values: make(map[string]json.RawMessage),
	}

	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plugin: open state for %s: %w", pluginID, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxStateTotalBytes+1))
	if err != nil {
		return nil, fmt.Errorf("plugin: read state for %s: %w", pluginID, err)
	}
	if len(data) > MaxStateTotalBytes {
		return nil, fmt.Errorf("plugin: state for %s is larger than the %d byte limit", pluginID, MaxStateTotalBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&s.values); err != nil {
		return nil, fmt.Errorf("plugin: state for %s is malformed: %w", pluginID, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("plugin: state for %s holds more than one JSON value", pluginID)
	}
	for key := range s.values {
		if !v1.ValidEntryID(key) {
			return nil, fmt.Errorf("plugin: state for %s holds the invalid key %q", pluginID, key)
		}
	}
	return s, nil
}

// Path is the file this store commits to.
func (s *Store) Path() string { return s.path }

// Get returns one stored value.
func (s *Store) Get(key string) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	if !ok {
		return nil, false
	}
	// Copy, so a caller cannot mutate what the store will write next.
	return append(json.RawMessage(nil), v...), true
}

// Keys returns the stored keys in order.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.values))
	for k := range s.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Set writes one value and commits the whole store. A JSON null deletes the
// key.
//
// The candidate document is built and measured before anything is written, so
// a rejected write leaves both memory and disk exactly as they were. A plugin
// that asks for too much gets an error and keeps the state it had.
func (s *Store) Set(ctx context.Context, key string, value json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !v1.ValidEntryID(key) {
		return fmt.Errorf("plugin: %q is not a state key", key)
	}
	if len(value) > MaxStateValueBytes {
		return fmt.Errorf("plugin: value for %q is %d bytes, more than the %d allowed",
			key, len(value), MaxStateValueBytes)
	}
	if !json.Valid(value) {
		return fmt.Errorf("plugin: value for %q is not valid JSON", key)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := make(map[string]json.RawMessage, len(s.values)+1)
	for k, v := range s.values {
		next[k] = v
	}
	deleting := bytes.Equal(bytes.TrimSpace(value), []byte("null"))
	if deleting {
		delete(next, key)
	} else {
		next[key] = append(json.RawMessage(nil), value...)
	}
	if len(next) > MaxStateKeys {
		return fmt.Errorf("plugin: %d keys, more than the %d allowed", len(next), MaxStateKeys)
	}

	encoded, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("plugin: encode state: %w", err)
	}
	if len(encoded) > MaxStateTotalBytes {
		return fmt.Errorf("plugin: state would be %d bytes, more than the %d byte total allowed",
			len(encoded), MaxStateTotalBytes)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.commit(encoded); err != nil {
		return err
	}
	s.values = next
	return nil
}

// commit replaces the state file atomically: a uniquely named temporary file
// in the destination directory, synced and closed, then renamed over the
// target. A reader therefore sees the old document or the new one, never a
// half-written one, and a failure at any step removes the temporary file.
func (s *Store) commit(data []byte) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("plugin: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("plugin: create temp state: %w", err)
	}
	tmp := f.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp)
		}
	}()

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("plugin: chmod temp state: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("plugin: write temp state: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("plugin: sync temp state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("plugin: close temp state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("plugin: replace %s: %w", s.path, err)
	}
	committed = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
