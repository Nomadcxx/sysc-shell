// Package store reads plugin catalogs from git-hosted sources and installs,
// updates, rolls back and removes plugins from their pinned release assets.
package store

import (
	"errors"
	"fmt"
)

// Kind names why a store operation failed. It is what the manager shows on
// the row the failure belongs to, so each value reads as a cause.
type Kind string

const (
	KindUnreachable Kind = "source unreachable"
	KindGitMissing  Kind = "git not found on PATH"
	KindGitTimeout  Kind = "git timed out"
	KindSchema      Kind = "catalog schema unsupported"
	KindCatalog     Kind = "catalog invalid"
	KindNoAsset     Kind = "no release for this machine"
	KindTooLarge    Kind = "download too large"
	KindChecksum    Kind = "sha256 mismatch"
	KindArchive     Kind = "archive rejected"
	KindManifest    Kind = "manifest mismatch"
	KindConsent     Kind = "consent required"
	KindNoPrevious  Kind = "no previous version"
	KindNotListed   Kind = "not in any enabled source"
	KindBusy        Kind = "store busy"
	KindDisk        Kind = "disk error"
)

// Error is every failure the store reports.
type Error struct {
	Kind   Kind
	Detail string
	Err    error
}

func (e *Error) Error() string {
	msg := string(e.Kind)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

func fail(k Kind, err error, format string, args ...any) *Error {
	return &Error{Kind: k, Detail: fmt.Sprintf(format, args...), Err: err}
}

// KindOf returns the kind of a store error, or "" for any other error.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}
