package plugin

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The Landlock filesystem rights, grouped the way a rule grants them.
const (
	llRead   = unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR
	llExec   = unix.LANDLOCK_ACCESS_FS_EXECUTE
	llChange = unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO | unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER | unix.LANDLOCK_ACCESS_FS_TRUNCATE
	llDevice = unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	// llResolveUnix is LANDLOCK_ACCESS_FS_RESOLVE_UNIX (ABI 9, uapi
	// linux/landlock.h): connecting to a pathname Unix socket whose server
	// started outside the plugin's domain. x/sys v0.47.0 does not name it.
	// Without it handled, a confined plugin reaches the session bus and the
	// compositor by path and starts programs outside its domain through them.
	llResolveUnix = 1 << 16
	// llFile are the rights a rule on a single file may carry.
	llFile = unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_TRUNCATE |
		unix.LANDLOCK_ACCESS_FS_IOCTL_DEV | llResolveUnix
)

// The Landlock ABIs that introduced what the boundary relies on beyond files.
const (
	scopeABI       = 6
	resolveUnixABI = 9
)

// handledFor is every filesystem right a kernel at this ABI can deny. A
// right left out of the handled set is not restricted at all.
func handledFor(abi int) uint64 {
	handled := uint64(llRead | llExec | llChange | llDevice | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK)
	if abi < 2 {
		handled &^= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi < 3 {
		handled &^= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi < 5 {
		handled &^= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	if abi >= resolveUnixABI {
		handled |= llResolveUnix
	}
	return handled
}

func (a Access) rights() uint64 {
	switch a {
	case AccessRead:
		return llRead
	case AccessReadExec:
		return llRead | llExec
	case AccessReadWrite:
		return llRead | llChange
	case AccessReadWriteExec:
		return llRead | llChange | llExec
	case AccessDevice:
		return llRead | llChange | llDevice
	case AccessSocket:
		return llResolveUnix
	}
	return 0
}

var landlockABI = sync.OnceValue(func() int {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0
	}
	return int(abi)
})

// LandlockABI is the Landlock version this kernel offers, 0 when it offers
// none (an old kernel, or Landlock not among the enabled security modules).
func LandlockABI() int { return landlockABI() }

// scopesFor is the IPC a kernel at this ABI can isolate the plugin from:
// abstract Unix sockets always, and signals to processes it did not start
// unless the plugin declared it may send them.
func scopesFor(abi int, signalAny bool) uint64 {
	if abi < scopeABI {
		return 0
	}
	scoped := uint64(unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET)
	if !signalAny {
		scoped |= unix.LANDLOCK_SCOPE_SIGNAL
	}
	return scoped
}

// restrictPaths confines this thread, and whatever it executes, to rules.
// Missing paths are skipped, except those a rule may create.
func restrictPaths(rules []PathRule, signalAny bool) error {
	abi := LandlockABI()
	if abi < 1 {
		return fmt.Errorf("landlock is not available")
	}
	handled := handledFor(abi)
	attr := unix.LandlockRulesetAttr{Access_fs: handled, Scoped: scopesFor(abi, signalAny)}
	// A kernel before the scopes rejects the longer attribute, so the size
	// stops before the network field there; network rights are not handled.
	size := unsafe.Sizeof(attr)
	if attr.Scoped == 0 {
		size = unsafe.Offsetof(attr.Access_net)
	}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), size, 0)
	if errno != 0 {
		return fmt.Errorf("create ruleset: %w", errno)
	}
	ruleset := int(fd)
	defer unix.Close(ruleset)
	for _, rule := range rules {
		if rule.Create {
			if err := os.MkdirAll(rule.Path, 0o700); err != nil {
				return fmt.Errorf("create %s: %w", rule.Path, err)
			}
		}
		if err := addRule(ruleset, rule, handled); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no new privileges: %w", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(ruleset), 0, 0); errno != 0 {
		return fmt.Errorf("restrict: %w", errno)
	}
	return nil
}

func addRule(ruleset int, rule PathRule, handled uint64) error {
	fd, err := unix.Open(rule.Path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err == unix.ENOENT {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", rule.Path, err)
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", rule.Path, err)
	}
	access := rule.Access.rights() & handled
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		access &= llFile
	}
	if access == 0 {
		return nil
	}
	beneath := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset),
		unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&beneath)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("allow %s: %w", rule.Path, errno)
	}
	return nil
}
