package store

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitMedia(t *testing.T, s *Store, sha string) MediaState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if media, ok := s.State().Media[sha]; ok && (media.Path != "" || media.Err != nil) {
			return media
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("media %s was not resolved", sha)
	return MediaState{}
}

func TestWantFetchesAMissAndReusesTheCachedFile(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	body := []byte("# README\n")
	sha := sum(body)
	url := f.srv.put("/README.md", body)
	key := MediaKey{URL: url, SHA256: sha, Max: 1024}
	f.st.Want([]MediaKey{key})
	first := waitMedia(t, f.st, sha)
	if first.Err != nil || first.Path != filepath.Join(f.mediaDir, sha) {
		t.Fatalf("first media state = %+v", first)
	}
	if got, err := os.ReadFile(first.Path); err != nil || string(got) != string(body) {
		t.Fatalf("cached content = %q, %v", got, err)
	}

	f.srv.mu.Lock()
	delete(f.srv.files, "/README.md")
	f.srv.mu.Unlock()
	f.st.Want(nil)
	f.st.Want([]MediaKey{key})
	second := waitMedia(t, f.st, sha)
	if second.Err != nil || second.Path != first.Path {
		t.Fatalf("cache hit = %+v, want path %q", second, first.Path)
	}
}

func TestWantReportsChecksumAndSizeFailures(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	body := []byte("12345")
	url := f.srv.put("/asset", body)
	cases := []struct {
		name string
		key  MediaKey
		kind Kind
	}{
		{name: "checksum", key: MediaKey{URL: url, SHA256: sum([]byte("wrong")), Max: 100}, kind: KindChecksum},
		{name: "too large", key: MediaKey{URL: url, SHA256: sum(body), Max: 4}, kind: KindTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.st.Want([]MediaKey{tc.key})
			media := waitMedia(t, f.st, tc.key.SHA256)
			if KindOf(media.Err) != tc.kind {
				t.Fatalf("media error = %v, want %s", media.Err, tc.kind)
			}
			if _, err := os.Stat(filepath.Join(f.mediaDir, tc.key.SHA256)); !os.IsNotExist(err) {
				t.Fatalf("failed download left cache file: %v", err)
			}
		})
	}
}

func TestWantReplacementCancelsAnUnneededFetch(t *testing.T) {
	t.Parallel()
	f := newStoreFixture(t, nil)
	started, canceled := make(chan struct{}), make(chan struct{})
	quick := []byte("next")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		_, _ = w.Write(quick)
	}))
	defer srv.Close()

	unneeded := MediaKey{URL: srv.URL + "/slow", SHA256: sum([]byte("slow")), Max: 1024}
	wanted := MediaKey{URL: srv.URL + "/quick", SHA256: sum(quick), Max: 1024}
	f.st.Want([]MediaKey{unneeded})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("README fetch did not start")
	}
	f.st.Want([]MediaKey{wanted})
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("replacing the wish list did not cancel the old fetch")
	}
	if media := waitMedia(t, f.st, wanted.SHA256); media.Err != nil || media.Path == "" {
		t.Fatalf("wanted media = %+v", media)
	}
	if _, ok := f.st.State().Media[unneeded.SHA256]; ok {
		t.Fatal("the replaced README remained in the current media state")
	}
}
