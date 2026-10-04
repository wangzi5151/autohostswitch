package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 操作日志只记录“时间 + 做了什么”，不收集任何设备信息、
// 不记录 hosts 具体内容、不联网。用户可一键清空。
//
// 日志行格式（v2，结构化，2026-10-04 起）：
// 2026-10-04 15:00:00 [Web] 应用配置「dev」 | 成功 | 快照：自动安全快照 2026-10-04 15:00:00
// 兼容 v1 旧格式：2026-10-04 15:00:00  动作描述

func (s *Store) logPath() string { return filepath.Join(s.DataDir, "operations.log") }

// LogEntry 是一条结构化操作记录。
type LogEntry struct {
	At       string // "2026-10-04 15:00:00"
	Source   string // CLI / Web / —（旧日志）
	Action   string // 做了什么
	Result   string // 成功 / 失败：原因
	Snapshot string // 关联快照名（可空）
}

// appendLog 追加一条“成功”日志（兼容旧调用点）。
func (s *Store) appendLog(action string) {
	s.appendLogEntry(s.Source, action, "成功", "")
}

// appendLogEntry 追加一条结构化日志。写日志失败不影响主流程（静默忽略）。
func (s *Store) appendLogEntry(source, action, result, snapshot string) {
	if source == "" {
		source = "CLI"
	}
	line := fmt.Sprintf("%s [%s] %s | %s", nowStr(), source, action, result)
	if snapshot != "" {
		line += " | 快照：" + snapshot
	}
	line += "\n"
	f, err := os.OpenFile(s.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// parseLogLine 解析一行日志，兼容 v1/v2 两种格式。
func parseLogLine(l string) LogEntry {
	l = strings.TrimSpace(l)
	e := LogEntry{Source: "—", Result: "—"}
	if len(l) >= 19 {
		e.At = l[:19]
		rest := strings.TrimSpace(l[19:])
		// v2: [来源] 动作 | 结果 | 快照：xxx
		if strings.HasPrefix(rest, "[") {
			if i := strings.Index(rest, "]"); i > 0 {
				e.Source = rest[1:i]
				rest = strings.TrimSpace(rest[i+1:])
			}
		}
		parts := strings.Split(rest, " | ")
		e.Action = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			e.Result = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			e.Snapshot = strings.TrimPrefix(strings.TrimSpace(parts[2]), "快照：")
		}
		return e
	}
	e.Action = l
	return e
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

// ReadLog 对外读取日志（原始行）。
func (s *Store) ReadLog() ([]string, error) { return s.readLog() }

// ReadLogEntries 对外读取结构化日志（旧的在前）。
func (s *Store) ReadLogEntries() ([]LogEntry, error) {
	lines, err := s.readLog()
	if err != nil {
		return nil, err
	}
	out := make([]LogEntry, 0, len(lines))
	for _, l := range lines {
		out = append(out, parseLogLine(l))
	}
	return out, nil
}

// ClearLog 一键清空日志。
func (s *Store) ClearLog() error {
	if err := os.WriteFile(s.logPath(), nil, 0o644); err != nil {
		return fmt.Errorf("清空日志失败：%w", err)
	}
	return nil
}
