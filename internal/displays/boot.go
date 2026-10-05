package displays

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// bootNow reads CLOCK_BOOTTIME, which keeps counting while the machine is
// suspended. A preview deadline is expressed in this clock so a suspended
// session expires on resume instead of holding a stale preview.
func bootNow() (time.Duration, error) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return 0, fmt.Errorf("displays: read boottime clock: %w", err)
	}
	return time.Duration(ts.Sec)*time.Second + time.Duration(ts.Nsec), nil
}

// sleepUntilBoot blocks until the boot clock passes deadlineNS. A timerfd on
// CLOCK_BOOTTIME is the only wait that keeps its meaning across suspend, which
// is the point: a preview must expire on resume rather than linger.
func sleepUntilBoot(deadlineNS int64) error {
	now, err := bootNow()
	if err != nil {
		return err
	}
	remaining := time.Duration(deadlineNS) - now
	if remaining <= 0 {
		return nil
	}
	fd, err := unix.TimerfdCreate(unix.CLOCK_BOOTTIME, unix.TFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("displays: create boot-clock timer: %w", err)
	}
	defer unix.Close(fd)
	secs := deadlineNS / int64(time.Second)
	nanos := deadlineNS % int64(time.Second)
	// An absolute deadline on the boot clock, so the wait cannot drift away
	// from the deadline the ticket records.
	if err := unix.TimerfdSettime(fd, unix.TFD_TIMER_ABSTIME, &unix.ItimerSpec{
		Interval: unix.Timespec{},
		Value:    unix.Timespec{Sec: secs, Nsec: nanos},
	}, nil); err != nil {
		return fmt.Errorf("displays: arm boot-clock timer: %w", err)
	}
	var buf [8]byte
	if _, err := unix.Read(fd, buf[:]); err != nil {
		return fmt.Errorf("displays: wait for boot-clock deadline: %w", err)
	}
	return nil
}

// Remaining reports how much of an armed deadline is left on the boot clock.
func Remaining(deadlineNS int64) (time.Duration, error) {
	now, err := bootNow()
	if err != nil {
		return 0, err
	}
	left := time.Duration(deadlineNS) - now
	if left < 0 {
		left = 0
	}
	return left, nil
}

// bootID names this boot. A ticket written before a reboot is stale even
// though its file and clock both still look valid.
func bootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("displays: read boot id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Identity is the exact compositor a transaction is bound to. PID alone is not
// enough: a recycled PID or a restarted compositor under the same socket path
// must never be handed a transaction prepared for its predecessor.
type Identity struct {
	UID        int    `json:"uid"`
	PID        int    `json:"pid"`
	StartTicks int64  `json:"start_ticks"`
	BootID     string `json:"boot_id"`
}

func (i Identity) Equal(other Identity) bool { return i == other }

func (i Identity) String() string {
	return fmt.Sprintf("uid %d pid %d start %d boot %s", i.UID, i.PID, i.StartTicks, i.BootID)
}

// compositorIdentity asks the socket who is on the other end and fingerprints
// that process. SO_PEERCRED works on the client side of a connected AF_UNIX
// socket on Linux, so one short connection is enough; nothing is sent to it.
func compositorIdentity(socket string) (Identity, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return Identity{}, fmt.Errorf("displays: connect to compositor socket: %w", err)
	}
	defer conn.Close()

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return Identity{}, fmt.Errorf("displays: %s is not a unix socket", socket)
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return Identity{}, fmt.Errorf("displays: compositor socket handle: %w", err)
	}
	var ucred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return Identity{}, fmt.Errorf("displays: compositor socket handle: %w", err)
	}
	if credErr != nil {
		return Identity{}, fmt.Errorf("displays: compositor peer credentials: %w", credErr)
	}
	if ucred.Uid != uint32(os.Getuid()) {
		return Identity{}, fmt.Errorf("displays: compositor socket %s is owned by uid %d, not %d", socket, ucred.Uid, os.Getuid())
	}
	ticks, err := processStartTicks(int(ucred.Pid))
	if err != nil {
		return Identity{}, err
	}
	id, err := bootID()
	if err != nil {
		return Identity{}, err
	}
	return Identity{UID: int(ucred.Uid), PID: int(ucred.Pid), StartTicks: ticks, BootID: id}, nil
}

// procPath is the only /proc access in the package, so every read of the
// compositor's own state is recognizable.
func procPath(pid int, file string) string { return fmt.Sprintf("/proc/%d/%s", pid, file) }

// processStartTicks returns field 22 of /proc/<pid>/stat: the process start time
// in clock ticks since boot. The comm field may itself contain spaces and
// parentheses, so the fields are counted from the last closing parenthesis.
func processStartTicks(pid int) (int64, error) {
	data, err := os.ReadFile(procPath(pid, "stat"))
	if err != nil {
		return 0, fmt.Errorf("displays: read compositor start time: %w", err)
	}
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return 0, fmt.Errorf("displays: compositor stat line is malformed")
	}
	fields := strings.Fields(text[end+1:])
	// fields[0] is the state, which is field 3 of the full line, so field N of
	// the full line is fields[N-3]; starttime is field 22.
	const startTimeField = 22 - 3
	if len(fields) <= startTimeField {
		return 0, fmt.Errorf("displays: compositor stat line has %d fields after comm", len(fields))
	}
	ticks, err := strconv.ParseInt(fields[startTimeField], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("displays: compositor start time is not a number: %w", err)
	}
	return ticks, nil
}
