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
	// saveInstalled and saveConsentLedger are the normal writers unless a
	// test injects a failure.
	saveInstalled     func(Installed) error
	saveConsentLedger func(plugin.ConsentLedger) error
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
	return matchFilesystem(m, p.Release.Filesystem)
}

// matchFilesystem refuses a release whose catalog promises less filesystem
// reach than its manifest takes. A catalog silent about it promised nothing,
// and a plugin confined beyond what the consent said is no harm.
func matchFilesystem(m plugin.Manifest, listed *catalog.Filesystem) error {
	if listed == nil {
		return nil
	}
	if m.Filesystem == nil {
		return fail(KindManifest, nil, "the catalog says the plugin is confined; its manifest declares no filesystem reach")
	}
	if !catalog.SameSet(m.Filesystem.Read, listed.Read) || !catalog.SameSet(m.Filesystem.ReadWrite, listed.ReadWrite) {
		return fail(KindManifest, nil, "filesystem reach is read %v, change %v; the catalog says read %v, change %v",
			m.Filesystem.Read, m.Filesystem.ReadWrite, listed.Read, listed.ReadWrite)
	}
	if !catalog.SameSet(m.Filesystem.Sockets, listed.Sockets) {
		return fail(KindManifest, nil, "sockets are %v; the catalog says %v", m.Filesystem.Sockets, listed.Sockets)
	}
	if m.Filesystem.SignalAny != listed.SignalAny() {
		return fail(KindManifest, nil, "signals to other processes: manifest %v, catalog %v", m.Filesystem.SignalAny, listed.SignalAny())
	}
	return nil
}

func (in *Installer) swapIn(id, staged string, rec Record) error {
	records, err := LoadInstalled(in.Root)
	if err != nil {
		return err
	}
	newConsent, err := in.consentRecord(staged)
	if err != nil {
		return err
	}
	restoreConsent, err := in.replaceConsent(id, newConsent)
	if err != nil {
		return err
	}

	active, prev := in.paths(id)
	had := exists(active)
	if had {
		if err := os.MkdirAll(filepath.Dir(prev), 0o755); err != nil {
			return fail(KindDisk, errors.Join(err, restoreConsent()), "")
		}
		if err := os.RemoveAll(prev); err != nil {
			return fail(KindDisk, errors.Join(err, restoreConsent()), "")
		}
		if err := in.doRename(active, prev); err != nil {
			return fail(KindDisk, errors.Join(err, restoreConsent()), "")
		}
	}
	if err := in.doRename(staged, active); err != nil {
		var rerr error
		if had {
			rerr = in.doRename(prev, active)
		}
		joined := errors.Join(err, rerr, restoreConsent())
		if had && rerr != nil {
			return fail(KindDisk, joined, "restoring the previous version failed")
		}
		return fail(KindDisk, joined, "")
	}
	if old, ok := records[id]; ok && had {
		old.Previous = nil
		rec.Previous = &old
	}
	records[id] = rec
	if err := in.saveRecords(records); err != nil {
		undoErr := in.undoSwap(id, had)
		var consentErr error
		if undoErr == nil {
			consentErr = restoreConsent()
		}
		joined := errors.Join(err, undoErr, consentErr)
		switch {
		case undoErr != nil:
			return fail(KindDisk, joined, "the new version is active with its approval, but its installed record could not be saved")
		case consentErr != nil:
			return fail(KindDisk, joined, "the update was rolled back, but its previous approval was not restored")
		default:
			return fail(KindDisk, joined, "the update was rolled back because its installed record could not be saved")
		}
	}
	return nil
}

func (in *Installer) saveRecords(records Installed) error {
	if in.saveInstalled != nil {
		if err := in.saveInstalled(records); err != nil {
			return fail(KindDisk, err, "")
		}
		return nil
	}
	return records.Save(in.Root)
}

