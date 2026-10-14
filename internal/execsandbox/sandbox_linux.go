//go:build linux

package execsandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func ABI() (int, error) {
	n, _, e := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if e != 0 {
		return 0, e
	}
	return int(n), nil
}
func Available() error {
	abi, err := ABI()
	if err != nil || abi < 6 {
		return fmt.Errorf("需要Linux Landlock ABI>=6，受限命令未执行（ABI=%d，%v）", abi, err)
	}
	return nil
}
func runChild(p policy, command string) error {
	runtime.LockOSThread()
	if err := Available(); err != nil {
		return err
	}
	if p.Mode != "read-only" && p.Mode != "workspace-write" {
		return fmt.Errorf("invalid mode")
	}
	for _, root := range []string{p.Workspace, p.Temporary} {
		if !filepath.IsAbs(root) || filepath.Clean(root) == "/" {
			return fmt.Errorf("invalid sandbox root")
		}
	}
	read := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR)
	write := uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SYM | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO | unix.LANDLOCK_ACCESS_FS_REFER | unix.LANDLOCK_ACCESS_FS_TRUNCATE)
	attr := unix.LandlockRulesetAttr{Access_fs: read | write | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV, Scoped: unix.LANDLOCK_SCOPE_SIGNAL | unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return errno
	}
	defer unix.Close(int(fd))
	allow := func(path string, access uint64, required bool) error {
		parent, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			if !required && os.IsNotExist(err) {
				return nil
			}
			return err
		}
		defer unix.Close(parent)
		var st unix.Stat_t
		if err := unix.Fstat(parent, &st); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			access &= unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(parent)}
		_, _, e := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		if e != 0 {
			return e
		}
		return nil
	}
	for _, path := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc", "/dev", "/proc/self", "/context"} {
		if err := allow(path, read, false); err != nil {
			return fmt.Errorf("runtime rule: %w", err)
		}
	}
	workspaceAccess := read
	if p.Mode == "workspace-write" {
		workspaceAccess |= write
	}
	if err := allow(p.Workspace, workspaceAccess, true); err != nil {
		return err
	}
	if err := allow(p.Temporary, read|write, true); err != nil {
		return err
	}
	if err := allow("/dev/null", unix.LANDLOCK_ACCESS_FS_READ_FILE|unix.LANDLOCK_ACCESS_FS_WRITE_FILE, false); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return errno
	}
	if err := unix.Chdir(p.Workspace); err != nil {
		return err
	}
	env := []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "TERM=dumb", "HOME=" + filepath.Join(p.Temporary, "home"), "TMPDIR=" + p.Temporary, "GOCACHE=" + filepath.Join(p.Temporary, "cache"), "GOPATH=" + filepath.Join(p.Temporary, "go"), "AIDE_CACHE=" + filepath.Join(p.Temporary, "cache"), "AIDE_SANDBOX=1"}
	return unix.Exec("/bin/bash", []string{"bash", "--noprofile", "--norc", "-c", command}, env)
}
