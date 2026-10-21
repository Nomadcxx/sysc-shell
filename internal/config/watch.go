package config

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const configWatchDebounce = 150 * time.Millisecond

type configWatch struct {
	fd       int
	cancelFD int
	wd       int32
	basename string
}

type configWatchEvents struct {
	notify bool
	resync bool
	lost   bool
}

// Watch waits for saves to path's lexical parent directory and calls notify
// once after a matching event burst settles. It does not read or parse config.
func Watch(ctx context.Context, path string, notify func()) error {
	if ctx == nil {
		return errors.New("config watch: nil context")
	}
	if path == "" {
		return errors.New("config watch: empty path")
	}
	if notify == nil {
		return errors.New("config watch: nil callback")
	}
	watch, err := newConfigWatch(path)
	if err != nil {
		return err
	}
	return watch.run(ctx, notify)
}

func newConfigWatch(path string) (*configWatch, error) {
	if path == "" {
		return nil, errors.New("config watch: empty path")
	}
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("config watch: initialize inotify: %w", err)
	}
	directory := filepath.Dir(path)
	wd, err := unix.InotifyAddWatch(fd, directory, unix.IN_ONLYDIR|
		unix.IN_CLOSE_WRITE|unix.IN_MOVED_TO|unix.IN_DELETE|unix.IN_MOVED_FROM|
		unix.IN_DELETE_SELF|unix.IN_MOVE_SELF)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("config watch: watch parent directory: %w", err)
	}
	cancelFD, err := unix.Eventfd(0, unix.EFD_NONBLOCK|unix.EFD_CLOEXEC)
	if err != nil {
		_, _ = unix.InotifyRmWatch(fd, uint32(wd))
		_ = unix.Close(fd)
		return nil, fmt.Errorf("config watch: create cancellation event: %w", err)
	}
	return &configWatch{fd: fd, cancelFD: cancelFD, wd: int32(wd), basename: filepath.Base(path)}, nil
}

func (w *configWatch) run(ctx context.Context, notify func()) error {
	if w == nil || w.fd < 0 || w.cancelFD < 0 {
		return errors.New("config watch: unavailable watcher")
	}
	defer w.close()
	if ctx == nil {
		return errors.New("config watch: nil context")
	}
	if notify == nil {
		return errors.New("config watch: nil callback")
	}

	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		var wake [8]byte
		binary.NativeEndian.PutUint64(wake[:], 1)
		for {
			_, err := unix.Write(w.cancelFD, wake[:])
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
	})
	defer func() {
		if !stop() {
			<-callbackDone
		}
	}()

	buf := make([]byte, 16*1024)
	fds := []unix.PollFd{
		{Fd: int32(w.fd), Events: unix.POLLIN},
		{Fd: int32(w.cancelFD), Events: unix.POLLIN},
	}
	var deadline time.Time
	for {
		if ctx.Err() != nil {
			return nil
		}
		timeout := configPollTimeout(deadline)
		ready, err := unix.Poll(fds, timeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("config watch: poll: %w", err)
		}
		if ready == 0 {
			if !deadline.IsZero() && !time.Now().Before(deadline) {
				if ctx.Err() != nil {
					return nil
				}
				notify()
				deadline = time.Time{}
			}
			continue
		}
		if ctx.Err() != nil || fds[1].Revents&(unix.POLLIN|unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return nil
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errors.New("config watch: inotify descriptor failed")
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		for {
			n, err := unix.Read(w.fd, buf)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if errors.Is(err, unix.EAGAIN) {
				break
			}
			if err != nil {
				return fmt.Errorf("config watch: read events: %w", err)
			}
			if n == 0 {
				return errors.New("config watch: inotify closed")
			}
			events, err := parseConfigEvents(buf[:n], w.wd, w.basename)
			if err != nil {
				return err
			}
			if events.lost {
				return errors.New("config watch: parent directory watch lost")
			}
			if events.notify {
				deadline = time.Now().Add(configWatchDebounce)
			}
		}
	}
}

func (w *configWatch) close() {
	if w == nil {
		return
	}
	if w.cancelFD >= 0 {
		_ = unix.Close(w.cancelFD)
		w.cancelFD = -1
	}
	if w.fd >= 0 {
		if w.wd >= 0 {
			_, _ = unix.InotifyRmWatch(w.fd, uint32(w.wd))
		}
		_ = unix.Close(w.fd)
		w.fd = -1
	}
}

func parseConfigEvents(data []byte, wd int32, basename string) (configWatchEvents, error) {
	var out configWatchEvents
	const lostMask = unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_IGNORED
	const matchMask = unix.IN_CLOSE_WRITE | unix.IN_MOVED_TO | unix.IN_DELETE | unix.IN_MOVED_FROM
	for offset := 0; offset < len(data); {
		remaining := len(data) - offset
		if remaining < unix.SizeofInotifyEvent {
			return configWatchEvents{}, errors.New("config watch: malformed event header")
		}
		header := data[offset : offset+unix.SizeofInotifyEvent]
		eventWD := int32(binary.NativeEndian.Uint32(header[0:4]))
		mask := binary.NativeEndian.Uint32(header[4:8])
		nameLen := binary.NativeEndian.Uint32(header[12:16])
		recordLen := uint64(unix.SizeofInotifyEvent) + uint64(nameLen)
		if recordLen > uint64(remaining) {
			return configWatchEvents{}, errors.New("config watch: malformed event length")
		}
		end := offset + int(recordLen)
		if mask&unix.IN_Q_OVERFLOW != 0 {
			out.notify, out.resync = true, true
			offset = end
			continue
		}
		if eventWD == wd && mask&lostMask != 0 {
			out.lost = true
		}
		if nameLen == 0 {
			if eventWD == wd && mask&matchMask != 0 {
				return configWatchEvents{}, errors.New("config watch: malformed empty event name")
			}
			offset = end
			continue
		}
		nameBytes := data[offset+unix.SizeofInotifyEvent : end]
		zero := bytes.IndexByte(nameBytes, 0)
		if zero < 0 {
			return configWatchEvents{}, errors.New("config watch: malformed event name")
		}
		if zero == 0 && eventWD == wd && mask&matchMask != 0 {
			return configWatchEvents{}, errors.New("config watch: malformed empty event name")
		}
		if eventWD == wd && mask&matchMask != 0 && string(nameBytes[:zero]) == basename {
			out.notify = true
		}
		offset = end
	}
	return out, nil
}

func configPollTimeout(deadline time.Time) int {
	if deadline.IsZero() {
		return -1
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	ms := (remaining + time.Millisecond - 1) / time.Millisecond
	if ms > time.Duration(int(^uint(0)>>1)) {
		return int(^uint(0) >> 1)
	}
	return int(ms)
}
