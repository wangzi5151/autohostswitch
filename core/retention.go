package core

import (
	"fmt"
	"os"
	"sort"
	"strconv"
)

// 自动快照保留上限：手动快照永久保留，自动快照只留最新的 N 个，
// 防止“无限快照”长期膨胀。可通过环境变量 AUTOHOSTSWITCH_AUTO_SNAP_MAX 覆盖。
const defaultMaxAutoSnapshots = 50

func maxAutoSnapshots() int {
	if v := os.Getenv("AUTOHOSTSWITCH_AUTO_SNAP_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultMaxAutoSnapshots
}

// pruneAutoSnapshots 裁剪超出上限的旧自动快照（最旧的先删）。
// 返回删除个数与释放字节数。
func (s *Store) pruneAutoSnapshots() (deleted int, freedBytes int64) {
	maxKeep := maxAutoSnapshots()
	list, err := s.loadSnapshots()
	if err != nil {
		return 0, 0
	}
	var autos []Snapshot
	for _, sn := range list {
		if sn.Auto && sn.ID != s.meta.OriginalSnapshotID {
			autos = append(autos, sn)
		}
	}
	// 按创建时间升序：最旧的在前
	sort.SliceStable(autos, func(i, j int) bool { return autos[i].CreatedAt < autos[j].CreatedAt })
	over := len(autos) - maxKeep
	if over <= 0 {
		return 0, 0
	}
	keep := map[string]bool{}
	for _, sn := range autos[over:] {
		keep[sn.ID] = true
	}
	var rest []Snapshot
	for _, sn := range list {
		if sn.Auto && sn.ID != s.meta.OriginalSnapshotID && !keep[sn.ID] {
			if fi, err := os.Stat(s.snapFile(sn.ID)); err == nil {
				freedBytes += fi.Size()
			}
			_ = os.Remove(s.snapFile(sn.ID))
			deleted++
			continue
		}
		rest = append(rest, sn)
	}
	_ = s.saveSnapshots(rest)
	return deleted, freedBytes
}

// PruneAutoSnapshots 对外：手动触发清理旧自动快照。
func (s *Store) PruneAutoSnapshots() (int, int64) {
	d, f := s.pruneAutoSnapshots()
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
