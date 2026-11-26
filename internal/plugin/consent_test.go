package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func digestOf(t *testing.T, dir string) string {
	t.Helper()
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ContentDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	if hex, ok := strings.CutPrefix(d, TreeDigestPrefix); !ok || len(hex) != 64 {
		t.Fatalf("digest %q is not a tree sha256", d)
	}
	return d
}

func TestContentDigestCoversCodeAndAsksOnly(t *testing.T) {
	t.Parallel()
	a := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	b := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	if digestOf(t, a) != digestOf(t, b) {
		t.Fatal("identical plugins digest differently")
	}

	// Every file an interpreter or the entry point may load is part of what
	// was approved, not only the manifest and entry point (audit sysc-2357).
	base := digestOf(t, a)
	helper := filepath.Join(a, "depth_helper.py")
	if err := os.WriteFile(helper, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withHelper := digestOf(t, a)
	if withHelper == base {
		t.Fatal("digest ignores a sibling file")
	}
	if err := os.WriteFile(helper, []byte("import os; os.system('x')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, a) == withHelper {
		t.Fatal("digest ignores a changed sibling file")
	}
	if err := os.Remove(helper); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, a) != base {
		t.Fatal("removing the sibling did not restore the digest")
	}

	// A symlink is recorded by its target, never followed out of the tree.
	if err := os.Symlink("/etc/hostname", filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}
	linked := digestOf(t, a)
	if linked == base {
		t.Fatal("digest ignores a symlink")
	}
	if err := os.Remove(filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, a) == linked {
		t.Fatal("digest ignores where a symlink points")
	}
	if err := os.Remove(filepath.Join(a, "link")); err != nil {
		t.Fatal(err)
	}

	// A changed entry point is a changed plugin.
	if err := os.WriteFile(filepath.Join(b, "bin/sysc-plugin-timer"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, b) == base {
		t.Fatal("digest ignores the entry point's bytes")
	}

	// So is a changed manifest: the asks are part of what was approved.
	c := writePlugin(t, edit(t, "capabilities", []string{"panels"}), "bin/sysc-plugin-timer")
	if digestOf(t, c) == base {
		t.Fatal("digest ignores the manifest's bytes")
	}
}

func TestContentDigestFramingCannotBeForged(t *testing.T) {
	t.Parallel()
	// Moving bytes between the two framed entries must change the digest:
	// the length prefix keeps one entry's tail from reading as the next
	// entry's head.
	one := contentDigestOf([]digestEntry{{"a", []byte("xy")}, {"b", []byte("z")}})
	two := contentDigestOf([]digestEntry{{"a", []byte("x")}, {"b", []byte("yz")}})
	if one == two {
		t.Fatal("framing lets bytes move between entries")
	}
}

func TestContentDigestRefusesAMissingEntryPoint(t *testing.T) {
	t.Parallel()
	dir := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(m.ExecPath); err != nil {
		t.Fatal(err)
	}
	if d, err := ContentDigest(m); err == nil {
		t.Fatalf("missing entry point digested to %q", d)
	}
}

func TestConsentVerdict(t *testing.T) {
	t.Parallel()
	dir := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	digest := digestOf(t, dir)
	approved := ConsentRecord{ContentSHA256: digest}
	for _, tc := range []struct {
		name   string
		source Source
		rec    ConsentRecord
		have   bool
		want   ConsentState
		grants bool
	}{
		{"system plugins are trusted by the package manager", SourceSystem, ConsentRecord{}, false, ConsentExempt, true},
		{"unchanged bytes keep the approval", SourceManaged, approved, true, ConsentApproved, true},
		{"a user copy is approved the same way", SourceUser, approved, true, ConsentApproved, true},
		{"no record yet keeps the grant and asks once", SourceUser, ConsentRecord{}, false, ConsentUnrecorded, true},
		{"changed bytes withdraw every capability", SourceManaged, ConsentRecord{ContentSHA256: "00"}, true, ConsentChanged, false},
		{"a record without a digest is not an approval", SourceUser, ConsentRecord{}, true, ConsentChanged, false},
	} {
		v := VerifyConsent(m, tc.source, func(Source, string) (ConsentRecord, bool, error) { return tc.rec, tc.have, nil })
		if v.State != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.name, v.State, tc.want)
		}
		if v.Grants() != tc.grants {
			t.Errorf("%s: grants = %v, want %v", tc.name, v.Grants(), tc.grants)
		}
		if tc.source != SourceSystem && v.Digest != digest {
			t.Errorf("%s: digest = %q, want the plugin's own", tc.name, v.Digest)
		}
	}
}

func TestConsentLedgerRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ConsentName)
	l, err := LoadConsent(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Lookup(SourceUser, "org.example"); ok {
		t.Fatal("an absent ledger holds a record")
	}
	rec := ConsentRecord{ContentSHA256: "abc", Capabilities: []string{"panels"}, Requires: []string{"curl"}}
	l.Set(SourceUser, "org.example", rec)
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadConsent(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := back.Lookup(SourceUser, "org.example")
	if !ok || got.ContentSHA256 != "abc" || got.Capabilities[0] != "panels" || got.Requires[0] != "curl" {
		t.Fatalf("round trip = %+v, %v", got, ok)
	}
	if _, ok := back.Lookup(SourceManaged, "org.example"); ok {
		t.Fatal("a user approval answered for the managed copy")
	}
}

func TestConsentLedgerRefusesAnotherSchema(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ConsentName)
	if err := os.WriteFile(path, []byte(`{"schema":2,"plugins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConsent(path); err == nil {
		t.Fatal("a ledger from a newer shell was read as this one's")
	}
	if err := os.WriteFile(path, []byte(`{"schema":1,"plugins":{},"extra":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConsent(path); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

// An approval an earlier shell recorded over the manifest and entry point
// keeps its grant while those still match, and asks once; it never matches a
// tree that changed in the files it did cover.
func TestConsentVerdictHonoursALegacyApproval(t *testing.T) {
	t.Parallel()
	dir := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := legacyDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(Source, string) (ConsentRecord, bool, error) {
		return ConsentRecord{ContentSHA256: legacy}, true, nil
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.py"), []byte("pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := VerifyConsent(m, SourceUser, lookup); v.State != ConsentLegacy || !v.Grants() {
		t.Fatalf("legacy approval = %+v; want legacy and granted", v)
	}
	if err := os.WriteFile(m.ExecPath, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v := VerifyConsent(m, SourceUser, lookup); v.State != ConsentChanged {
		t.Fatalf("legacy approval of an older entry point = %+v; want changed", v)
	}
	// A tree record never falls back to the legacy comparison.
	tree := func(Source, string) (ConsentRecord, bool, error) {
		return ConsentRecord{ContentSHA256: TreeDigestPrefix + legacy}, true, nil
	}
	if v := VerifyConsent(m, SourceUser, tree); v.State != ConsentChanged {
		t.Fatalf("stale tree record = %+v; want changed", v)
	}
}

// An unreadable ledger cannot say whether a copy changed, so it must not
// fall back to the unrecorded grant (audit sysc-2358).
func TestConsentVerdictWithholdsWhenTheLedgerIsUnreadable(t *testing.T) {
	t.Parallel()
	dir := writePlugin(t, timerManifest, "bin/sysc-plugin-timer")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	unreadable := func(Source, string) (ConsentRecord, bool, error) {
		return ConsentRecord{}, false, errors.New("consent.json: invalid character")
	}
	for _, source := range []Source{SourceUser, SourceManaged} {
		v := VerifyConsent(m, source, unreadable)
		if v.State != ConsentUnreadable || v.Grants() {
			t.Errorf("%s: verdict = %+v, grants %v; want unreadable and withheld", source, v, v.Grants())
		}
		if v.Digest != digestOf(t, dir) {
			t.Errorf("%s: digest = %q, want the plugin's own", source, v.Digest)
		}
	}
	if v := VerifyConsent(m, SourceSystem, unreadable); v.State != ConsentExempt {
		t.Errorf("system copy = %+v; the package manager guards it, not the ledger", v)
	}
}

func TestRuntimeWithdrawsTheGrantFromAChangedCopy(t *testing.T) {
	t.Parallel()
	m := installHelper(t, "ok")
	digest, err := ContentDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		recorded string
		want     ConsentState
		granted  bool
	}{
		{"approved", digest, ConsentApproved, true},
		{"changed", "0000", ConsentChanged, false},
	} {
		opts := helperOptions()
		opts.Consent = func(Source, string) (ConsentRecord, bool, error) {
			return ConsentRecord{ContentSHA256: tc.recorded}, true, nil
		}
		r := NewRuntime(Candidate{Dir: m.Dir, Source: SourceUser, Manifest: m}, opts)
		if err := r.Start(context.Background()); err != nil {
			t.Fatalf("%s: Start: %v", tc.name, err)
		}
		st := r.Status()
		if st.Consent.State != tc.want || st.Consent.Digest != digest {
			t.Errorf("%s: consent = %+v, want %q at %s", tc.name, st.Consent, tc.want, digest)
		}
		if got := r.Allows(CapNotifications); got != tc.granted {
			t.Errorf("%s: notifications granted = %v, want %v", tc.name, got, tc.granted)
		}
		if st.State != StateRunning {
			t.Errorf("%s: state = %q; a changed copy still runs, only without capabilities", tc.name, st.State)
		}
		r.Stop()
	}
}
