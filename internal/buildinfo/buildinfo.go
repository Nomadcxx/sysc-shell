// Package buildinfo owns the identity a build reports about itself.
//
// Version and Commit are set with -ldflags -X by the release workflow. The
// defaults are the honest answer for a build nobody stamped, so an unstamped
// binary reports "development" instead of a tag-shaped string it cannot prove.
package buildinfo

var (
	// Version is the released version, without a leading v.
	Version = "development"
	// Commit is the short commit sha the binary was built from.
	Commit = "unknown"
)

// Line is the one rendering of the build stamp. Every consumer prints this
// string verbatim so the flag, the status payload and installer output agree.
func Line() string {
	return "sysc-shell " + Version + " (" + Commit + ")"
}
