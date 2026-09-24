package store

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Limits bounds one archive.
type Limits struct {
	MaxEntries int
	MaxBytes   int64
}

var DefaultLimits = Limits{MaxEntries: 256, MaxBytes: 128 << 20}

// Extract unpacks a gzipped tar into dest, which must exist. The archive must
// hold exactly one top-level directory, named root, containing only regular
// files and directories.
//
// Writes go through os.Root, so no entry can land outside dest whatever its
// name; the name checks exist to reject such an archive by name rather than
// by an opaque error. Links are refused outright: a plugin directory has no
// use for one, and each is a way out of the tree.
func Extract(r io.Reader, dest, root string, lim Limits) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fail(KindArchive, err, "not a gzip stream")
	}
	defer gz.Close()
	fsys, err := os.OpenRoot(dest)
	if err != nil {
		return fail(KindDisk, err, "")
	}
	defer fsys.Close()

	tr := tar.NewReader(gz)
	var entries int
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(KindArchive, err, "")
		}
		if entries++; entries > lim.MaxEntries {
			return fail(KindArchive, nil, "more than %d entries", lim.MaxEntries)
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		if !filepath.IsLocal(name) || path.Clean(name) != name {
			return fail(KindArchive, nil, "%q is not a plain relative path", hdr.Name)
		}
		if top, _, _ := strings.Cut(name, "/"); top != root {
			return fail(KindArchive, nil, "%q is outside %s/", hdr.Name, root)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := fsys.MkdirAll(name, 0o755); err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
		case tar.TypeReg:
			if total += hdr.Size; hdr.Size < 0 || total > lim.MaxBytes {
				return fail(KindArchive, nil, "more than %d bytes", lim.MaxBytes)
			}
			if err := fsys.MkdirAll(path.Dir(name), 0o755); err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			// O_EXCL: an archive naming one file twice is malformed, and the
			// second write must not replace what was already checked.
			f, err := fsys.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
			n, err := io.Copy(f, io.LimitReader(tr, hdr.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil && n != hdr.Size {
				err = io.ErrUnexpectedEOF
			}
			if err != nil {
				return fail(KindArchive, err, "%q", hdr.Name)
			}
		default:
			return fail(KindArchive, nil, "%q is not a regular file or directory", hdr.Name)
		}
	}
	if info, err := fsys.Stat(root); err != nil || !info.IsDir() {
		return fail(KindArchive, err, "no %s/ directory", root)
	}
	return nil
}
