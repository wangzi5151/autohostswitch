package core

import (
	"fmt"
	"os"
	"strings"

	"github.com/wangzi5151/autohostswitch/platform"
)

// ValidationError 是 hosts 语法校验失败时的错误，携带全部问题明细。
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("hosts 内容校验没通过，拒绝写入（你的原文件没有被改动）：\n")
	for _, it := range e.Issues {
		if it.Warn {
			continue // 警告不阻止写入，这里只列错误
		}
		if it.Line > 0 {
			fmt.Fprintf(&b, "  第 %d 行：%s\n", it.Line, it.Msg)
		} else {
			fmt.Fprintf(&b, "  %s\n", it.Msg)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// WarningsText 把警告单独格式化出来，给用户看但不阻止。
func WarningsText(issues []Issue) string {
	var b strings.Builder
	for _, it := range issues {
		if !it.Warn {
			continue
		}
		if it.Line > 0 {
			fmt.Fprintf(&b, "  ⚠ 第 %d 行：%s\n", it.Line, it.Msg)
		} else {
			fmt.Fprintf(&b, "  ⚠ %s\n", it.Msg)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ApplyHosts 是全工具唯一的 hosts 写入入口。
// 固定流程：语法校验 → 自动安全快照 → 写入文件 → 记日志。
// 任何一步失败都不动原文件（校验和快照在前，写入是最后一步）。
func (s *Store) ApplyHosts(content, actor string) error {
	issues := ValidateHosts(content)
	if HasError(issues) {
		return &ValidationError{Issues: issues}
	}
	content = NormalizeHosts(content)

	current, err := os.ReadFile(s.HostsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取当前 hosts 失败：%w", err)
	}
	if string(current) == content {
		return fmt.Errorf("内容和当前 hosts 完全一样，不用写")
	}

	// 先写权限预检：给中文提示而不是让 WriteFile 抛系统英文
	if ok, werr := platform.Writable(s.HostsPath); !ok {
		if werr != nil && !os.IsPermission(werr) && !os.IsNotExist(werr) {
			// 路径不存在等非权限问题，直接说明
			return fmt.Errorf("写前检查失败：%w", werr)
		}
		if os.IsPermission(werr) || werr == nil {
			return fmt.Errorf("%s", platform.FriendlyWriteError(
				fmt.Errorf("permission denied"), "autohostswitch"))
		}
	}

	// 覆盖前自动生成安全快照（内容无变化时内部会跳过）
	if err := s.createSafetySnapshot(current); err != nil {
		return fmt.Errorf("生成安全快照失败，为保护你的 hosts 已中止写入：%w", err)
	}

	if err := os.WriteFile(s.HostsPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("%s", platform.FriendlyWriteError(err, "autohostswitch"))
	}
	s.appendLog(actor)

	if w := WarningsText(issues); w != "" {
		// 警告不阻止流程，调用方决定展示
		return &applyWarnings{msg: actor + " 成功。\n但有几处提醒你看一下：\n" + w}
	}
	return nil
}

// applyWarnings 是“成功但有警告”的特殊返回，调用方可断言展示。
type applyWarnings struct{ msg string }

func (e *applyWarnings) Error() string { return e.msg }

// IsApplyWarnings 判断 err 是否为“成功但有警告”。
func IsApplyWarnings(err error) bool {
	_, ok := err.(*applyWarnings)
	return ok
}

// ReadCurrentHosts 读取当前 hosts 文本。
func (s *Store) ReadCurrentHosts() (string, error) {
	data, err := os.ReadFile(s.HostsPath)
	if err != nil {
		return "", fmt.Errorf("读取 hosts 失败：%w", err)
	}
	return string(data), nil
}
