package store

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

// Installer changes the managed plugin tree. It is used from the store's one
// worker goroutine only, so it holds no lock of its own.
type Installer struct {
	// Root is the managed plugin root.
	Root   string
	Client *http.Client
	// Arch is runtime.GOARCH; assets are keyed linux-<Arch>.
	Arch   string
	Limits Limits
	// Replace stops plugin id, runs swap, and rescans. Every change to the
	// tree goes through it, so the host never runs a directory mid-swap. Nil
	// runs swap directly.
	Replace func(id string, swap func() error) error

	// rename is os.Rename unless a test injects a failure.
	rename func(oldpath, newpath string) error
}

// Plan is one resolved release to install.
type Plan struct {
	Source        string
	CatalogCommit string
	ID            string
	Release       catalog.Release
}

// Install downloads, verifies and installs p, keeping any installed version as
// the one-step rollback. Everything that can fail is done in staging first; a
// failure before the final rename leaves the previous version in place.
func (in *Installer) Install(ctx context.Context, p Plan) error {
	if !v1.ValidPluginID(p.ID) {
		return fail(KindNotListed, nil, "%q is not a plugin id", p.ID)
	}
	asset, ok := p.Release.Assets["linux-"+in.Arch]
	if !ok {
		return fail(KindNoAsset, nil, "%s %s has no linux-%s asset", p.ID, p.Release.Version, in.Arch)
	}
	stage, err := in.stage("install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	archive := filepath.Join(stage, "asset.tar.gz")
	if err := Download(ctx, in.Client, asset.URL, asset.SHA256, asset.Size, archive); err != nil {
		return err
	}
	tree := filepath.Join(stage, "tree")
	if err := os.Mkdir(tree, 0o755); err != nil {
		return fail(KindDisk, err, "")
	}
	f, err := os.Open(archive)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	lim := in.Limits
	if lim.MaxEntries == 0 {
		lim = DefaultLimits
	}
	err = Extract(f, tree, p.ID, lim)
	_ = f.Close()
	if err != nil {
		return err
	}
	staged := filepath.Join(tree, p.ID)
	m, err := plugin.LoadManifest(staged)
	if err != nil {
		return fail(KindManifest, err, "")
	}
	if err := matchManifest(m, p); err != nil {
		return err
	}
	rec := recordFromManifest(m)
	rec.Source, rec.CatalogCommit, rec.SHA256, rec.InstalledAt = p.Source, p.CatalogCommit, asset.SHA256, time.Now().UTC()
	return in.replace(p.ID, func() error { return in.swapIn(p.ID, staged, rec) })
}

// matchManifest proves the archive is the release the catalog described, so a
// catalog cannot under-declare what the plugin will ask for.
func matchManifest(m plugin.Manifest, p Plan) error {
	caps := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = string(c)
	}
	switch {
	case m.ID != p.ID:
		return fail(KindManifest, nil, "id is %q; the catalog says %q", m.ID, p.ID)
	case m.Version != p.Release.Version:
		return fail(KindManifest, nil, "version is %s; the catalog says %s", m.Version, p.Release.Version)
	case m.Protocol != p.Release.Protocol:
		return fail(KindManifest, nil, "protocol is %d.%d; the catalog says %d.%d",
			m.Protocol.Major, m.Protocol.Minor, p.Release.Protocol.Major, p.Release.Protocol.Minor)
	case !catalog.SameSet(caps, p.Release.Capabilities):
		return fail(KindManifest, nil, "capabilities are %v; the catalog says %v", caps, p.Release.Capabilities)
	case !catalog.SameSet(m.Requires, p.Release.Requires.Commands):
		return fail(KindManifest, nil, "required commands are %v; the catalog says %v", m.Requires, p.Release.Requires.Commands)
	}
	return nil
}

func (in *Installer) swapIn(id, staged string, rec Record) error {
	records, err := LoadInstalled(in.Root)
	if err != nil {
		return err
	}
	active, prev := in.paths(id)
	had := exists(active)
	if had {
		if err := os.MkdirAll(filepath.Dir(prev), 0o755); err != nil {
			return fail(KindDisk, err, "")
		}
		if err := os.RemoveAll(prev); err != nil {
			return fail(KindDisk, err, "")
		}
		if err := in.doRename(active, prev); err != nil {
			return fail(KindDisk, err, "")
		}
	}
	if err := in.doRename(staged, active); err != nil {
		if had {
			if rerr := in.doRename(prev, active); rerr != nil {
				return fail(KindDisk, errors.Join(err, rerr), "restoring the previous version failed")
			}
		}
		return fail(KindDisk, err, "")
	}
	if old, ok := records[id]; ok && had {
		old.Previous = nil
		rec.Previous = &old
	}
	records[id] = rec
	return records.Save(in.Root)
}

