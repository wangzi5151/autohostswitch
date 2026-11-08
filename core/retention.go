package core

import (
	"fmt"
	"os"
	"strconv"
)

// 自动快照保留上限：手动快照永久保留，自动快照只留最新的 N 个，
// 防止“无限快照”长期膨胀。可通过环境变量 AUTOHOSTSWITCH_AUTO_SNAP_MAX 覆盖。
const defaultMaxAutoSnapshots = 50

func maxAutoSnapshots() int {
	if v := os.Getenv("AUTOHOSTSWITCH_AUTO_SNAP_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			// 至少保留 1 个：设为 0 会让刚创建的安全快照立刻被裁掉，Undo 失效。
			if n < 1 {
				n = 1
			}
			return n
		}
	}
	return defaultMaxAutoSnapshots
}

// pruneAutoSnapshotsLocked 裁剪超出上限的旧自动快照（最旧的先删）。
// 调用方必须已持有 dataMu。返回删除个数与释放字节数。
//
// 永远保留：原始备份、最新一份自动快照（刚创建的安全快照）、
// 以及 last_apply.json 指向的撤销点——即使上限被设成 1 也不能把撤销点删掉。
func (s *Store) pruneAutoSnapshotsLocked() (deleted int, freedBytes int64, err error) {
	maxKeep := maxAutoSnapshots()
	list, err := s.loadSnapshots()
	if err != nil {
		return 0, 0, err
	}
	origID := s.getMeta().OriginalSnapshotID
	// list 由 loadSnapshots 返回，索引顺序就是“新的在前”（创建时 prepend）。
	// 不能按 CreatedAt 排序：同一秒内创建的快照时间戳相同，稳定排序会保留
	// “新在前”的顺序，反而把最旧的当成最新保留下来。
	var autos []Snapshot
	for _, sn := range list {
		if sn.Auto && sn.ID != origID {
			autos = append(autos, sn) // 新 → 旧
		}
	}
	if len(autos) <= maxKeep {
		return 0, 0, nil
	}
	keep := map[string]bool{}
	// 保留最新的 maxKeep 个（autos 前 maxKeep 个）
	for i := 0; i < len(autos) && i < maxKeep; i++ {
		keep[autos[i].ID] = true
	}
	// 保护撤销点（可能比最新 N 个更旧）
	if la, ok := s.LastApplyState(); ok {
		keep[la.SnapshotID] = true
	}
	var rest []Snapshot
	for _, sn := range list {
		if sn.Auto && sn.ID != origID && !keep[sn.ID] {
			if fi, statErr := os.Stat(s.snapFile(sn.ID)); statErr == nil {
				freedBytes += fi.Size()
			}
			_ = os.Remove(s.snapFile(sn.ID))
			deleted++
			continue
		}
		rest = append(rest, sn)
	}
	if err := s.saveSnapshots(rest); err != nil {
		return deleted, freedBytes, err
	}
	return deleted, freedBytes, nil
}

// PruneAutoSnapshots 对外：手动触发清理旧自动快照。
func (s *Store) PruneAutoSnapshots() (int, int64) {
	s.dataMu.Lock()
	d, f, err := s.pruneAutoSnapshotsLocked()
	s.dataMu.Unlock()
	if err != nil {
		s.appendLog(fmt.Sprintf("清理旧自动快照失败：%s", shortErr(err)))
		return d, f
	}
	if d > 0 {
		s.appendLog(fmt.Sprintf("清理旧自动快照 %d 个，释放 %s", d, formatBytes(f)))
	}
	return d, f
}

// SnapshotsDiskUsage 返回快照总占用（字节）与个数。
func (s *Store) SnapshotsDiskUsage() (bytes int64, count int) {
	list, err := s.loadSnapshots()
	if err != nil {
		return 0, 0
	}
	for _, sn := range list {
		if fi, err := os.Stat(s.snapFile(sn.ID)); err == nil {
			bytes += fi.Size()
			count++
		}
	}
	return bytes, count
}

// formatBytes 人性化字节数。
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
