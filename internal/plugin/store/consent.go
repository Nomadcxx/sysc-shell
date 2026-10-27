package store

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

// recordConsent approves the tree now installed at dir. Every change the store
// makes to the managed tree was consented to by the operation that made it,
// so the store records what that operation left behind.
func (in *Installer) recordConsent(source plugin.Source, id, dir string) error {
	m, err := plugin.LoadManifest(dir)
	if err != nil {
		return fail(KindManifest, err, "")
	}
	digest, err := plugin.ContentDigest(m)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	l, err := plugin.LoadConsent(plugin.ConsentPath(in.Root))
	if err != nil {
		return fail(KindDisk, err, "approvals unreadable; not rewriting the ledger")
	}
	l.Set(source, id, plugin.RecordFor(m, digest, time.Now().UTC()))
	if err := l.Save(plugin.ConsentPath(in.Root)); err != nil {
		return fail(KindDisk, err, "recording the approval failed")
	}
	return nil
}

func (in *Installer) forgetConsent(id string) error {
	l, err := plugin.LoadConsent(plugin.ConsentPath(in.Root))
	if err != nil {
		return fail(KindDisk, err, "approvals unreadable; not rewriting the ledger")
	}
	l.Delete(plugin.SourceManaged, id)
	if err := l.Save(plugin.ConsentPath(in.Root)); err != nil {
		return fail(KindDisk, err, "forgetting the approval failed")
	}
	return nil
}

// Approve records one copy of a plugin as it is now. It is the user's answer
// to a copy whose bytes no longer match its approval, or a hand-placed copy
// that has none.
func (s *Store) Approve(source plugin.Source, id string) (<-chan error, error) {
	if _, err := s.approvalDir(source, id); err != nil {
		return nil, err
	}
	return s.enqueue(op{name: "approve " + id, id: id, run: func(context.Context) error {
		dir, err := s.approvalDir(source, id)
		if err != nil {
			return err
		}
		return s.opts.Installer.recordConsent(source, id, dir)
	}})
}

func (s *Store) approvalDir(source plugin.Source, id string) (string, error) {
	switch source {
	case plugin.SourceUser:
		if s.opts.Local != nil {
			if dir := s.opts.Local()[id]; dir != "" {
				return dir, nil
			}
		}
	case plugin.SourceManaged:
		if exists(filepath.Join(s.opts.Installer.Root, id)) {
			return filepath.Join(s.opts.Installer.Root, id), nil
		}
	}
	return "", fail(KindNotListed, nil, "no %s copy of %s to approve", source, id)
}

// adoptApprovals records the trees of managed installs made before approvals
// were recorded. Their user consented when the store installed them; a
// directory the store cannot account for is left unrecorded.
func (s *Store) adoptApprovals(installed Installed) {
	root := s.opts.Installer.Root
	l, err := plugin.LoadConsent(plugin.ConsentPath(root))
	if err != nil {
		return // leave the unreadable ledger untouched; recordConsent will refuse to rewrite it
	}
	for id, rec := range installed {
		if rec.Source == SourceUnknown {
			continue
		}
		if _, ok := l.Lookup(plugin.SourceManaged, id); ok {
			continue
		}
		if err := s.opts.Installer.recordConsent(plugin.SourceManaged, id, filepath.Join(root, id)); err != nil {
			slog.Warn("plugin store: cannot record an earlier approval", "plugin", id, "err", err)
		}
	}
}
