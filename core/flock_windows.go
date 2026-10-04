//go:build windows

package core

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// errLocked 统一“锁被占用”的内部哨兵错误。
var errLocked = errors.New("locked")

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	procLockEx   = kernel32.NewProc("LockFileEx")
	procUnlockEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
	// ERROR_LOCK_VIOLATION (33)：LockFileEx 在 LOCKFILE_FAIL_IMMEDIATELY 下
	// 锁被占用时的返回值。syscall 包没导出它，这里按 MSDN 定义。
	errnoLockViolation syscall.Errno = 33
)

// lockExclusiveNB 用 LockFileEx 拿非阻塞独占锁（锁定 1 个字节即可）。
func lockExclusiveNB(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, e := procLockEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		uintptr(0), uintptr(1), uintptr(0),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		if e == errnoLockViolation {
			return errLocked
		}
		return e
	}
	return nil
}

func unlockExclusive(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, e := procUnlockEx.Call(
		f.Fd(),
		uintptr(0), uintptr(1), uintptr(0),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return e
	}
	return nil
}
