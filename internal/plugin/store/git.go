package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

// CatalogFile is the one file a source repository must hold at its root.
const CatalogFile = "catalog.json"

var errOutputTooLarge = errors.New("output too large")

// Git reads catalogs through the git command, as Noctalia does: a blobless,
// checkout-free clone per source, and git show for the one file needed.
type Git struct {
	// Bin is the git executable; empty means git on PATH.
	Bin string
	// Timeout bounds each invocation; zero means 60 seconds.
	Timeout time.Duration
}

// Catalog clones or fetches url into dir and returns catalog.json at the
// remote's default branch together with the commit it was read from.
func (g Git) Catalog(ctx context.Context, dir, url string) ([]byte, string, error) {
	name := g.Bin
	if name == "" {
		name = "git"
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, "", fail(KindGitMissing, err, "")
	}
	have, err := g.run(ctx, bin, dir, catalog.MaxCatalogBytes, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(string(have)) != url {
		// No clone yet, a broken one, or a clone of a URL the source no
		// longer names: start again rather than fetch the wrong remote.
		if err := os.RemoveAll(dir); err != nil {
			return nil, "", fail(KindDisk, err, "")
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return nil, "", fail(KindDisk, err, "")
		}
		if _, err := g.run(ctx, bin, "", catalog.MaxCatalogBytes, "clone", "--filter=blob:none", "--no-checkout", "--quiet", "--", url, dir); err != nil {
			return nil, "", err
		}
	} else if _, err := g.run(ctx, bin, dir, catalog.MaxCatalogBytes, "fetch", "--quiet", "origin"); err != nil {
		return nil, "", err
	}
	commit, err := g.run(ctx, bin, dir, catalog.MaxCatalogBytes, "rev-parse", "origin/HEAD")
	if err != nil {
		return nil, "", err
	}
	body, err := g.run(ctx, bin, dir, catalog.MaxCatalogBytes, "show", "origin/HEAD:"+CatalogFile)
	if err != nil {
		return nil, "", err
	}
	return body, strings.TrimSpace(string(commit)), nil
}

func (g Git) run(ctx context.Context, bin, dir string, max int, args ...string) ([]byte, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
		// Stop git walking up into an enclosing repository when dir is not
		// yet a clone of its own.
		env = append(env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	stdout := &capped{max: max}
	stderr := &capped{max: 512, truncate: true}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, fail(KindGitTimeout, nil, "git %s", args[len(args)-1])
	case stdout.over:
		return nil, fail(KindCatalog, nil, "%s is larger than %d bytes", CatalogFile, max)
	case err != nil:
		return nil, fail(KindUnreachable, err, "%s", strings.TrimSpace(stderr.buf.String()))
	}
	return stdout.buf.Bytes(), nil
}

// capped is a writer with a ceiling. Over it, it either fails the command or,
// for stderr, keeps the head and drops the rest.
type capped struct {
	buf      bytes.Buffer
	max      int
	truncate bool
	over     bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.over = true
		if !c.truncate {
			return 0, errOutputTooLarge
		}
		c.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return c.buf.Write(p)
}
