package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 操作日志只记录“时间 + 做了什么”，不收集任何设备信息、
// 不记录 hosts 具体内容、不联网。用户可一键清空。

func (s *Store) logPath() string { return filepath.Join(s.DataDir, "operations.log") }

// appendLog 追加一条操作日志。写日志失败不影响主流程（静默忽略）。
func (s *Store) appendLog(action string) {
	line := fmt.Sprintf("%s  %s\n", nowStr(), action)
	f, err := os.OpenFile(s.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// readLog 读取全部日志行（旧的在前）。
func (s *Store) readLog() ([]string, error) {
	data, err := os.ReadFile(s.logPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取日志失败：%w", err)
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// ReadLog 对外读取日志。
func (s *Store) ReadLog() ([]string, error) { return s.readLog() }

// ClearLog 一键清空日志。
func (s *Store) ClearLog() error {
	if err := os.WriteFile(s.logPath(), nil, 0o644); err != nil {
		return fmt.Errorf("清空日志失败：%w", err)
	}
	return nil
}
