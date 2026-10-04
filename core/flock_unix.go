//go:build !windows

package core

import (
	"errors"
	"os"
	"syscall"
)

// errLocked 统一“锁被占用”的内部哨兵错误。
var errLocked = errors.New("locked")

// lockExclusiveNB 用 flock(2) 拿非阻塞独占锁。
func lockExclusiveNB(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errLocked
		}
		return err
	}
	return nil
}

func unlockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
