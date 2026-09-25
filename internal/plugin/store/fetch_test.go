package store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	t.Parallel()
	srv := newAssetServer(t)
	body := []byte("release bytes")
	url := srv.put("/a.tar.gz", body)
	ctx := context.Background()

	cases := []struct {
		name  string
		url   string
		sha   string
		limit int64
		want  Kind
	}{
		{"ok", url, sum(body), int64(len(body)), ""},
		{"checksum", url, sum([]byte("other")), int64(len(body)), KindChecksum},
		{"too large", url, sum(body), int64(len(body)) - 1, KindTooLarge},
		{"not found", srv.URL + "/missing", sum(body), 100, KindUnreachable},
		{"remote http", "http://example.invalid/a", sum(body), 100, KindCatalog},
	}
	for _, c := range cases {
		dst := filepath.Join(t.TempDir(), "asset")
		err := Download(ctx, NewHTTPClient(), c.url, c.sha, c.limit, dst)
		if KindOf(err) != c.want || (c.want == "") != (err == nil) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
		_, statErr := os.Stat(dst)
		if c.want == "" && statErr != nil {
			t.Errorf("%s: no file written", c.name)
		}
		if c.want != "" && statErr == nil {
			t.Errorf("%s: a rejected download was left on disk", c.name)
		}
	}
}

func TestDownloadRefusesARedirectToRemoteHTTP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.RedirectHandler("http://example.invalid/a", http.StatusFound))
	t.Cleanup(srv.Close)
	err := Download(context.Background(), NewHTTPClient(), srv.URL, sum(nil), 10, filepath.Join(t.TempDir(), "a"))
	if KindOf(err) != KindUnreachable {
		t.Fatalf("err = %v, want the redirect refused", err)
	}
}
