package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openStore(t *testing.T, root, id string) *Store {
	t.Helper()
	s, err := OpenStore(root, id)
	if err != nil {
		t.Fatalf("OpenStore(%q): %v", id, err)
	}
	return s
}

func set(t *testing.T, s *Store, key, value string) error {
	t.Helper()
	return s.Set(context.Background(), key, json.RawMessage(value))
}

func TestStoreRoundTripsAValue(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := openStore(t, root, "org.sysc.timer")
	if err := set(t, s, "remaining", `{"seconds":42}`); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := s.Get("remaining")
	if !ok {
		t.Fatal("Get missed a value just written")
	}
	if string(got) != `{"seconds":42}` {
		t.Errorf("value = %s", got)
	}

	// A fresh store over the same directory sees the committed value, which is
	// what a plugin restart depends on.
	again := openStore(t, root, "org.sysc.timer")
	if got, ok := again.Get("remaining"); !ok || string(got) != `{"seconds":42}` {
		t.Errorf("reopened value = %s, ok = %v", got, ok)
	}
}

func TestStoreKeepsPluginsInSeparateNamespaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	timer := openStore(t, root, "org.sysc.timer")
	clock := openStore(t, root, "org.sysc.world-clock")
	if err := set(t, timer, "shared", `1`); err != nil {
		t.Fatal(err)
	}
	if err := set(t, clock, "shared", `2`); err != nil {
		t.Fatal(err)
	}
	if v, _ := timer.Get("shared"); string(v) != "1" {
		t.Errorf("timer value = %s, want its own", v)
	}
	if v, _ := clock.Get("shared"); string(v) != "2" {
		t.Errorf("clock value = %s, want its own", v)
	}
	if timer.Path() == clock.Path() {
		t.Fatal("two plugins share one state file")
	}
}

func TestOpenStoreRejectsAnIDThatCouldEscapeTheRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, id := range []string{"", "..", "../escape", "org.sysc/../../etc", "/etc/passwd", "Org.Sysc"} {
		if _, err := OpenStore(root, id); err == nil {
			t.Errorf("OpenStore accepted id %q", id)
		}
	}
}

func TestStoreDeletesOnANullValue(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	if err := set(t, s, "k", `1`); err != nil {
		t.Fatal(err)
	}
	if err := set(t, s, "k", `null`); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.Get("k"); ok {
		t.Fatal("a null value did not delete the key")
	}
	if got := s.Keys(); len(got) != 0 {
		t.Fatalf("keys = %v, want none", got)
	}
}

func TestStoreListsKeysInOrder(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	for _, k := range []string{"zebra", "alpha", "middle"} {
		if err := set(t, s, k, `1`); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"alpha", "middle", "zebra"}
	got := s.Keys()
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
}

func TestStoreRejectsInvalidKeys(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	for _, k := range []string{"", "Has Space", "../escape", "a.b", strings.Repeat("a", 200)} {
		if err := set(t, s, k, `1`); err == nil {
			t.Errorf("Set accepted key %q", k)
		}
	}
}

func TestStoreRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	if err := set(t, s, "k", `{"unclosed":`); err == nil {
		t.Fatal("Set accepted malformed JSON")
	}
}

func TestStoreBoundsOneValue(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	// A JSON string of the limit plus its quotes is over by two bytes.
	over := `"` + strings.Repeat("a", MaxStateValueBytes) + `"`
	if err := set(t, s, "big", over); err == nil {
		t.Fatalf("Set accepted a value over the %d byte limit", MaxStateValueBytes)
	}
	under := `"` + strings.Repeat("a", MaxStateValueBytes-2) + `"`
	if err := set(t, s, "big", under); err != nil {
		t.Fatalf("Set rejected a value at the limit: %v", err)
	}
}

func TestStoreBoundsTheWholeFile(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	chunk := `"` + strings.Repeat("a", MaxStateValueBytes-2) + `"`
	var err error
	writes := 0
	for i := 0; i < MaxStateKeys; i++ {
		if err = set(t, s, "k"+itoa(i), chunk); err != nil {
			break
		}
		writes++
	}
	if err == nil {
		t.Fatalf("wrote %d values with no total limit reached", writes)
	}
	if !strings.Contains(err.Error(), "total") {
		t.Fatalf("err = %v, want it to name the total limit", err)
	}
}

func TestStoreBoundsTheKeyCount(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	for i := 0; i < MaxStateKeys; i++ {
		if err := set(t, s, "k"+itoa(i), `1`); err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
	}
	if err := set(t, s, "one-too-many", `1`); err == nil {
		t.Fatalf("Set accepted more than %d keys", MaxStateKeys)
	}
}

func TestARejectedWriteChangesNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := openStore(t, root, "org.sysc.timer")
	if err := set(t, s, "keep", `"safe"`); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}

	over := `"` + strings.Repeat("a", MaxStateValueBytes) + `"`
	if err := set(t, s, "keep", over); err == nil {
		t.Fatal("the oversized write was accepted")
	}

	if v, ok := s.Get("keep"); !ok || string(v) != `"safe"` {
		t.Errorf("in-memory value = %s, ok = %v, want the previous one", v, ok)
	}
	after, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("file changed after a rejected write:\n%s\n%s", before, after)
	}
}

func TestStoreLeavesNoTemporaryFileBehind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := openStore(t, root, "org.sysc.timer")
	if err := set(t, s, "k", `1`); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(s.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(s.Path()) {
		t.Fatalf("directory holds %v, want only the state file", entries)
	}
}

func TestSetHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	s := openStore(t, t.TempDir(), "org.sysc.timer")
	if err := set(t, s, "before", `1`); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Set(ctx, "after", json.RawMessage(`1`)); err == nil {
		t.Fatal("Set ignored a cancelled context")
	}
	if _, ok := s.Get("after"); ok {
		t.Error("a cancelled write left a value behind")
	}
	if _, ok := s.Get("before"); !ok {
		t.Error("a cancelled write disturbed an existing value")
	}
}

func TestOpenStoreRejectsAMalformedStateFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "org.sysc.timer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, StateFileName), []byte(`{"k":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(root, "org.sysc.timer"); err == nil {
		t.Fatal("OpenStore accepted a malformed state file")
	}
}

func TestOpenStoreRejectsAnOversizedStateFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "org.sysc.timer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blob := append([]byte(`{"k":"`), make([]byte, MaxStateTotalBytes)...)
	blob = append(blob, '"', '}')
	for i := 6; i < len(blob)-2; i++ {
		blob[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(dir, StateFileName), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(root, "org.sysc.timer"); err == nil {
		t.Fatal("OpenStore accepted an oversized state file")
	}
}

// Not parallel: it replaces XDG_STATE_HOME and HOME for the process.
func TestStateRootFollowsTheEnvironment(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/state")
	if got, want := StateRoot(), "/tmp/state/sysc-shell/plugins"; got != want {
		t.Errorf("StateRoot = %q, want %q", got, want)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/example")
	if got, want := StateRoot(), "/home/example/.local/state/sysc-shell/plugins"; got != want {
		t.Errorf("StateRoot = %q, want %q", got, want)
	}

	// A relative XDG_STATE_HOME is not usable and must fall back rather than
	// resolve against whatever directory the shell happens to be started in.
	t.Setenv("XDG_STATE_HOME", "relative/path")
	if got, want := StateRoot(), "/home/example/.local/state/sysc-shell/plugins"; got != want {
		t.Errorf("StateRoot = %q, want %q", got, want)
	}
}
