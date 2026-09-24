package store

import (
	"cmp"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin"
	v1 "github.com/Nomadcxx/sysc-shell/plugin/v1"
)

const (
	// Schema is the one catalog schema this shell reads. It changes only for
	// a breaking change; new fields are additive and ignored by older shells.
	Schema = 1

	MaxCatalogBytes       = 4 << 20
	MaxAssetBytes   int64 = 64 << 20

	CategoryOther = "other"
)

// Categories is the closed set a catalog row may name: the DMS registry's
// working set with its duplicate spellings merged.
var Categories = []string{
	"utilities", "monitoring", "system", "appearance", "productivity",
	"media", "audio", "networking", "weather", "finance", "social",
}

var (
	versionPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	assetKeyPattern = regexp.MustCompile(`^linux-[a-z0-9]+$`)
)

type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Screenshot struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type Requires struct {
	Commands []string `json:"commands"`
}

// Release is one installable version of a plugin.
type Release struct {
	Version      string           `json:"version"`
	Protocol     v1.Version       `json:"protocol"`
	Capabilities []string         `json:"capabilities"`
	Requires     Requires         `json:"requires"`
	Assets       map[string]Asset `json:"assets"`
	ReleaseNotes string           `json:"release_notes,omitempty"`
}

// Entry is one catalog row. Its embedded Release is the newest version; older
// ones in Releases exist for hosts below that version's protocol.
type Entry struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Author          string      `json:"author"`
	Description     string      `json:"description"`
	LongDescription string      `json:"long_description,omitempty"`
	Category        string      `json:"category"`
	License         string      `json:"license,omitempty"`
	Homepage        string      `json:"homepage,omitempty"`
	Screenshot      *Screenshot `json:"screenshot,omitempty"`
	AddedAt         time.Time   `json:"added_at,omitzero"`
	UpdatedAt       time.Time   `json:"updated_at,omitzero"`
	Deprecated      bool        `json:"deprecated,omitempty"`
	Release
	Releases []Release `json:"releases,omitempty"`
}

// RowError is one catalog row that did not decode or validate.
type RowError struct {
	Index int
	ID    string
	Err   error
}

// Catalog is one decoded source. A bad row is rejected alone, because one
// author's mistake must not hide every other plugin in the source.
type Catalog struct {
	Entries  []Entry
	Rejected []RowError
}

// Decode parses and validates a catalog. Unknown fields are ignored, unlike the
// strict plugin wire decode: a catalog is read by shells of every age.
func Decode(data []byte) (Catalog, error) {
	if len(data) > MaxCatalogBytes {
		return Catalog{}, fail(KindCatalog, nil, "larger than %d bytes", MaxCatalogBytes)
	}
	var doc struct {
		Schema  int               `json:"schema"`
		Plugins []json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Catalog{}, fail(KindCatalog, err, "")
	}
	if doc.Schema != Schema {
		return Catalog{}, fail(KindSchema, nil, "schema %d; this shell reads %d", doc.Schema, Schema)
	}

	type row struct {
		index int
		entry Entry
	}
	var cat Catalog
	var rows []row
	count := map[string]int{}
	for i, raw := range doc.Plugins {
		var e Entry
		err := json.Unmarshal(raw, &e)
		if err == nil {
			err = e.validate()
		}
		if err != nil {
			cat.Rejected = append(cat.Rejected, RowError{Index: i, ID: e.ID, Err: err})
			continue
		}
		count[e.ID]++
		rows = append(rows, row{i, e})
	}
	for _, r := range rows {
		if count[r.entry.ID] > 1 {
			cat.Rejected = append(cat.Rejected, RowError{Index: r.index, ID: r.entry.ID,
				Err: fail(KindCatalog, nil, "id %q appears more than once", r.entry.ID)})
			continue
		}
		cat.Entries = append(cat.Entries, r.entry)
	}
	return cat, nil
}

