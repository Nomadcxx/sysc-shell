package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"

	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

// MediaKey identifies one sha256-pinned item the current UI needs.
type MediaKey struct {
	URL    string
	SHA256 string
	Max    int64
}

// MediaState is the cache result for one wanted sha256.
type MediaState struct {
	Path string
	Err  error
}

// Want replaces the media set needed by the current UI. It never blocks on
// I/O; the worker coalesces requests and cancels an unneeded active download.
func (s *Store) Want(keys []MediaKey) {
	wanted := make(map[string]MediaKey, len(keys))
	ordered := make([]MediaKey, 0, len(keys))
	for _, key := range keys {
		if _, exists := wanted[key.SHA256]; exists {
			continue
		}
		wanted[key.SHA256] = key
		ordered = append(ordered, key)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if maps.Equal(s.mediaWanted, wanted) {
		return
	}
	previous := s.mediaWanted
	s.mediaWanted = wanted
	media := make(map[string]MediaState, len(wanted))
	for sha, state := range s.state.Media {
		old, hadOld := previous[sha]
		current, stillWanted := wanted[sha]
		if hadOld && stillWanted && old == current {
			media[sha] = state
		}
	}
	s.state.Media = media
	if s.mediaCancel != nil && s.mediaActive.SHA256 != "" {
		if current, ok := wanted[s.mediaActive.SHA256]; !ok || current != s.mediaActive {
			s.mediaCancel()
		}
	}
	s.publishLocked()
	select {
	case s.mediaWant <- ordered:
	default:
		select {
		case <-s.mediaWant:
		default:
		}
		s.mediaWant <- ordered
	}
}

func (s *Store) fetchMedia(ctx context.Context, keys []MediaKey) {
	for _, key := range keys {
		s.mu.Lock()
		current, wanted := s.mediaWanted[key.SHA256]
		if !wanted || current != key {
			s.mu.Unlock()
			continue
		}
		if state, ok := s.state.Media[key.SHA256]; ok && (state.Path != "" || state.Err != nil) {
			s.mu.Unlock()
			continue
		}
		if !validMediaKey(key) {
			s.mu.Unlock()
			s.finishMedia(key, MediaState{Err: fail(KindCatalog, nil, "invalid media key")})
			continue
		}
		fetchCtx, cancel := context.WithCancel(ctx)
		s.mediaActive, s.mediaCancel = key, cancel
		s.mu.Unlock()

		path := filepath.Join(s.opts.MediaDir, key.SHA256)
		err := s.fetchOneMedia(fetchCtx, key, path)
		cancel()
		s.mu.Lock()
		if s.mediaActive == key {
			s.mediaActive = MediaKey{}
			s.mediaCancel = nil
		}
		current, wanted = s.mediaWanted[key.SHA256]
		if wanted && current == key && ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			media := MediaState{Path: path, Err: err}
			if err != nil {
				media.Path = ""
			}
			s.state.Media[key.SHA256] = media
			s.publishLocked()
		}
		s.mu.Unlock()
	}
}

func validMediaKey(key MediaKey) bool {
	if len(key.SHA256) != sha256.Size*2 || key.Max < 0 {
		return false
	}
	for _, c := range key.SHA256 {
		if !('0' <= c && c <= '9') && !('a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) finishMedia(key MediaKey, media MediaState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.mediaWanted[key.SHA256]; ok && current == key {
		s.state.Media[key.SHA256] = media
		s.publishLocked()
	}
}

func (s *Store) fetchOneMedia(ctx context.Context, key MediaKey, path string) error {
	if err := catalog.CheckFetchURL(key.URL); err != nil {
		return fail(KindCatalog, err, "")
	}
	hit, err := checkMediaCache(path, key)
	if err != nil || hit {
		return err
	}
	if err := os.MkdirAll(s.opts.MediaDir, 0o755); err != nil {
		return fail(KindDisk, err, "")
	}
	client := s.opts.Installer.Client
	if client == nil {
		client = NewHTTPClient()
	}
	return Download(ctx, client, key.URL, key.SHA256, key.Max, path)
}

func checkMediaCache(path string, key MediaKey) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fail(KindDisk, err, "")
	}
	if !info.Mode().IsRegular() {
		if err := os.Remove(path); err != nil {
			return false, fail(KindDisk, err, "removing invalid cache entry")
		}
		return false, nil
	}
	if info.Size() > key.Max {
		if err := os.Remove(path); err != nil {
			return false, fail(KindDisk, err, "removing oversized cache entry")
		}
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, fail(KindDisk, err, "")
	}
	h := sha256.New()
	n, readErr := io.Copy(h, io.LimitReader(f, key.Max+1))
	closeErr := f.Close()
	if readErr != nil {
		return false, fail(KindDisk, readErr, "")
	}
	if closeErr != nil {
		return false, fail(KindDisk, closeErr, "")
	}
	if n <= key.Max && hex.EncodeToString(h.Sum(nil)) == key.SHA256 {
		return true, nil
	}
	if err := os.Remove(path); err != nil {
		return false, fail(KindDisk, err, "removing mismatched cache entry")
	}
	return false, nil
}
