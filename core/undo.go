package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// lastApply 记录“上一次成功写入 hosts 之前”的安全快照，
// 是“一键撤销”的依据：撤销 = 用这份快照把 hosts 写回去。
// 撤销本身也是一次写入，所以撤销后会产生新的撤销点——
// 用户可以在“改前 / 改后”两个状态之间来回切换，每一步都有快照兜底。
type lastApply struct {
	SnapshotID   string `json:"snapshot_id"`
	SnapshotName string `json:"snapshot_name"`
	Actor        string `json:"actor"` // 当时做了什么，例如“应用配置「开发环境」”
	At           string `json:"at"`
}

func (s *Store) lastApplyPath() string { return filepath.Join(s.DataDir, "last_apply.json") }

// recordLastApply 在每次成功写入后记录撤销点。写入失败时不调用，
// 所以 last_apply.json 永远指向“最后一次成功的写入之前”的状态。
func (s *Store) recordLastApply(safety *Snapshot, actor string) {
	if safety == nil {
		return
	}
	la := lastApply{
		SnapshotID:   safety.ID,
		SnapshotName: safety.Name,
		Actor:        actor,
		At:           nowStr(),
	}
	data, err := json.MarshalIndent(la, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.lastApplyPath(), data, 0o644)
}

// LastApplyState 返回当前是否可以撤销，以及撤销会回到哪个状态。
// ok=false 表示没有可撤销的操作（比如数据目录被清空过，或撤销用的快照被手动删了）。
func (s *Store) LastApplyState() (la lastApply, ok bool) {
	data, err := os.ReadFile(s.lastApplyPath())
	if err != nil {
		return lastApply{}, false
	}
	var v lastApply
	if err := json.Unmarshal(data, &v); err != nil || v.SnapshotID == "" {
		return lastApply{}, false
	}
	// 快照还在才算数（用户可能手动删了快照）
	if _, err := s.GetSnapshot(v.SnapshotID); err != nil {
		return lastApply{}, false
	}
	return v, true
}

// clearLastApply 清除撤销点（撤销成功后调用，一次撤销只允许一次）。
func (s *Store) clearLastApply() {
	_ = os.Remove(s.lastApplyPath())
}

// Undo 撤销上一次写入：把 hosts 恢复到上次写入之前的安全快照。
// 撤销本身也是一次受保护的写入（走 ApplyHostsGuarded：校验 + 新的安全快照 +
// 原子写入 + 写入锁），并会产生新的撤销点，所以可以在改前/改后之间来回切换——
// 每一步都有快照，不会丢。
func (s *Store) Undo() (*ApplyResult, error) {
	la, ok := s.LastApplyState()
	if !ok {
		return nil, errors.New("没有可撤销的操作（本次数据目录下还没有成功写入过 hosts，或撤销用的快照被删了）")
	}
	snap, err := s.GetSnapshot(la.SnapshotID)
	if err != nil {
		s.clearLastApply()
		return nil, fmt.Errorf("撤销用的快照找不到了：%w", err)
	}
	content, err := s.SnapshotContent(snap)
	if err != nil {
		return nil, err
	}
	// 注意：不要先清撤销点。ApplyHosts 成功后会自动把撤销点更新为
	// “撤销前”的状态（可来回切换）；如果写入失败，旧撤销点保留仍然有效。
	res, err := s.ApplyHosts(string(content), fmt.Sprintf("撤销上一次操作（%s）", la.Actor))
	if err != nil {
		return nil, err
	}
	return res, nil
}
