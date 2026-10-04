package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Snapshot 是一次 hosts 快照的元信息；文件本体存在 snapshots/<ID>.hosts。
type Snapshot struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	Auto      bool   `json:"auto"` // true=写入前自动生成的安全快照
	// —— 内容元数据（创建时一次算好，列表页直接读索引，不再逐个读文件）——
	Size  int64  `json:"size"`  // 快照文件字节数
	Lines int    `json:"lines"` // 行数
	Hash  string `json:"hash"`  // 内容 SHA256（用于快速比对/去重）
}

func (s *Store) snapIndexPath() string { return filepath.Join(s.DataDir, "snapshots.json") }
func (s *Store) snapFile(id string) string {
	return filepath.Join(s.snapDir(), id+".hosts")
}

// loadSnapshots 读取快照索引（按创建时间倒序）。
func (s *Store) loadSnapshots() ([]Snapshot, error) {
	data, err := os.ReadFile(s.snapIndexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取快照索引失败：%w", err)
	}
	var list []Snapshot
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, &SnapshotCorruptedError{Detail: fmt.Sprintf("解析 %s 失败：%v", s.snapIndexPath(), err)}
	}
	// 回填旧快照缺失的元数据（v0.5.0 之前创建的快照没有这些字段）
	dirty := false
	for i := range list {
		if list[i].Size == 0 && list[i].Hash == "" {
			if data, err := os.ReadFile(s.snapFile(list[i].ID)); err == nil {
				fillSnapMeta(&list[i], data)
				dirty = true
			}
		}
	}
	if dirty {
		_ = s.saveSnapshots(list) // 回填失败不致命，下次再试
	}
	return list, nil
}

func (s *Store) saveSnapshots(list []Snapshot) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.snapIndexPath(), data, 0o644)
}

// saveSnapshotFile 把快照内容落盘（内部用，不更新索引调用方负责）。
// fillSnapMeta 填充快照元数据（大小/行数/哈希）。
func fillSnapMeta(snap *Snapshot, content []byte) {
	snap.Size = int64(len(content))
	snap.Lines = CountLines(string(content))
	snap.Hash = HashHosts(string(content))
}

func (s *Store) saveSnapshotFile(snap Snapshot, content []byte) error {
	if err := os.WriteFile(s.snapFile(snap.ID), content, 0o644); err != nil {
		return fmt.Errorf("保存快照文件失败：%w", err)
	}
	list, err := s.loadSnapshots()
	if err != nil {
		return err
	}
	list = append([]Snapshot{snap}, list...)
	return s.saveSnapshots(list)
}

// CreateSnapshot 把当前 hosts 存成快照。note 可为空。
func (s *Store) CreateSnapshot(name, note string) (*Snapshot, error) {
	raw, err := os.ReadFile(s.HostsPath)
	if err != nil {
		return nil, fmt.Errorf("读取当前 hosts 失败：%w", err)
	}
	if name == "" {
		name = "手动快照 " + nowStr()
	}
	snap := Snapshot{ID: newID(), Name: name, Note: note, CreatedAt: nowStr()}
	fillSnapMeta(&snap, raw)
	if err := s.saveSnapshotFile(snap, raw); err != nil {
		return nil, err
	}
	s.appendLog(fmt.Sprintf("创建快照「%s」", name))
	return &snap, nil
}

// CreateSnapshotOfContent 把给定内容存成快照（导入、订阅等场景用）。
func (s *Store) CreateSnapshotOfContent(name, note string, content []byte, auto bool) (*Snapshot, error) {
	snap := Snapshot{ID: newID(), Name: name, Note: note, CreatedAt: nowStr(), Auto: auto}
	fillSnapMeta(&snap, content)
	if err := s.saveSnapshotFile(snap, content); err != nil {
		return nil, err
	}
	if auto {
		// 自动快照有保留上限，顺手裁剪最旧的
		s.pruneAutoSnapshots()
	}
	return &snap, nil
}

// ListSnapshots 返回全部快照（新的在前）。
func (s *Store) ListSnapshots() ([]Snapshot, error) {
	list, err := s.loadSnapshots()
	if err != nil {
		return nil, err
	}
	// 防御性排序：新的在前
	sort.SliceStable(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })
	return list, nil
}

// GetSnapshot 按 ID 或 Name 查找快照（Name 允许重名时返回第一个）。
func (s *Store) GetSnapshot(idOrName string) (*Snapshot, error) {
	list, err := s.loadSnapshots()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == idOrName || list[i].Name == idOrName {
			return &list[i], nil
		}
	}
	return nil, &SnapshotNotFoundError{Query: idOrName}
}

// SnapshotContent 读取快照的文件内容。
func (s *Store) SnapshotContent(snap *Snapshot) ([]byte, error) {
	data, err := os.ReadFile(s.snapFile(snap.ID))
	if err != nil {
		return nil, &SnapshotCorruptedError{Name: snap.Name, Detail: "快照文件丢失或无法读取：" + err.Error()}
	}
	return data, nil
}

