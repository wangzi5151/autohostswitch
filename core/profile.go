package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Profile 是一套 hosts 配置方案（如「开发环境」「屏蔽广告」）。
// 内容存在 profiles/<ID>.hosts，元信息在 profiles.json。
type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Store) profileIndexPath() string { return filepath.Join(s.DataDir, "profiles.json") }
func (s *Store) profileFile(id string) string {
	return filepath.Join(s.profileDir(), id+".hosts")
}

func (s *Store) loadProfiles() ([]Profile, error) {
	data, err := os.ReadFile(s.profileIndexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取配置索引失败：%w", err)
	}
	var list []Profile
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("配置索引损坏（%s）：%w", s.profileIndexPath(), err)
	}
	return list, nil
}

func (s *Store) saveProfiles(list []Profile) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.profileIndexPath(), data, 0o644)
}

// CreateProfile 新建配置集。content 会先做语法校验，坏内容拒绝入库。
func (s *Store) CreateProfile(name, note, content string) (*Profile, error) {
	if name == "" {
		return nil, fmt.Errorf("配置名不能为空")
	}
	if issues := ValidateHosts(content); HasError(issues) {
		return nil, &ValidationError{Issues: issues}
	}
	if _, err := s.GetProfile(name); err == nil {
		return nil, fmt.Errorf("已存在同名配置「%s」，请换个名字", name)
	}
	p := Profile{ID: newID(), Name: name, Note: note,
		CreatedAt: nowStr(), UpdatedAt: nowStr()}
	if err := os.WriteFile(s.profileFile(p.ID), []byte(NormalizeHosts(content)), 0o644); err != nil {
		return nil, fmt.Errorf("保存配置内容失败：%w", err)
	}
	list, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	list = append([]Profile{p}, list...)
	if err := s.saveProfiles(list); err != nil {
		return nil, err
	}
	s.appendLog(fmt.Sprintf("新建配置「%s」", name))
	return &p, nil
}

// ListProfiles 返回全部配置（新的在前）。
func (s *Store) ListProfiles() ([]Profile, error) {
	list, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })
	return list, nil
}

// GetProfile 按 ID 或 Name 查找配置。
func (s *Store) GetProfile(idOrName string) (*Profile, error) {
	list, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == idOrName || list[i].Name == idOrName {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("找不到配置「%s」", idOrName)
}

// ProfileContent 读取配置内容。
func (s *Store) ProfileContent(p *Profile) (string, error) {
	data, err := os.ReadFile(s.profileFile(p.ID))
	if err != nil {
		return "", fmt.Errorf("配置文件丢失（%s）：%w", p.Name, err)
	}
	return string(data), nil
}

// UpdateProfileContent 更新配置内容（同样走校验）。
func (s *Store) UpdateProfileContent(idOrName, content string) error {
	if issues := ValidateHosts(content); HasError(issues) {
		return &ValidationError{Issues: issues}
	}
	p, err := s.GetProfile(idOrName)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.profileFile(p.ID), []byte(NormalizeHosts(content)), 0o644); err != nil {
		return fmt.Errorf("保存配置内容失败：%w", err)
	}
	list, _ := s.loadProfiles()
	for i := range list {
		if list[i].ID == p.ID {
			list[i].UpdatedAt = nowStr()
		}
	}
	s.appendLog(fmt.Sprintf("编辑配置「%s」", p.Name))
	return s.saveProfiles(list)
}

// RenameProfile 重命名配置。
func (s *Store) RenameProfile(idOrName, newName string) error {
	if newName == "" {
		return fmt.Errorf("新名字不能为空")
	}
	if _, err := s.GetProfile(newName); err == nil {
		return fmt.Errorf("已存在同名配置「%s」", newName)
	}
	list, err := s.loadProfiles()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID == idOrName || list[i].Name == idOrName {
			list[i].Name = newName
			list[i].UpdatedAt = nowStr()
			s.appendLog(fmt.Sprintf("配置重命名为「%s」", newName))
			return s.saveProfiles(list)
		}
	}
	return fmt.Errorf("找不到配置「%s」", idOrName)
}

// DeleteProfile 删除配置。
func (s *Store) DeleteProfile(idOrName string) error {
	list, err := s.loadProfiles()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID == idOrName || list[i].Name == idOrName {
			_ = os.Remove(s.profileFile(list[i].ID))
			name := list[i].Name
			list = append(list[:i], list[i+1:]...)
			s.appendLog(fmt.Sprintf("删除配置「%s」", name))
			return s.saveProfiles(list)
		}
	}
	return fmt.Errorf("找不到配置「%s」", idOrName)
}

// ApplyProfile 一键应用配置：校验 → 自动安全快照 → 写入 → 日志。
func (s *Store) ApplyProfile(idOrName string) error {
	p, err := s.GetProfile(idOrName)
	if err != nil {
		return err
	}
	content, err := s.ProfileContent(p)
	if err != nil {
		return err
	}
	return s.ApplyHosts(content, fmt.Sprintf("应用配置「%s」", p.Name))
}

// SeedDefaultProfile 首次运行时种一颗“重置默认”配置，内容取自原始备份。
// 已存在则跳过，保证幂等。
func (s *Store) SeedDefaultProfile() error {
	if _, err := s.GetProfile("重置默认"); err == nil {
		return nil
	}
	raw, err := os.ReadFile(s.HostsPath)
	if err != nil {
		raw = []byte("# 空 hosts\n")
	}
	_, err = s.CreateProfile("重置默认", "回到系统最初的样子（首次运行时的 hosts）", string(raw))
	// 时间戳函数在 CreateProfile 内部已处理
	_ = time.Now
	return err
}
