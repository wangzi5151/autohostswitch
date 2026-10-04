package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrWriteLocked 表示另一个 AutoHostSwitch 进程正在写入 hosts。
// 锁是“君子协定”（advisory）：只约束同样走本锁的 AutoHostSwitch 进程
// （CLI/Web 共用同一数据目录），外部文本编辑器不遵守它——
// 那种情况由 Hash guard（ConcurrentChangeError）兜底。
var ErrWriteLocked = errors.New("另一个 AutoHostSwitch 正在写入 hosts，请稍后再试")

// WriteLock 是进程间写入互斥锁。拿锁失败（被占用）时 LockWrite 直接返回
// ErrWriteLocked，不阻塞等待——hosts 写入是秒级操作，等待不如让用户重试。
type WriteLock struct {
	f *os.File
}

// LockWrite 在数据目录下拿独占写入锁。调用方必须在写入完成后 Unlock。
func LockWrite(dataDir string) (*WriteLock, error) {
	path := filepath.Join(dataDir, "write.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败：%w", err)
	}
	if err := lockExclusiveNB(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errLocked) {
			return nil, ErrWriteLocked
		}
		return nil, fmt.Errorf("加写入锁失败：%w", err)
	}
	return &WriteLock{f: f}, nil
}

// Unlock 释放锁。
func (l *WriteLock) Unlock() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockExclusive(l.f)
	_ = l.f.Close()
	return err
}
