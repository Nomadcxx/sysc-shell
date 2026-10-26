package dep

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Version is a Go module version: a semantic version with the optional
// pre-release and build metadata a module path can carry.
type Version struct {
	// Major, Minor and Patch are the release numbers.
	Major, Minor, Patch int
	// Pre is the pre-release identifier. It is empty for a release version.
	Pre string
	// Build is the build metadata. It never takes part in a comparison.
	Build string
	// Pseudo is true for a pseudo-version: the form the go command writes
	// when it resolves a commit that has no tag of its own.
	Pseudo bool
}

// ParseVersion reads a module version.
//
// It accepts the two forms a go.mod carries: a release version, and the
// pseudo-version the go command writes for an untagged commit. Anything else
// is an error, because a version the tool cannot order is a version the tool
// must not count a drift for.
func ParseVersion(text string) (Version, error) {
	raw := strings.TrimSpace(text)
	if !strings.HasPrefix(raw, "v") {
		return Version{}, fmt.Errorf("%q is not a module version: it has no leading v", raw)
	}
	raw = raw[1:]
	var v Version
	if i := strings.IndexByte(raw, '+'); i >= 0 {
		v.Build = raw[i+1:]
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '-'); i >= 0 {
		v.Pre = raw[i+1:]
		raw = raw[:i]
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("%q is not a module version: it has no major, minor and patch", text)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("%q is not a module version: %q is not a release number", text, p)
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	if v.Pre != "" {
		v.Pseudo = isPseudoPre(v.Pre)
	}
	return v, nil
}

// isPseudoPre reports whether a pre-release identifier is the one the go
// command writes for a commit with no tag of its own.
//
// There are two shapes: the patch number with a zero minor and the commit
// time and hash, as in v0.3.5-0.20260729084153-ddb7ff96ad4d, and the zero
// release with the commit time and hash, as in
// v0.0.0-20211118161319-6a13c67c3ce4.
func isPseudoPre(pre string) bool {
	rest := strings.TrimPrefix(pre, "0.")
	time, hash, found := strings.Cut(rest, "-")
	if !found || len(time) != 14 || len(hash) != 12 {
		return false
	}
	for _, r := range time {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range hash {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// IsPseudoVersion reports whether a version string is a pseudo-version: a
// commit with no release tag of its own.
func IsPseudoVersion(text string) bool {
	v, err := ParseVersion(text)
	if err != nil {
		return false
	}
	return v.Pseudo
}

// String writes the version back the way go.mod writes it.
func (v Version) String() string {
	out := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		out += "-" + v.Pre
	}
	if v.Build != "" {
		out += "+" + v.Build
	}
	return out
}

// Compare orders two versions: -1 when v is older, 0 when they are the same
// and 1 when v is newer.
//
// The build metadata takes no part in the order, as the semantic version
// specification requires. A pre-release is older than the release it leads
// to.
func (v Version) Compare(o Version) int {
	for _, pair := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.Pre == "" && o.Pre == "":
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	}
	return comparePre(v.Pre, o.Pre)
}

// comparePre orders two pre-release identifiers by the rules of the semantic
// version specification: the identifiers are compared one dot-separated field
// at a time, a numeric field is compared as a number and is older than an
// alphanumeric one, and a shorter identifier is older when every field it
// shares is equal.
func comparePre(a, b string) int {
	af := strings.Split(a, ".")
	bf := strings.Split(b, ".")
	for i := 0; i < len(af) && i < len(bf); i++ {
		if af[i] == bf[i] {
			continue
		}
		an, aerr := strconv.Atoi(af[i])
		bn, berr := strconv.Atoi(bf[i])
		switch {
		case aerr == nil && berr == nil:
			if an < bn {
				return -1
			}
			return 1
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		default:
			if af[i] < bf[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(af) < len(bf):
		return -1
	case len(af) > len(bf):
		return 1
	}
	return 0
}

// CompareVersions orders two version strings. A version that cannot be read
// sorts before every version that can, so a malformed pin never reads as the
// newest thing in the tree.
func CompareVersions(a, b string) int {
	av, aerr := ParseVersion(a)
	bv, berr := ParseVersion(b)
	switch {
	case aerr != nil && berr != nil:
		return strings.Compare(a, b)
	case aerr != nil:
		return -1
	case berr != nil:
		return 1
	}
	return av.Compare(bv)
}

// SortVersions orders version strings oldest first.
func SortVersions(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i], versions[j]) < 0
	})
}
