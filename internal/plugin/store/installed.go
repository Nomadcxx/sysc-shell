package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
)

const (
	InstalledName = "installed.json"
	// SourceUnknown marks a plugin whose provenance was lost: a directory with
	// no record, or a record that no longer matches its manifest.
	SourceUnknown = "unknown"
)

// Record is what the store knows about one installed plugin. Capabilities and
// Requires are kept so an update's consent check needs no disk read.
type Record struct {
	Source        string    `json:"source"`
	Version       string    `json:"version"`
	CatalogCommit string    `json:"catalog_commit,omitempty"`
	SHA256        string    `json:"sha256,omitempty"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	Requires      []string  `json:"requires,omitempty"`
	InstalledAt   time.Time `json:"installed_at,omitzero"`
	Previous      *Record   `json:"previous,omitempty"`
}

// Installed maps plugin id to its record.
type Installed map[string]Record

// LoadInstalled reads the records under root and reconciles them with the
// directories actually there. The tree is the truth: a record without a
// directory is dropped, a directory without a matching record is adopted with
// an unknown source, and a corrupt file is rebuilt. Nothing is deleted.
func LoadInstalled(root string) (Installed, error) {
	in := Installed{}
	path := filepath.Join(root, InstalledName)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fail(KindDisk, err, "")
	default:
		if jerr := json.Unmarshal(data, &in); jerr != nil {
			slog.Warn("plugin store: installed records unreadable; rebuilding from disk", "path", path, "err", jerr)
			in = Installed{}
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fail(KindDisk, err, "")
	}
	present := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := plugin.LoadManifest(filepath.Join(root, e.Name()))
		if err != nil || m.ID != e.Name() {
			continue
		}
		present[m.ID] = true
		if rec, ok := in[m.ID]; !ok || rec.Version != m.Version {
			in[m.ID] = recordFromManifest(m)
		}
	}
	for id := range in {
		if !present[id] {
			delete(in, id)
		}
	}
	return in, nil
}

func recordFromManifest(m plugin.Manifest) Record {
	caps := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = string(c)
	}
	return Record{Source: SourceUnknown, Version: m.Version, Capabilities: caps, Requires: m.Requires}
}

// Save writes the records atomically.
func (in Installed) Save(root string) error {
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return fail(KindDisk, err, "")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fail(KindDisk, err, "")
	}
	tmp, err := os.CreateTemp(root, ".installed-*.json")
	if err != nil {
		return fail(KindDisk, err, "")
	}
	_, err = tmp.Write(data)
	if serr := tmp.Sync(); err == nil {
		err = serr
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(root, InstalledName))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fail(KindDisk, err, "")
	}
	return nil
}
