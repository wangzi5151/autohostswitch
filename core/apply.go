package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// ConcurrentChangeError 表示“你读取之后、写入之前，hosts 被别的程序改过”。
// 为避免覆盖别人的修改，写入已中止。请重新读取后再操作。
type ConcurrentChangeError struct {
	// 内部用，不展示
}

func (e *ConcurrentChangeError) Error() string {
	return "检测到 hosts 在你读取之后被其他程序修改过（可能是另一个 AutoHostSwitch、文本编辑器或安全软件）。\n" +
		"为避免覆盖别人的修改，本次写入已中止——你的原文件没有被改动。\n" +
		"请重新读取最新内容，确认后再操作。"
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

// HashHosts 返回 hosts 文本的 SHA256，用于并发修改检测。
func HashHosts(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// ApplyResult 描述一次写入 hosts 的完整阶段，供 UI/CLI 向用户展示进度。
type ApplyResult struct {
	Steps    []string // 例如：已通过校验 → 已生成安全快照 → 已原子写入 → 已验证落盘
	Warnings string   // 非阻塞的警告文本（可空）
}

// ApplyHosts 是全工具唯一的 hosts 写入入口（不带并发 guard）。
// 固定流程：语法校验 → 并发检查 → 自动安全快照 → 原子写入 → 落盘验证 → 记日志。
func (s *Store) ApplyHosts(content, actor string) (*ApplyResult, error) {
	return s.ApplyHostsGuarded(content, actor, "")
}

// ApplyHostsGuarded 同 ApplyHosts，但多一层并发保护：
// expectHash 非空时，要求写入瞬间的文件哈希与它一致，否则报 ConcurrentChangeError。
// 调用方应在“用户看到内容的那一刻”计算哈希并传入。
//
// 写入临界区（哈希复核 → 安全快照 → 原子写入）持有进程间写入锁，
// 防止同一台机器上的另一个 AutoHostSwitch（CLI/Web）同时写入。
// 外部编辑器不走本锁，那种情况仍由哈希比对兜底。
// ApplyHostsGuarded 是写入 hosts 的统一出口：成功记“成功+安全快照名”，
// 失败记“失败：原因”。所有写入（CLI/Web、恢复、撤销）都经过这里，
// 所以操作历史天然完整。
func (s *Store) ApplyHostsGuarded(content, actor, expectHash string) (*ApplyResult, error) {
	res, err := s.applyHostsGuarded(content, actor, expectHash)
	if err != nil {
		s.appendLogEntry(s.Source, actor, "失败："+shortErr(err), "")
	}
	return res, err
}

func (s *Store) applyHostsGuarded(content, actor, expectHash string) (*ApplyResult, error) {
	res := &ApplyResult{}
	issues := ValidateHosts(content)
	if HasError(issues) {
		return nil, &ValidationError{Issues: issues}
	}
	content = NormalizeHosts(content)
	res.Steps = append(res.Steps, "已通过语法校验")

	lk, err := LockWrite(s.DataDir)
	if err != nil {
		return nil, err // ErrWriteLocked 会直接告诉用户稍后再试
	}
	defer lk.Unlock()

	current, err := os.ReadFile(s.HostsPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取当前 hosts 失败：%w", err)
	}
	// 并发修改检测：用户所见版本 vs 即将覆盖的版本（锁内复核，防止检查与写入之间被插队）
	if expectHash != "" && HashHosts(string(current)) != expectHash {
		return nil, &ConcurrentChangeError{}
	}
	if string(current) == content {
		return nil, fmt.Errorf("内容和当前 hosts 完全一样，不用写")
	}

	// 写权限预检：给中文提示而不是让 WriteFile 抛系统英文
	if ok, werr := platform.Writable(s.HostsPath); !ok {
		if os.IsPermission(werr) || werr == nil {
			return nil, fmt.Errorf("%s", platform.FriendlyWriteError(
				fmt.Errorf("permission denied"), "autohostswitch"))
		}
		return nil, fmt.Errorf("写前检查失败：%w", werr)
	}

	// 覆盖前自动生成安全快照（内容无变化时内部会跳过）
	safety, err := s.createSafetySnapshot(current)
	if err != nil {
		return nil, fmt.Errorf("生成安全快照失败，为保护你的 hosts 已中止写入：%w", err)
	}
	res.Steps = append(res.Steps, "已生成安全快照")

	// 原子写入：临时文件 → fsync → 校验 → 同目录 rename → 读回验证
	if err := s.atomicWrite(content); err != nil {
		return nil, err
	}
	res.Steps = append(res.Steps, "已原子写入", "已验证落盘内容一致")

	snapName := ""
	if safety != nil {
		snapName = safety.Name
	}
	s.appendLogEntry(s.Source, actor, "成功", snapName)
	// 记录写入后的哈希，供“外部修改检测”用
	m := s.getMeta()
	m.LastKnownHash = HashHosts(content)
	s.setMeta(m)
	_ = s.saveMeta()
	if w := WarningsText(issues); w != "" {
		res.Warnings = w
	}
	// 记录“上一次写入”，供“撤销”使用（快照 ID + 操作描述）
	s.recordLastApply(safety, actor)
	return res, nil
}

// shortErr 把错误信息截短，适合记日志（一行）。
func shortErr(err error) string {
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	if len([]rune(msg)) > 120 {
		msg = string([]rune(msg)[:120]) + "…"
	}
	return msg
}

// atomicWrite 把 content 原子地写入 hosts 文件：
//  1. 在同目录创建临时文件（保证 rename 时同文件系统，原子替换）
//  2. 写入后 Sync 落盘，再读回临时文件确认字节一致
//  3. rename 覆盖目标（POSIX 与 Windows 均为原子替换）
//  4. 读回目标文件，最终确认内容一致
//
// 即使在第 2 步之后程序崩溃/断电，原 hosts 文件也不受影响。
func (s *Store) atomicWrite(content string) error {
	dir := filepath.Dir(s.HostsPath)
	tmp, err := os.CreateTemp(dir, ".autohostswitch-*.tmp")
	if err != nil {
		return fmt.Errorf("%s", platform.FriendlyWriteError(err, "autohostswitch"))
	}
	tmpName := tmp.Name()
	// 任何失败都要清理临时文件
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s", platform.FriendlyWriteError(err, "autohostswitch"))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("刷盘失败（Sync）：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败：%w", err)
	}
	// 校验临时文件内容
	if data, err := os.ReadFile(tmpName); err != nil || string(data) != content {
		return fmt.Errorf("临时文件校验不一致，已中止写入（原文件未动）")
	}
	// 原子替换
	if err := os.Rename(tmpName, s.HostsPath); err != nil {
		return fmt.Errorf("%s", platform.FriendlyWriteError(err, "autohostswitch"))
	}
	// 尽力 fsync 目录（Unix），提高 rename 的持久性；失败不致命
	syncDir(dir)
	// 最终验证：读回确认
	if back, err := os.ReadFile(s.HostsPath); err != nil {
		return fmt.Errorf("写入后读回失败：%w", err)
	} else if string(back) != content {
		return fmt.Errorf("写入后校验不一致：落盘内容与预期不符，请检查磁盘")
	}
	success = true
	return nil
}

// ReadCurrentHosts 读取当前 hosts 文本。
func (s *Store) ReadCurrentHosts() (string, error) {
	data, err := os.ReadFile(s.HostsPath)
	if err != nil {
		return "", fmt.Errorf("读取 hosts 失败：%w", err)
	}
	return string(data), nil
}
