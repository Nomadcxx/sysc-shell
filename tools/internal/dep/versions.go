package dep

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProxyBase is the module proxy the version lookup asks. It is the proxy the
// go command itself uses, so the versions it names are the versions a build
// can resolve.
const ProxyBase = "https://proxy.golang.org"

// ErrNotPublished is returned for a module the proxy does not carry.
var ErrNotPublished = fmt.Errorf("the module is not published")

// Versions returns the published versions of a module, oldest first.
//
// The proxy is asked for the version list rather than the go command, because
// the go command reads the module graph of the tree it runs in, and a report
// must not depend on the tree it reports about.
func Versions(ctx context.Context, client *http.Client, proxy, module string) ([]string, error) {
	if proxy == "" {
		proxy = ProxyBase
	}
	url := strings.TrimSuffix(proxy, "/") + "/" + EscapeModule(module) + "/@v/list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusGone:
		return nil, fmt.Errorf("%s: %w", module, ErrNotPublished)
	default:
		return nil, fmt.Errorf("%s: the proxy answered %s", module, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	SortVersions(out)
	return out, nil
}

// LatestRelease returns the newest published release of a module.
//
// A pre-release is not a release: it is a candidate for one. The house pins
// releases, so the version a pin is measured against is a release. A module
// with nothing but pre-releases falls back to the newest of those.
func LatestRelease(published []string) string {
	release := ""
	for _, v := range published {
		parsed, err := ParseVersion(v)
		if err != nil || parsed.Pre != "" {
			continue
		}
		if release == "" || CompareVersions(v, release) > 0 {
			release = v
		}
	}
	if release != "" {
		return release
	}
	for _, v := range published {
		if release == "" || CompareVersions(v, release) > 0 {
			release = v
		}
	}
	return release
}

// LatestVersion returns the newest published version of a module, or an
// empty string when the module has no published version.
func LatestVersion(ctx context.Context, client *http.Client, proxy, module string) (string, error) {
	versions, err := Versions(ctx, client, proxy, module)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", nil
	}
	return versions[len(versions)-1], nil
}

// ReleasesBehind counts the published releases that are newer than the pin.
//
// A pre-release is not a release: it is a candidate for one, and the house
// pins releases. A pin that is not published at all is not counted: the
// caller decides what to say about it, because a pseudo-version and a
// version that was never published are different facts.
func ReleasesBehind(pinned string, published []string) int {
	n := 0
	for _, v := range published {
		parsed, err := ParseVersion(v)
		if err != nil || parsed.Pre != "" {
			continue
		}
		if CompareVersions(v, pinned) > 0 {
			n++
		}
	}
	return n
}

// EscapeModule writes a module path the way the proxy names it: a capital
// letter becomes an exclamation mark and the letter in lower case.
func EscapeModule(module string) string {
	var b strings.Builder
	for _, r := range module {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NewHTTPClient returns the client the tools talk to the network with. The
// timeout is short because every call it makes is a small request to a
// service that answers in milliseconds.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
