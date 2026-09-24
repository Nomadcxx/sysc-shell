package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

// NewHTTPClient is the client every store download uses. Redirects are held to
// the same URL rule as the catalog, so a release host cannot bounce a download
// to plain http elsewhere.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return catalog.CheckFetchURL(req.URL.String())
		},
	}
}

// Download fetches rawURL into dst, which must not exist. It refuses more than
// limit bytes and anything whose sha256 is not sha, and leaves nothing behind
// when it refuses.
func Download(ctx context.Context, c *http.Client, rawURL, sha string, limit int64, dst string) error {
	if err := catalog.CheckFetchURL(rawURL); err != nil {
		return fail(KindCatalog, err, "")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fail(KindUnreachable, err, "%s", rawURL)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fail(KindUnreachable, err, "%s", rawURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(KindUnreachable, nil, "%s: %s", rawURL, resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		err = fail(KindUnreachable, err, "%s", rawURL)
	case n > limit:
		err = fail(KindTooLarge, nil, "%s is larger than %d bytes", rawURL, limit)
	default:
		if got := hex.EncodeToString(h.Sum(nil)); got != sha {
			err = fail(KindChecksum, nil, "want %s, got %s", sha, got)
		}
	}
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
