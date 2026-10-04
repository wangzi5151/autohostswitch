package core

// 统一错误类型。
//
// 设计原则：
//   - 可判定：调用方用 errors.As 判断类型，而不是字符串匹配；
//   - 可映射：CLI 据此返回稳定退出码，Web 据此返回 HTTP 状态码；
//   - 可读：Error() 永远是中文人类可读文本，不抛堆栈。
//
// 退出码映射（CLI，见 cli.exitFor）：
//  0 成功；1 一般错误（含快照/配置不存在、快照损坏）；
//  2 hosts 校验失败；3 权限不足；4 并发冲突（外部修改 / 写入锁被占）。

import "fmt"

// SnapshotNotFoundError：按 ID 或名字找不到快照。
type SnapshotNotFoundError struct {
	Query string // 用户输入的 ID 或名字
}

func (e *SnapshotNotFoundError) Error() string {
	return fmt.Sprintf("找不到快照「%s」", e.Query)
}

// SnapshotCorruptedError：快照索引损坏或快照文件丢失/损坏。
type SnapshotCorruptedError struct {
	Name   string // 快照名（索引损坏时为空）
	Detail string
}

func (e *SnapshotCorruptedError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("快照「%s」已损坏：%s", e.Name, e.Detail)
	}
	return fmt.Sprintf("快照索引损坏：%s", e.Detail)
}

// ProfileNotFoundError：按 ID 或名字找不到配置集。
type ProfileNotFoundError struct {
	Query string
}

func (e *ProfileNotFoundError) Error() string {
	return fmt.Sprintf("找不到配置「%s」", e.Query)
}