// undoSwap puts the previous tree back after the new one was activated. It is
// only used while the store worker still holds the plugin stopped.
func (in *Installer) undoSwap(id string, had bool) error {
	active, prev := in.paths(id)
	if !had {
		if err := os.RemoveAll(active); err != nil {
			return err
		}
		return nil
	}
	hold, err := in.stage("undo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(hold)
	held := filepath.Join(hold, id)
	if err := in.doRename(active, held); err != nil {
		return err
	}
	if err := in.doRename(prev, active); err != nil {
		if rerr := in.doRename(held, active); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return nil
}

// undoRollback puts the installed version back after a rollback's record could
// not be saved. The rolled-back tree moves aside, the installed tree returns to
// active, and the rolled-back tree takes its place as the kept version.
func (in *Installer) undoRollback(id string) error {
	active, prev := in.paths(id)
	hold, err := in.stage("undo-rollback-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(hold)
	tmp := filepath.Join(hold, id)
	if err := in.doRename(active, tmp); err != nil {
		return err
	}
	if err := in.doRename(prev, active); err != nil {
		if rerr := in.doRename(tmp, active); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return in.doRename(tmp, prev)
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
		nextConsent, err := in.consentRecord(prev)
		if err != nil {
			return err
		}
		restoreConsent, err := in.replaceConsent(id, nextConsent)
		if err != nil {
			return err
		}
		hold, err := in.stage("rollback-")
		if err != nil {
			return fail(KindDisk, errors.Join(err, restoreConsent()), "")
		}
		defer os.RemoveAll(hold)
		staged := filepath.Join(hold, id)
		if err := in.doRename(prev, staged); err != nil {
			return fail(KindDisk, errors.Join(err, restoreConsent()), "")
		}
		if err := in.doRename(active, prev); err != nil {
			rerr := in.doRename(staged, prev)
			joined := errors.Join(err, rerr, restoreConsent())
			if rerr != nil {
				return fail(KindDisk, joined, "restoring the previous version failed")
			}
			return fail(KindDisk, joined, "")
		}
		if err := in.doRename(staged, active); err != nil {
			rerr := in.doRename(prev, active)
			if rerr == nil {
				rerr = in.doRename(staged, prev)
			}
			joined := errors.Join(err, rerr, restoreConsent())
			if rerr != nil {
				return fail(KindDisk, joined, "restoring the installed version failed")
			}
			return fail(KindDisk, joined, "")
		}

		cur := records[id]
		next := Record{Source: SourceUnknown}
		if cur.Previous != nil {
			next = *cur.Previous
		}
		cur.Previous = nil
		next.Previous = &cur
		records[id] = next
		if err := in.saveRecords(records); err != nil {
			undoErr := in.undoRollback(id)
			var consentErr error
			if undoErr == nil {
				consentErr = restoreConsent()
			}
			joined := errors.Join(err, undoErr, consentErr)
			switch {
			case undoErr != nil:
				return fail(KindDisk, joined, "the rollback could not be undone, so its installed record was not saved")
			case consentErr != nil:
				return fail(KindDisk, joined, "the rollback was undone, but its previous approval was not restored")
			default:
				return fail(KindDisk, joined, "the rollback was undone because its installed record could not be saved")
			}
		}
		return nil
	})
}

// Remove deletes every version of id. Its configuration is not the store's to
// touch: an absent plugin keeps its settings and placements. The trees move
// aside before their approval and record are forgotten, so a failed write can
// put them back rather than leave a live tree without its approval.
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
		hold, err := in.stage("remove-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(hold)

		type move struct{ from, to string }
		moves := []move{
			{from: active, to: filepath.Join(hold, "active")},
			{from: prev, to: filepath.Join(hold, "prev")},
		}
		var moved []move
		restore := func() error {
			var errs []error
			for i := len(moved) - 1; i >= 0; i-- {
				if err := in.doRename(moved[i].to, moved[i].from); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		}
		for _, m := range moves {
			if !exists(m.from) {
				continue
			}
			if err := in.doRename(m.from, m.to); err != nil {
				return fail(KindDisk, errors.Join(err, restore()), "")
			}
			moved = append(moved, m)
		}
		if err := in.forgetConsent(id); err != nil {
			return fail(KindDisk, errors.Join(err, restore()), "")
		}
		delete(records, id)
		if err := in.saveRecords(records); err != nil {
			rerr := restore()
			var cerr error
			dir := active
			if !exists(dir) {
				dir = prev
			}
			if exists(dir) {
				cerr = in.recordConsent(plugin.SourceManaged, id, dir)
			}
			joined := errors.Join(err, rerr, cerr)
			switch {
			case rerr != nil:
				return fail(KindDisk, joined, "the removal could not be undone after its record could not be saved")
			case cerr != nil:
				return fail(KindDisk, joined, "the removal was undone, but its approval was not restored")
			default:
				return fail(KindDisk, joined, "the removal was undone because its record could not be saved")
			}
		}
		return nil
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
//
// A crash after the approval write but before the tree move, in swapIn or
// Rollback, leaves the live tree with the other version's digest approved.
// VerifyConsent reports changed, so the plugin starts without capabilities
// until it is approved again. Closing that window needs a durable pending
// marker; the approval is deliberately written first so a crash can never
// leave a live tree with no approval at all.
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
