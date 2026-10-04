package core

import (
	"fmt"
	"strings"
)

// ResolveSource 把 diff/预览用的来源标识解析成 hosts 文本。
// 合法形式：
//   - "current"            当前 hosts
//   - "snapshot:<名或ID>"   某个快照
//   - "profile:<名或ID>"    某个配置
func (s *Store) ResolveSource(src string) (string, error) {
	if src == "current" {
		return s.ReadCurrentHosts()
	}
	if name, ok := strings.CutPrefix(src, "snapshot:"); ok {
		snap, err := s.GetSnapshot(name)
		if err != nil {
			return "", err
		}
		data, err := s.SnapshotContent(snap)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if name, ok := strings.CutPrefix(src, "profile:"); ok {
		p, err := s.GetProfile(name)
		if err != nil {
			return "", err
		}
		return s.ProfileContent(p)
	}
	return "", fmt.Errorf("来源格式不对：「%s」，应为 current、snapshot:<名> 或 profile:<名>", src)
}

// DiffSources 对两个来源做 diff。
func (s *Store) DiffSources(from, to string) ([]DiffLine, error) {
	a, err := s.ResolveSource(from)
	if err != nil {
		return nil, fmt.Errorf("读取来源 %q 失败：%w", from, err)
	}
	b, err := s.ResolveSource(to)
	if err != nil {
		return nil, fmt.Errorf("读取来源 %q 失败：%w", to, err)
	}
	return DiffHosts(a, b), nil
}