// RenameSnapshot 重命名快照（备注可一并改，传空字符串表示不改备注）。
func (s *Store) RenameSnapshot(idOrName, newName, newNote string) error {
	if newName == "" {
		return fmt.Errorf("新名字不能为空")
	}
	list, err := s.loadSnapshots()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID == idOrName || list[i].Name == idOrName {
			list[i].Name = newName
			if newNote != "" {
				list[i].Note = newNote
			}
			s.appendLog(fmt.Sprintf("快照重命名为「%s」", newName))
			return s.saveSnapshots(list)
		}
	}
	return &SnapshotNotFoundError{Query: idOrName}
}

// DeleteSnapshot 删除快照。系统原始备份拒绝删除（那是救命稻草）。
func (s *Store) DeleteSnapshot(idOrName string) error {
	if idOrName == s.getMeta().OriginalSnapshotID {
		return fmt.Errorf("「系统原始备份」不能删除，它是紧急恢复的最后一道防线")
	}
	list, err := s.loadSnapshots()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID == idOrName || list[i].Name == idOrName {
			_ = os.Remove(s.snapFile(list[i].ID)) // 文件丢了也不致命
			name := list[i].Name
			list = append(list[:i], list[i+1:]...)
			s.appendLog(fmt.Sprintf("删除快照「%s」", name))
			return s.saveSnapshots(list)
		}
	}
	return &SnapshotNotFoundError{Query: idOrName}
}

// RestoreSnapshot 把快照内容写回 hosts（走统一写入通道：校验+安全快照+日志）。
func (s *Store) RestoreSnapshot(idOrName string) (*ApplyResult, error) {
	snap, err := s.GetSnapshot(idOrName)
	if err != nil {
		return nil, err
	}
	content, err := s.SnapshotContent(snap)
	if err != nil {
		return nil, err
	}
	return s.ApplyHosts(string(content), fmt.Sprintf("从快照「%s」恢复", snap.Name))
}

// RestoreOriginal 紧急一键恢复：写回首次运行备份的原始 hosts。
func (s *Store) RestoreOriginal() (*ApplyResult, error) {
	if s.getMeta().OriginalSnapshotID == "" {
		return nil, fmt.Errorf("没有找到原始备份，可能数据目录被手动清空过")
	}
	snap, err := s.GetSnapshot(s.getMeta().OriginalSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("原始备份快照丢失：%w", err)
	}
	content, err := s.SnapshotContent(snap)
	if err != nil {
		return nil, err
	}
	return s.ApplyHosts(string(content), "紧急恢复：还原为系统原始 hosts")
}

// createSafetySnapshot 在覆盖前自动生成安全快照。
// 如果当前内容与最近一次自动快照完全一致则跳过，避免快照泛滥。
// 返回实际创建的快照（跳过时返回 nil），供“撤销上一次操作”记录。
func (s *Store) createSafetySnapshot(current []byte) (*Snapshot, error) {
	list, err := s.loadSnapshots()
	if err != nil {
		return nil, err
	}
	for _, snap := range list {
		if !snap.Auto {
			continue
		}
		if data, err := os.ReadFile(s.snapFile(snap.ID)); err == nil && string(data) == string(current) {
			return &snap, nil // 已经有一份一模一样的安全快照，直接复用它
		}
		break // 只看最新的一份自动快照
	}
	snap, err := s.CreateSnapshotOfContent("自动安全快照 "+nowStr(),
		"写入 hosts 前自动生成，防止改错变砖", current, true)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// RestoreSnapshotGuarded 同 RestoreSnapshot，多一层并发保护。
func (s *Store) RestoreSnapshotGuarded(idOrName, expectHash string) (*ApplyResult, error) {
	snap, err := s.GetSnapshot(idOrName)
	if err != nil {
		return nil, err
	}
	content, err := s.SnapshotContent(snap)
	if err != nil {
		return nil, err
	}
	return s.ApplyHostsGuarded(string(content), "从快照「"+snap.Name+"」恢复", expectHash)
}

// RestoreOriginalGuarded 同 RestoreOriginal，多一层并发保护。
func (s *Store) RestoreOriginalGuarded(expectHash string) (*ApplyResult, error) {
	if s.getMeta().OriginalSnapshotID == "" {
		return nil, fmt.Errorf("没有找到原始备份，可能数据目录被手动清空过")
	}
	snap, err := s.GetSnapshot(s.getMeta().OriginalSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("原始备份快照丢失：%w", err)
	}
	content, err := s.SnapshotContent(snap)
	if err != nil {
		return nil, err
	}
	return s.ApplyHostsGuarded(string(content), "紧急恢复：还原为系统原始 hosts", expectHash)
}
