package core

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// backupVersion 是备份文件格式版本，未来改格式时用于兼容判断。
const backupVersion = 1

// Backup 是“导出整套”的 JSON 结构：快照 + 配置 + 日志，全程本地文件。
type Backup struct {
	App        string       `json:"app"`
	Version    int          `json:"version"`
	ExportedAt string       `json:"exported_at"`
	Snapshots  []backupSnap `json:"snapshots"`
	Profiles   []backupProf `json:"profiles"`
	Log        []string     `json:"log,omitempty"`
}

type backupSnap struct {
	Name      string `json:"name"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	Auto      bool   `json:"auto"`
	Content   string `json:"content"`
}

type backupProf struct {
	Name      string `json:"name"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Content   string `json:"content"`
}

// ExportAll 导出全部快照与配置为一个 JSON 备份，可用于换机/多设备同步。
func (s *Store) ExportAll() ([]byte, error) {
	b := Backup{
		App:        "autohostswitch",
		Version:    backupVersion,
		ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	snaps, err := s.loadSnapshots()
	if err != nil {
		return nil, err
	}
	for _, sn := range snaps {
		content, err := os.ReadFile(s.snapFile(sn.ID))
		if err != nil {
			continue // 文件丢了就跳过，不让整个导出失败
		}
		b.Snapshots = append(b.Snapshots, backupSnap{
			Name: sn.Name, Note: sn.Note, CreatedAt: sn.CreatedAt,
			Auto: sn.Auto, Content: string(content),
		})
	}
	profs, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	for _, p := range profs {
		content, err := os.ReadFile(s.profileFile(p.ID))
		if err != nil {
			continue
		}
		b.Profiles = append(b.Profiles, backupProf{
			Name: p.Name, Note: p.Note, CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt, Content: string(content),
		})
	}
	if logLines, err := s.readLog(); err == nil {
		b.Log = logLines
	}
	return json.MarshalIndent(b, "", "  ")
}

// ExportAllToFile 导出到文件。
func (s *Store) ExportAllToFile(path string) error {
	data, err := s.ExportAll()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写备份文件失败：%w", err)
	}
	s.appendLog(fmt.Sprintf("导出全部备份到 %s", path))
	return nil
}

// ImportAll 从 JSON 备份导入。已存在的同名快照/配置会自动改名，不覆盖。
// 返回 (导入快照数, 导入配置数, error)。
func (s *Store) ImportAll(data []byte) (int, int, error) {
	var b Backup
	if err := json.Unmarshal(data, &b); err != nil {
		return 0, 0, fmt.Errorf("备份文件不是合法的 JSON：%w", err)
	}
	if b.App != "autohostswitch" {
		return 0, 0, fmt.Errorf("这不是 AutoHostSwitch 的备份文件（app=%q）", b.App)
	}
	if b.Version > backupVersion {
		return 0, 0, fmt.Errorf("备份版本 v%d 太新，当前工具只认 v%d，请升级工具", b.Version, backupVersion)
	}
	ns, np := 0, 0
	for _, sn := range b.Snapshots {
		name := uniqueSnapName(s, sn.Name)
		if _, err := s.CreateSnapshotOfContent(name, sn.Note+"（导入于 "+nowStr()+"）", []byte(sn.Content), sn.Auto); err != nil {
			return ns, np, fmt.Errorf("导入快照「%s」失败：%w", sn.Name, err)
		}
		ns++
	}
	for _, p := range b.Profiles {
		name := uniqueProfileName(s, p.Name)
		if issues := ValidateHosts(p.Content); HasError(issues) {
			// 坏内容不入库，但不中断整个导入
			s.appendLog(fmt.Sprintf("导入跳过配置「%s」：内容校验没通过", p.Name))
			continue
		}
		if _, err := s.CreateProfile(name, p.Note+"（导入于 "+nowStr()+"）", p.Content); err != nil {
			return ns, np, fmt.Errorf("导入配置「%s」失败：%w", p.Name, err)
		}
		np++
	}
	s.appendLog(fmt.Sprintf("从备份导入：%d 个快照，%d 个配置", ns, np))
	return ns, np, nil
}

// uniqueSnapName / uniqueProfileName 在重名时自动加后缀。
func uniqueSnapName(s *Store, name string) string {
	if _, err := s.GetSnapshot(name); err != nil {
		return name
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s（导入%d）", name, i)
		if _, err := s.GetSnapshot(cand); err != nil {
			return cand
		}
	}
}

func uniqueProfileName(s *Store, name string) string {
	if _, err := s.GetProfile(name); err != nil {
		return name
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s（导入%d）", name, i)
		if _, err := s.GetProfile(cand); err != nil {
			return cand
		}
	}
}
