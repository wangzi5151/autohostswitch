package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- 快照异常 ----------

func TestSnapshotIndexCorrupted(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	// 把索引写成损坏的 JSON
	if err := os.WriteFile(filepath.Join(s.DataDir, "snapshots.json"), []byte("{broken json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.ListSnapshots()
	if err == nil || !strings.Contains(err.Error(), "损坏") {
		t.Fatalf("索引损坏应明确报错，实际：%v", err)
	}
}

func TestSnapshotFileMissing(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	snap, err := s.CreateSnapshot("t1", "")
	if err != nil {
		t.Fatal(err)
	}
	// 手动删掉快照文件（模拟磁盘损坏/误删）
	if err := os.Remove(s.snapFile(snap.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SnapshotContent(snap); err == nil {
		t.Fatal("快照文件丢失应报错")
	}
	if _, err := s.RestoreSnapshot(snap.ID); err == nil {
		t.Fatal("恢复丢失的快照应报错")
	}
	// 索引里仍有记录但文件没了，prune 不应崩
	s.PruneAutoSnapshots()
}

func TestRestoreAfterDelete(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	snap, err := s.CreateSnapshot("t1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSnapshot(snap.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreSnapshot(snap.ID); err == nil {
		t.Fatal("恢复已删除的快照应报错")
	}
}

// ---------- Undo 跨重启 ----------

func TestUndoAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	hosts := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	s1, err := NewStoreWith(hosts, data)
	if err != nil {
		t.Fatal(err)
	}
	after := "127.0.0.1 localhost\n9.9.9.9 dns.example\n"
	if _, err := s1.ApplyHosts(after, "测试写入"); err != nil {
		t.Fatal(err)
	}
	// 模拟重启：全新 Store 实例指向同一目录
	s2, err := NewStoreWith(hosts, data)
	if err != nil {
		t.Fatal(err)
	}
	la, ok := s2.LastApplyState()
	if !ok {
		t.Fatal("重启后撤销点应还在（last_apply.json 持久化）")
	}
	if la.Actor != "测试写入" {
		t.Fatalf("撤销点操作名不对：%s", la.Actor)
	}
	if _, err := s2.Undo(); err != nil {
		t.Fatal(err)
	}
	cur, _ := s2.ReadCurrentHosts()
	if cur != NormalizeHosts("127.0.0.1 localhost\n") {
		t.Fatalf("重启后撤销结果不对：%q", cur)
	}
}

// ---------- 校验边界 ----------

func TestValidateHosts_EdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"ipv6", "::1 localhost\nfe80::1 linklocal\n2001:db8::1 v6.example\n", false},
		{"crlf", "127.0.0.1 localhost\r\n# 注释\r\n", false},
		{"tab分隔", "127.0.0.1\tlocalhost\t\tmyhost\n", false},
		{"行尾注释", "127.0.0.1 localhost # 本机\n", false},
		{"空行空白行", "\n   \n127.0.0.1 localhost\n\n", false},
		{"多域名", "127.0.0.1 a b c.d e-f\n", false},
		{"重复域名警告", "127.0.0.1 a\n127.0.0.2 a\n", false}, // 重复是警告不是错误
		{"非法ip", "999.1.1.1 bad\n", true},
		{"空ip", " localhost\n", true},
		{"超长hostname", "127.0.0.1 " + strings.Repeat("a", 300) + "\n", true},
		{"空文件", "", false},
		{"纯注释", "# hello\n# world\n", false},
		{"单字符域名", "127.0.0.1 x\n", false},
		{"下划线警告", "127.0.0.1 my_host\n", false}, // 警告
		{"中文域名", "127.0.0.1 测试\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := ValidateHosts(c.content)
			gotErr := HasError(issues)
			if gotErr != c.wantErr {
				t.Errorf("ValidateHosts(%q) 错误=%v，期望错误=%v，issues=%v", c.name, gotErr, c.wantErr, issues)
			}
		})
	}
}

// ---------- 大文件 ----------

// genBigHosts 生成约 sizeMB 的 hosts 内容。
func genBigHosts(sizeMB int) string {
	var b strings.Builder
	b.Grow(sizeMB << 20)
	i := 0
	for b.Len() < sizeMB<<20 {
		fmt.Fprintf(&b, "127.0.0.%d host%d.example.com # 注释\n", i%254+1, i)
		i++
	}
	return b.String()
}

func BenchmarkParseHosts_1MB(b *testing.B) {
	content := genBigHosts(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		issues := ValidateHosts(content)
		if HasError(issues) {
			b.Fatal("1MB 生成内容不应有错误")
		}
	}
}

