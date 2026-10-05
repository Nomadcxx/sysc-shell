package lock

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Lease belongs to the shell. The locker never writes this resource snapshot.
type Lease struct {
	Session, Compositor      string
	Generation               uint64
	WallsKnown, WallsRunning bool
}

func (l Lease) CanRestore(s State) bool {
	return s.Known && s.Phase == "idle" && s.Session == l.Session && s.Compositor == l.Compositor && s.ConfirmedUnlock == l.Generation && s.Generation == s.ConfirmedUnlock && l.Generation != 0
}
func LoadLease(path string) (*Lease, error) {
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !privateLeaseFile(info, true) {
		return nil, fmt.Errorf("background lease directory must be private")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !privateLeaseFile(info, false) || info.Size() > 4096 {
		return nil, fmt.Errorf("invalid background lease")
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	var lease Lease
	if err = json.Unmarshal(data, &lease); err != nil {
		return nil, err
	}
	if lease.Session == "" || lease.Compositor == "" || lease.Generation == 0 {
		return nil, fmt.Errorf("invalid background lease identity")
	}
	return &lease, nil
}
func (l Lease) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !privateLeaseFile(info, true) {
		return fmt.Errorf("background lease directory must be private")
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".lease-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func privateLeaseFile(info os.FileInfo, dir bool) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return false
	}
	if dir {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && info.Mode().Perm() == 0600
}
