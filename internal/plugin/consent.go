package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ContentDigest is the sha256 of what a user approves when they approve a
// plugin: its manifest, which carries the asks, and its resolved entry point,
// which is the code. Nothing else in the directory is covered. State, caches
// and the store's records change on every run, and digesting them would make a
// stable approval impossible.
//
// Each file contributes "path \0 length \0 bytes", sorted by its slash-separated
// path inside the plugin directory, so the result does not depend on the order
// files are read in and bytes cannot move from one entry into the next.
func ContentDigest(m Manifest) (string, error) {
	realDir, err := filepath.EvalSymlinks(m.Dir)
	if err != nil {
		return "", fmt.Errorf("plugin: digest %s: %w", m.Dir, err)
	}
	paths := []string{filepath.Join(realDir, ManifestName)}
	realExec, err := filepath.EvalSymlinks(m.ExecPath)
	if err != nil {
		return "", fmt.Errorf("plugin: digest %s: %w", m.ID, err)
	}
	if !slices.Contains(paths, realExec) {
		paths = append(paths, realExec)
	}
	entries := make([]digestEntry, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(realDir, path)
		if err != nil {
			return "", fmt.Errorf("plugin: digest %s: %w", m.ID, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("plugin: digest %s: %w", m.ID, err)
		}
		entries = append(entries, digestEntry{path: filepath.ToSlash(rel), data: data})
	}
	return contentDigestOf(entries), nil
}

type digestEntry struct {
	path string
	data []byte
}

func contentDigestOf(entries []digestEntry) string {
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b digestEntry) int { return strings.Compare(a.path, b.path) })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.path))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(e.data))))
		h.Write([]byte{0})
		h.Write(e.data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ConsentName is the ledger of approvals, kept in the managed root beside the
// store's installed.json. The store's worker is its only writer.
const ConsentName = "consent.json"

// ConsentPath is the approval ledger for a managed root. The store's worker is
// its only writer; the plugin host reads it at every start.
func ConsentPath(managedRoot string) string { return filepath.Join(managedRoot, ConsentName) }

// consentSchema is the ledger layout this shell writes and reads.
const consentSchema = 1

// ConsentRecord is one approval: the digest the user approved, and the asks
// that digest carried, kept so a later change can be described as from → to.
type ConsentRecord struct {
	ContentSHA256 string    `json:"content_sha256"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	Requires      []string  `json:"requires,omitempty"`
	ApprovedAt    time.Time `json:"approved_at,omitzero"`
}

// ConsentLedger holds every approval, keyed by the root a copy was approved in
// and its plugin ID: approving a user copy says nothing about the managed copy
// it shadows.
type ConsentLedger struct {
	Schema  int                      `json:"schema"`
	Plugins map[string]ConsentRecord `json:"plugins"`
}

func consentKey(source Source, id string) string { return string(source) + "/" + id }

// LoadConsent reads the ledger at path. An absent file is an empty ledger. A
// ledger this shell did not write, or cannot read, is an error: the caller
// decides whether that means "nothing recorded".
func LoadConsent(path string) (ConsentLedger, error) {
	l := ConsentLedger{Schema: consentSchema, Plugins: map[string]ConsentRecord{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, fmt.Errorf("plugin: read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var read ConsentLedger
	if err := dec.Decode(&read); err != nil {
		return l, fmt.Errorf("plugin: %s: %w", path, err)
	}
	if read.Schema != consentSchema {
		return l, fmt.Errorf("plugin: %s has schema %d; this shell reads %d", path, read.Schema, consentSchema)
	}
	maps.Copy(l.Plugins, read.Plugins)
	return l, nil
}

// Lookup returns the approval recorded for one copy of a plugin.
func (l ConsentLedger) Lookup(source Source, id string) (ConsentRecord, bool) {
	rec, ok := l.Plugins[consentKey(source, id)]
	return rec, ok
}

// Set records an approval, replacing any earlier one for the same copy.
func (l *ConsentLedger) Set(source Source, id string, rec ConsentRecord) {
	if l.Plugins == nil {
		l.Plugins = map[string]ConsentRecord{}
	}
	l.Plugins[consentKey(source, id)] = rec
}

// Delete forgets the approval for one copy.
func (l ConsentLedger) Delete(source Source, id string) { delete(l.Plugins, consentKey(source, id)) }

// Save writes the ledger atomically, so a reader never sees half of it.
func (l ConsentLedger) Save(path string) error {
	l.Schema = consentSchema
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".consent-*.json")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if serr := tmp.Sync(); err == nil {
		err = serr
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// ConsentState is where one plugin copy stands against its recorded approval.
type ConsentState string

const (
	// ConsentExempt is a system plugin: the package manager installed it as
	// root, and its tree is guarded there, not here.
	ConsentExempt ConsentState = "exempt"
	// ConsentApproved is a copy whose bytes match the approval on record.
	ConsentApproved ConsentState = "approved"
	// ConsentUnrecorded is a copy with no approval on record, which is every
	// hand-placed plugin enabled before approvals were recorded. It keeps the
	// grant it had, so nothing stops working, and the manager asks once.
	ConsentUnrecorded ConsentState = "unrecorded"
	// ConsentChanged is a copy whose bytes no longer match what was approved.
	// It starts with no capabilities until the user approves it again.
	ConsentChanged ConsentState = "changed"
)

// ConsentVerdict is the answer for one start.
type ConsentVerdict struct {
	State ConsentState
	// Digest is the copy's current content digest; empty for an exempt copy
	// or one that could not be read.
	Digest string
	// Approved is the record the digest was compared against, if any.
	Approved *ConsentRecord
}

// Grants reports whether this start may hold the capabilities it asks for.
func (v ConsentVerdict) Grants() bool { return v.State != ConsentChanged }

// ConsentLookup returns the approval recorded for one copy of a plugin.
type ConsentLookup func(source Source, id string) (ConsentRecord, bool)

// VerifyConsent compares a copy's bytes with its recorded approval. A copy
// whose bytes cannot be read is treated as changed: the start that follows
// will fail on the same missing file, and it must not fail holding a grant.
func VerifyConsent(m Manifest, source Source, lookup ConsentLookup) ConsentVerdict {
	if source == SourceSystem {
		return ConsentVerdict{State: ConsentExempt}
	}
	digest, err := ContentDigest(m)
	if err != nil {
		return ConsentVerdict{State: ConsentChanged}
	}
	rec, ok := lookup(source, m.ID)
	if !ok {
		return ConsentVerdict{State: ConsentUnrecorded, Digest: digest}
	}
	v := ConsentVerdict{State: ConsentChanged, Digest: digest, Approved: &rec}
	if rec.ContentSHA256 != "" && rec.ContentSHA256 == digest {
		v.State = ConsentApproved
	}
	return v
}

// RecordFor is the approval of a copy as it is now.
func RecordFor(m Manifest, digest string, at time.Time) ConsentRecord {
	caps := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		caps[i] = string(c)
	}
	return ConsentRecord{ContentSHA256: digest, Capabilities: caps, Requires: slices.Clone(m.Requires), ApprovedAt: at}
}