func (e *Entry) validate() error {
	switch {
	case !v1.ValidPluginID(e.ID):
		return fail(KindCatalog, nil, "%q is not a plugin id", e.ID)
	case e.Name == "" || e.Author == "" || e.Description == "":
		return fail(KindCatalog, nil, "name, author and description are required")
	case e.Category == "":
		return fail(KindCatalog, nil, "category is required")
	case e.Homepage != "" && !isHTTPS(e.Homepage):
		return fail(KindCatalog, nil, "homepage %q is not an https URL", e.Homepage)
	}
	// A newer catalog may name a category this shell has never heard of; it
	// shows as Other rather than failing the row.
	if !slices.Contains(Categories, e.Category) {
		e.Category = CategoryOther
	}
	if s := e.Screenshot; s != nil {
		if err := checkFetchURL(s.URL); err != nil {
			return err
		}
		if !sha256Pattern.MatchString(s.SHA256) {
			return fail(KindCatalog, nil, "screenshot sha256 %q is not 64 lower-case hex digits", s.SHA256)
		}
	}
	if err := e.Release.validate(); err != nil {
		return err
	}
	for i := range e.Releases {
		if err := e.Releases[i].validate(); err != nil {
			return fail(KindCatalog, err, "releases[%d]", i)
		}
	}
	return nil
}

func (r Release) validate() error {
	switch {
	case !versionPattern.MatchString(r.Version):
		return fail(KindCatalog, nil, "version %q is not MAJOR.MINOR.PATCH", r.Version)
	case r.Protocol.Major < 1 || r.Protocol.Minor < 0:
		return fail(KindCatalog, nil, "protocol %d.%d is not a plugin protocol", r.Protocol.Major, r.Protocol.Minor)
	case len(r.Assets) == 0:
		return fail(KindCatalog, nil, "version %s has no assets", r.Version)
	case r.ReleaseNotes != "" && !isHTTPS(r.ReleaseNotes):
		return fail(KindCatalog, nil, "release notes %q is not an https URL", r.ReleaseNotes)
	}
	for key, a := range r.Assets {
		if !assetKeyPattern.MatchString(key) {
			return fail(KindCatalog, nil, "asset key %q is not linux-<arch>", key)
		}
		if err := checkFetchURL(a.URL); err != nil {
			return err
		}
		if !sha256Pattern.MatchString(a.SHA256) {
			return fail(KindCatalog, nil, "asset sha256 %q is not 64 lower-case hex digits", a.SHA256)
		}
		if a.Size < 1 || a.Size > MaxAssetBytes {
			return fail(KindCatalog, nil, "asset size %d is outside 1..%d", a.Size, MaxAssetBytes)
		}
	}
	return nil
}

// checkFetchURL admits https anywhere and plain http only to this machine. The
// sha256 pins content either way; the loopback exception lets authors and
// tests serve assets locally without a certificate.
func checkFetchURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fail(KindCatalog, err, "%q is not a URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fail(KindCatalog, nil, "%q must use https", raw)
}

func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

type Compat string

const (
	Compatible   Compat = "compatible"
	HeldBack     Compat = "held back"
	Incompatible Compat = "incompatible"
)

// Resolution is what this host would install from one entry.
type Resolution struct {
	// Release is nil when nothing is installable.
	Release *Release
	Compat  Compat
	// Needs is the newest release's protocol, which the manager names when
	// the row is held back or incompatible.
	Needs v1.Version
}

// Resolve picks the newest release this host can run on arch.
func Resolve(e Entry, arch string) Resolution {
	key := "linux-" + arch
	candidates := append([]Release{e.Release}, e.Releases...)
	var best *Release
	for i := range candidates {
		r := &candidates[i]
		if !plugin.HostSupports(r.Protocol) {
			continue
		}
		if _, ok := r.Assets[key]; !ok {
			continue
		}
		if best == nil || Newer(r.Version, best.Version) {
			best = r
		}
	}
	res := Resolution{Release: best, Needs: e.Protocol}
	switch {
	case best == nil:
		res.Compat = Incompatible
	case best.Version == e.Version:
		res.Compat = Compatible
	default:
		res.Compat = HeldBack
	}
	return res
}

// Newer reports whether version a is later than b. Anything that is not
// MAJOR.MINOR.PATCH is never newer, so a malformed installed record cannot
// manufacture an update.
func Newer(a, b string) bool {
	if !versionPattern.MatchString(a) || !versionPattern.MatchString(b) {
		return false
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range 3 {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if c := cmp.Compare(x, y); c != 0 {
			return c > 0
		}
	}
	return false
}

// sameSet compares two string lists as sets.
func sameSet(a, b []string) bool {
	return slices.Equal(slices.Compact(slices.Sorted(slices.Values(a))), slices.Compact(slices.Sorted(slices.Values(b))))
}
