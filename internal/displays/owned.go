package displays

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sblinch/kdl-go"
)

// OwnedFileName is the shell's record of what it wrote. It sits beside the
// managed file rather than in the shell JSON, because the shell JSON is
// configuration the user edits and this is bookkeeping about bytes on disk.
const OwnedFileName = ManagedFileName + ".owned.json"

// backupName is the single non-overwriting copy of the original root. The
// timestamp keeps a second setup attempt from overwriting the first backup,
// and refusing to overwrite is the point: a backup that can be replaced is not
// a backup.
const backupName = ManagedFileName + ".root.bak"

// Owned is the established ownership record. Every hash is what the shell last
// wrote; anything else on disk is an external edit, and the rule is to stop
// rather than overwrite it.
type Owned struct {
	Version int `json:"version"`
	// Root is the symlink-resolved configuration file. Writes go here so the
	// user's symlink survives.
	Root string `json:"root"`
	// RootPath is the path as configured, kept so the explanation can show
	// what the user actually pointed at.
	RootPath string `json:"root_path"`
	// RootSHA256 is the root content including the owned include line.
	RootSHA256 string `json:"root_sha256"`
	// Managed is the owned output file.
	Managed string `json:"managed"`
	// ManagedSHA256 is the owned file's content as of the last write.
	ManagedSHA256 string `json:"managed_sha256"`
	// Fingerprint is the resolved graph at the last accepted write, so a
	// change anywhere in the includes is visible on the next check.
	Fingerprint string `json:"fingerprint"`
	// Backup is the one-time copy of the root from before the first install.
	Backup string `json:"backup"`
	// Connectors are the outputs the managed file is authoritative for.
	Connectors []string `json:"connectors"`
}

const ownedVersion = 1

// ErrExternalEdit reports bytes the shell did not write. It is a distinct
// error because the only correct response is to offer adoption, never to
// retry the write.
var ErrExternalEdit = errors.New("displays: file was edited outside the shell")

// ErrNotSetUp reports that no ownership record exists yet.
var ErrNotSetUp = errors.New("displays: display settings are not set up")

// OwnershipPath is the record's path beside root.
func OwnershipPath(root string) string {
	return filepath.Join(filepath.Dir(root), OwnedFileName)
}

// LoadOwnership reads the record beside root.
func LoadOwnership(root string) (Owned, error) {
	path := OwnershipPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Owned{}, fmt.Errorf("%w: %s", ErrNotSetUp, path)
	}
	if err != nil {
		return Owned{}, fmt.Errorf("displays: read %s: %w", path, err)
	}
	if len(data) > maxFileBytes {
		return Owned{}, fmt.Errorf("displays: %s: %w", path, errResourceLimit)
	}
	var own Owned
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&own); err != nil {
		return Owned{}, fmt.Errorf("displays: parse %s: %w", path, err)
	}
	if own.Version != ownedVersion {
		return Owned{}, fmt.Errorf("displays: %s has unsupported version %d", path, own.Version)
	}
	return own, nil
}

// Store writes the record atomically with the same discipline the managed file
// uses: same-directory temp, fsync, rename, directory sync.
func (o Owned) Store(root string) error {
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return fmt.Errorf("displays: encode ownership: %w", err)
	}
	data = append(data, '\n')
	path := OwnershipPath(root)
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return err
	}
	return nil
}