// Rollback swaps the installed version with the kept previous one.
func (in *Installer) Rollback(id string) error {
	if !v1.ValidPluginID(id) {
		return fail(KindNotListed, nil, "%q is not a plugin id", id)
	}
	active, prev := in.paths(id)
	if !exists(prev) || !exists(active) {
		return fail(KindNoPrevious, nil, "%s", id)
	}
	return in.replace(id, func() error {
		records, err := LoadInstalled(in.Root)
		if err != nil {
			return err
		}
		hold, err := in.stage("rollback-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(hold)
		held := filepath.Join(hold, id)
		if err := in.doRename(active, held); err != nil {
			return fail(KindDisk, err, "")
		}
		if err := in.doRename(prev, active); err != nil {
			if rerr := in.doRename(held, active); rerr != nil {
				return fail(KindDisk, errors.Join(err, rerr), "restoring the installed version failed")
			}
			return fail(KindDisk, err, "")
		}
		keepErr := in.doRename(held, prev)

		cur := records[id]
		next := Record{Source: SourceUnknown}
		if cur.Previous != nil {
			next = *cur.Previous
		}
		cur.Previous = nil
		if keepErr == nil {
			next.Previous = &cur
		}
		records[id] = next
		if err := records.Save(in.Root); err != nil {
			return err
		}
		if keepErr != nil {
			return fail(KindDisk, keepErr, "rolled back, but the replaced version could not be kept")
		}
		return nil
	})
}

// Remove deletes every version of id. Its configuration is not the store's to
// touch: an absent plugin keeps its settings and placements.
func (in *Installer) Remove(id string) error {
	if !v1.ValidPluginID(id) {
		return fail(KindNotListed, nil, "%q is not a plugin id", id)
	}
	return in.replace(id, func() error {
		records, err := LoadInstalled(in.Root)
		if err != nil {
			return err
		}
		active, prev := in.paths(id)
		for _, dir := range []string{active, prev} {
			if err := os.RemoveAll(dir); err != nil {
				return fail(KindDisk, err, "")
			}
		}
		delete(records, id)
		return records.Save(in.Root)
	})
}

// CleanStaging removes work an interrupted run left behind.
func (in *Installer) CleanStaging() error {
	return os.RemoveAll(filepath.Join(in.Root, ".staging"))
}

// RecoverInterrupted restores a plugin whose swap crashed between the two
// renames of swapIn: active moved to .prev, but staged never made it to
// active, so only .prev/<id> is left and LoadInstalled would drop the
// record. For each directory under .prev whose active counterpart is
// missing, it renames .prev/<id> back to <root>/<id>.
func (in *Installer) RecoverInterrupted() error {
	prevRoot := filepath.Join(in.Root, ".prev")
	entries, err := os.ReadDir(prevRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fail(KindDisk, err, "")
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		active, prev := in.paths(id)
		if exists(active) {
			continue
		}
		if err := in.doRename(prev, active); err != nil {
			errs = append(errs, fail(KindDisk, err, "recovering %s", id))
		}
	}
	return errors.Join(errs...)
}

func (in *Installer) stage(prefix string) (string, error) {
	staging := filepath.Join(in.Root, ".staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", fail(KindDisk, err, "")
	}
	dir, err := os.MkdirTemp(staging, prefix)
	if err != nil {
		return "", fail(KindDisk, err, "")
	}
	return dir, nil
}

func (in *Installer) paths(id string) (active, prev string) {
	return filepath.Join(in.Root, id), filepath.Join(in.Root, ".prev", id)
}

func (in *Installer) replace(id string, swap func() error) error {
	if in.Replace == nil {
		return swap()
	}
	return in.Replace(id, swap)
}

func (in *Installer) doRename(oldpath, newpath string) error {
	if in.rename != nil {
		return in.rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
