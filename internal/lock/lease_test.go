package lock

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBackgroundLeaseFollowsConfirmedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "background.json")
	sealed := State{Known: true, Snapshot: Snapshot{Session: "s", Compositor: "n:1", Generation: 5, Phase: "sealed"}}
	lease := Lease{Session: "s", Compositor: "n:1", Generation: 5, WallsKnown: true, WallsRunning: true}
	if err := lease.Save(path); err != nil {
		t.Fatal(err)
	}
	r, err := LoadLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.CanRestore(sealed) {
		t.Fatal("restored while sealed")
	}
	for _, state := range []State{{Snapshot: Snapshot{Phase: "idle"}}, {Known: true, Snapshot: Snapshot{Session: "s", Compositor: "n:1", Phase: "idle", ConfirmedUnlock: 4}}, {Known: true, Snapshot: Snapshot{Session: "other", Compositor: "n:1", Phase: "idle", ConfirmedUnlock: 5}}} {
		if r.CanRestore(state) {
			t.Fatal("unknown/mismatched restored")
		}
	}
	sealed.Phase = "idle"
	sealed.ConfirmedUnlock = 5
	sealed.Generation = 5
	if !r.CanRestore(sealed) {
		t.Fatal("matching receipt did not restore")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestBackgroundLeaseRejectsNewerAcquisition(t *testing.T) {
	l := Lease{Session: "s", Compositor: "n:1", Generation: 5}
	s := State{Known: true, Snapshot: Snapshot{Session: "s", Compositor: "n:1", Generation: 6, ConfirmedUnlock: 5, Phase: "idle"}}
	if l.CanRestore(s) {
		t.Fatal("restored into newer acquisition")
	}
}
func TestBackgroundLeaseRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := (Lease{Session: "s", Compositor: "n:1", Generation: 5}).Save(target); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLease(link); err == nil {
		t.Fatal("followed symlink")
	}
}

func TestBackgroundLeaseFIFOReadDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "lease")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLease(path); err == nil {
		t.Fatal("accepted FIFO")
	}
}