// rootTarget resolves root to the file writes must replace. A symlink is
// resolved and the symlink itself preserved, so a user who links
// config.kdl into a dotfiles repository keeps that link.
func rootTarget(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("displays: resolve %s: %w", root, err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("displays: stat %s: %w", abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("displays: %s is a directory", abs)
	}
	// A root reached through a symlink is rewritten at its target, so a user's
	// link keeps working and the replace stays atomic on the real file.
	if n, err := filepath.EvalSymlinks(abs); err == nil && n != abs {
		resolved, err := os.Lstat(n)
		if err != nil {
			return "", fmt.Errorf("displays: stat %s: %w", n, err)
		}
		// A hard-linked root cannot be replaced by rename without breaking
		// the other name, and copying in place would not be atomic. Refuse
		// instead of guessing which name is the configuration.
		if err := checkReplaceable(n, resolved); err != nil {
			return "", err
		}
		return n, nil
	}
	if err := checkReplaceable(abs, info); err != nil {
		return "", err
	}
	return abs, nil
}

func checkReplaceable(path string, info os.FileInfo) error {
	if n, ok := info.Sys().(*syscall.Stat_t); ok && n.Nlink > 1 {
		return fmt.Errorf("displays: %s has %d hard links; replace it directly instead", path, n.Nlink)
	}
	if info.Mode().Perm()&0o200 == 0 {
		return fmt.Errorf("displays: %s is not writable", path)
	}
	return nil
}

// checkDirWritable is the directory half of the same question: a rename needs
// a writable directory even when the file itself is not.
func checkDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("displays: create %s: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".displays-probe-*")
	if err != nil {
		return fmt.Errorf("displays: %s is not writable: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// Setup performs the two-file bootstrap, in the order that makes an
// interruption safe. The owned file is created first and is comment-only, so
// a crash between the two steps leaves a root whose output behaviour is
// unchanged and a retry sees an existing owned file it may legitimately use.
// Only then is the single include line prepended.
//
// root must be the path the user confirmed; it is never guessed.
func Setup(root string) (Owned, error) {
	target, err := rootTarget(root)
	if err != nil {
		return Owned{}, err
	}
	dir := filepath.Dir(target)
	if err := checkDirWritable(dir); err != nil {
		return Owned{}, err
	}
	managed := ManagedPath(target)

	// Setting up again after a successful setup is not an error: the shell
	// re-runs discovery on every start, and a refusal here would leave the
	// owned file looking like a stranger's the next time.
	if existing, err := LoadOwnership(root); err == nil &&
		(existing.Root == target || existing.RootPath == root) {
		if err := CheckOwnership(root, existing); err == nil {
			return existing, nil
		}
	}

	original, err := os.ReadFile(target)
	if err != nil {
		return Owned{}, fmt.Errorf("displays: read %s: %w", target, err)
	}
	if len(original) > maxFileBytes {
		return Owned{}, fmt.Errorf("displays: %s: %w", target, errResourceLimit)
	}
	if bytesHaveInclude(original, managed) {
		return Owned{}, fmt.Errorf("displays: %s already includes %s but no ownership record exists; "+
			"remove the include or the file after checking it, then set up again", target, ManagedFileName)
	}

	backup, err := writeBackup(dir, target, original)
	if err != nil {
		return Owned{}, err
	}

	comment := commentOnly(managed)
	existing, err := os.ReadFile(managed)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := writeFileAtomic(managed, comment, 0o600); err != nil {
			return Owned{}, err
		}
	case err != nil:
		return Owned{}, fmt.Errorf("displays: read %s: %w", managed, err)
	default:
		// An unrelated file already sits at the owned path. Refuse rather
		// than take a name the user may be using for their own text.
		if Hash(existing) != Hash(comment) {
			return Owned{}, fmt.Errorf("displays: %s already exists and is not display settings the shell wrote", managed)
		}
	}

	line, err := IncludeLine(managed)
	if err != nil {
		return Owned{}, err
	}
	updated := append(append([]byte{}, line...), original...)
	if err := writeFileAtomic(target, updated, fileMode(target)); err != nil {
		return Owned{}, err
	}
	if err := syncDir(dir); err != nil {
		return Owned{}, err
	}
	own := Owned{
		Version:       ownedVersion,
		Root:          target,
		RootPath:      root,
		RootSHA256:    Hash(updated),
		Managed:       managed,
		ManagedSHA256: Hash(comment),
		Backup:        backup,
	}
	// The fingerprint is the user's graph as the shell just left it. Every
	// later check compares against it, so an edit to any file the user's
	// configuration includes is visible before a publication overwrites it.
	graph, err := LoadGraph(target)
	if err != nil {
		return own, err
	}
	own.Fingerprint = graph.UserFingerprint(managed)
	if err := own.Store(root); err != nil {
		return own, err
	}
	return own, nil
}

func fileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o600
}

// commentOnly is the owned file before it owns anything. It is a comment so
// that the file's mere existence changes no output, which is what makes the
// include line safe to add before the content is real.
func commentOnly(managed string) []byte {
	return []byte(fmt.Sprintf("// %s is written by sysc-shell.\n// Display settings kept here take precedence over the blocks they were cloned from.\n",
		ManagedFileName))
}

func bytesHaveInclude(root []byte, managed string) bool {
	for _, line := range strings.Split(string(root), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, includeLinePrefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, includeLinePrefix))
		if strings.Trim(rest, `"`) == managed {
			return true
		}
	}
	return false
}

func writeBackup(dir, target string, original []byte) (string, error) {
	backup := filepath.Join(dir, fmt.Sprintf("%s.%d", backupName, time.Now().Unix()))
	if _, err := os.Lstat(backup); err == nil {
		return "", fmt.Errorf("displays: backup %s already exists; refusing to overwrite it", backup)
	}
	if err := writeFileAtomic(backup, original, fileMode(target)); err != nil {
		return "", err
	}
	return backup, nil
}

