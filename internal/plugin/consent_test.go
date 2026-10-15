package plugin

import (
	"context"
	"os"
	"path/filepath"
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
	if len(d) != 64 {
		t.Fatalf("digest %q is not a hex sha256", d)
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

	// State, caches and records written beside the plugin are not its code.
	base := digestOf(t, a)
	for _, extra := range []string{"state.json", "installed.json", "cache/thumb.png"} {
		path := filepath.Join(a, extra)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := digestOf(t, a); got != base {
		t.Fatal("digest covers files the plugin did not declare")
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
		v := VerifyConsent(m, tc.source, func(Source, string) (ConsentRecord, bool) { return tc.rec, tc.have })
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
		opts.Consent = func(Source, string) (ConsentRecord, bool) {
			return ConsentRecord{ContentSHA256: tc.recorded}, true
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
