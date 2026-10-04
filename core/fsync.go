package core

import "os"

// syncDir 尽力对目录做 fsync，提高 rename 的持久性。
// Windows 下对目录 Sync 可能失败，这里忽略错误——它只是加固，不是必需。
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