func BenchmarkNormalizeHosts_10MB(b *testing.B) {
	content := genBigHosts(10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizeHosts(content)
	}
}

func BenchmarkDiffHosts_1MB(b *testing.B) {
	a := genBigHosts(1)
	c := a + "9.9.9.9 new.example # 新增\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffHosts(a, c)
	}
}

// ---------- Profile ID/Name 回归（P0-1）----------

// TestProfileLifecycle 全生命周期：Create→Rename→Update→Apply→Reload→Delete，
// ID 在任何时候都不变，所有操作都按稳定 ID 生效。
func TestProfileLifecycle(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")

	// Create
	p, err := s.CreateProfile("dev", "", "127.0.0.1 localhost\n127.0.0.1 dev.local\n")
	if err != nil {
		t.Fatal(err)
	}
	origID := p.ID
	if origID == "" {
		t.Fatal("ID 不应为空")
	}

	// Rename：ID 必须不变
	if err := s.RenameProfile(origID, "development"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProfile(origID)
	if err != nil {
		t.Fatal("重命名后按 ID 查找失败")
	}
	if got.ID != origID || got.Name != "development" {
		t.Fatalf("重命名后 ID/Name 不对：%+v", got)
	}
	// 旧名字应找不到了
	if _, err := s.GetProfile("dev"); err == nil {
		t.Fatal("旧名字重命名后仍能查到")
	}

	// Update（用 ID）：内容必须写到同一个文件
	newContent := "127.0.0.1 localhost\n127.0.0.1 dev.local\n9.9.9.9 new.local\n"
	if err := s.UpdateProfileContent(origID, newContent); err != nil {
		t.Fatal(err)
	}
	c, err := s.ProfileContent(got)
	if err != nil {
		t.Fatal(err)
	}
	if c != NormalizeHosts(newContent) {
		t.Fatalf("按 ID 更新后内容不对：%q", c)
	}

	// Reload：新 Store 实例，ID 依然稳定
	s2, err := NewStoreWith(s.HostsPath, s.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := s2.GetProfile(origID)
	if err != nil {
		t.Fatal("reload 后按 ID 查找失败")
	}
	if got2.Name != "development" {
		t.Fatalf("reload 后名字丢了：%+v", got2)
	}

	// Apply（用 ID）
	if _, err := s2.ApplyProfile(origID); err != nil {
		t.Fatal(err)
	}
	cur, _ := s2.ReadCurrentHosts()
	if cur != NormalizeHosts(newContent) {
		t.Fatalf("按 ID 应用后 hosts 不对：%q", cur)
	}

	// Undo：撤销应用，回到之前
	if _, err := s2.Undo(); err != nil {
		t.Fatal(err)
	}

	// Delete（用 ID）
	if err := s2.DeleteProfile(origID); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.GetProfile(origID); err == nil {
		t.Fatal("删除后仍能查到")
	}
	if _, err := os.Stat(s2.profileFile(origID)); !os.IsNotExist(err) {
		t.Fatal("删除后内容文件还在")
	}
}

// TestProfileRenameSameName 改成相同名字应无操作成功。
func TestProfileRenameSameName(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	p, err := s.CreateProfile("dev", "", "127.0.0.1 localhost\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameProfile(p.ID, "dev"); err != nil {
		t.Fatalf("同名重命名应成功：%v", err)
	}
}

// ---------- 大文件全链路基准（P3-12）：防止性能倒退 ----------

func BenchmarkParseHosts_50MB(b *testing.B) {
	content := genBigHosts(50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if issues := ValidateHosts(content); HasError(issues) {
			b.Fatal("不应有错误")
		}
	}
}

// BenchmarkSnapshotRestore_10MB 快照创建 + 恢复全链路。
func BenchmarkSnapshotRestore_10MB(b *testing.B) {
	content := genBigHosts(10)
	dir := b.TempDir()
	hosts := dir + "/hosts"
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	s, err := NewStoreWith(hosts, dir+"/data")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap, err := s.CreateSnapshotOfContent("bench", "", []byte(content), false)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := s.RestoreSnapshot(snap.ID); err != nil {
			b.Fatal(err)
		}
		if err := s.DeleteSnapshot(snap.ID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExportImport_10MB 导出 + 导入全链路。
func BenchmarkExportImport_10MB(b *testing.B) {
	content := genBigHosts(10)
	dir := b.TempDir()
	hosts := dir + "/hosts"
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	s, err := NewStoreWith(hosts, dir+"/data")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := s.CreateSnapshotOfContent("bench", "", []byte(content), false); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, err := s.ExportAll()
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := s.ImportAll(data); err != nil {
			b.Fatal(err)
		}
	}
}