// CheckOwnership verifies that the root and the owned file still hold the bytes
// the shell wrote, and that the whole resolved graph is unchanged. A mismatch
// is an external edit: the caller must offer adoption, not write.
func CheckOwnership(root string, own Owned) error {
	for _, f := range []struct{ path, want string }{
		{own.Root, own.RootSHA256},
		{own.Managed, own.ManagedSHA256},
	} {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrExternalEdit, f.path, err)
		}
		if Hash(data) != f.want {
			return fmt.Errorf("%w: %s", ErrExternalEdit, f.path)
		}
	}
	graph, err := LoadGraph(own.Root)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExternalEdit, err)
	}
	if got := graph.UserFingerprint(own.Managed); got != own.Fingerprint {
		return fmt.Errorf("%w: the included files under %s changed", ErrExternalEdit, filepath.Dir(own.Root))
	}
	return nil
}

// Adopting an externally edited owned file is the only way past an external
// edit, and it is explicit: the user has seen the file, the graph parsed and
// validated, and now the new bytes become the shell's baseline.
func Adopt(root string, own Owned) (Owned, error) {
	rootData, err := os.ReadFile(own.Root)
	if err != nil {
		return own, fmt.Errorf("displays: read %s: %w", own.Root, err)
	}
	// A managed file the user deleted is adopted back as the comment-only
	// bootstrap, so removing it is a reset rather than a dead end.
	managedData, err := os.ReadFile(own.Managed)
	switch {
	case errors.Is(err, os.ErrNotExist):
		managedData = commentOnly(own.Managed)
		if err := writeFileAtomic(own.Managed, managedData, 0o600); err != nil {
			return own, err
		}
	case err != nil:
		return own, fmt.Errorf("displays: read %s: %w", own.Managed, err)
	}
	graph, err := LoadGraph(own.Root)
	if err != nil {
		return own, err
	}
	owned, err := managedConnectors(own.Managed)
	if err != nil {
		return own, err
	}

	own.RootSHA256 = Hash(rootData)
	own.ManagedSHA256 = Hash(managedData)
	own.Fingerprint = graph.UserFingerprint(own.Managed)
	own.Connectors = owned
	if err := own.Store(root); err != nil {
		return own, err
	}
	return own, nil
}

// PublishManaged replaces the owned file's content. It refuses when the file
// holds bytes the shell did not write, so a conflict visible before
// publication is never overwritten.
func PublishManaged(own *Owned, data []byte) error {
	graph, err := LoadGraph(own.Root)
	if err != nil {
		return err
	}
	if graph.UserFingerprint(own.Managed) != own.Fingerprint {
		return fmt.Errorf("%w: the included files under %s changed", ErrExternalEdit, filepath.Dir(own.Root))
	}
	current, err := os.ReadFile(own.Managed)
	if err != nil {
		return fmt.Errorf("displays: read %s: %w", own.Managed, err)
	}
	if Hash(current) != own.ManagedSHA256 {
		return fmt.Errorf("%w: %s", ErrExternalEdit, own.Managed)
	}
	if err := writeFileAtomic(own.Managed, data, 0o600); err != nil {
		return err
	}
	owned, err := managedConnectors(own.Managed)
	if err != nil {
		return err
	}
	own.ManagedSHA256 = Hash(data)
	own.Connectors = owned
	own.Fingerprint = graph.UserFingerprint(own.Managed)
	return nil
}

// managedConnectors lists the output names the owned file declares. It parses
// the owned file alone, not the root graph: the root also contains the user's
// own blocks, and the shell does not claim those.
func managedConnectors(managed string) ([]string, error) {
	data, err := os.ReadFile(managed)
	if err != nil {
		return nil, fmt.Errorf("displays: read %s: %w", managed, err)
	}
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("displays: parse %s: %w", managed, err)
	}
	var out []string
	for _, n := range doc.Nodes {
		if name, ok := n.Name.Value.(string); ok && name == "output" {
			out = append(out, text(n.Arguments[0]))
		}
	}
	return out, nil
}

// writeFileAtomic is the shell's one write path for these files: a temp in the
// same directory so the rename cannot cross a filesystem, fsync before the
// rename so the content is durable, then the rename, then a directory sync so
// the new name itself is durable.
var atomicReplace = os.Rename

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".displays-*.tmp")
	if err != nil {
		return fmt.Errorf("displays: create temp in %s: %w", dir, err)
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return fmt.Errorf("displays: chmod temp: %w", err)
	}
	// A replaced file keeps its group. The temp file takes the process's
	// group, which differs from the user's own when the shell runs under
	// newgrp. Best effort: a group this account is not in cannot be kept.
	if info, err := os.Stat(path); err == nil {
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			_ = f.Chown(-1, int(st.Gid))
		}
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("displays: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("displays: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("displays: close temp: %w", err)
	}
	if err := atomicReplace(tmp, path); err != nil {
		return fmt.Errorf("displays: replace %s: %w", path, err)
	}
	done = true
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("displays: open %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("displays: sync %s: %w", dir, err)
	}
	return nil
}

// fileDigest is the hash of a file's current content, or an empty string when
// it cannot be read. Slice C's decision table needs to compare an observed
// managed file against the old and candidate hashes.
func fileDigest(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
