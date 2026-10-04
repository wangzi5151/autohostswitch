package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateHosts_OK(t *testing.T) {
	content := `# 注释行
127.0.0.1 localhost mydev.local
::1 ip6-localhost

192.168.1.10 nas.home router.home  # 行内注释
`
	if issues := ValidateHosts(content); len(issues) != 0 {
		t.Fatalf("want no issues, got %+v", issues)
	}
}

func TestValidateHosts_Errors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"坏IP", "999.999.1.1 bad.local\n", true},
		{"缺域名", "127.0.0.1\n", true},
		{"缺IP", "justahost\n", true},
		{"非法字符", "127.0.0.1 bad;host\n", true},
		{"横杠开头", "127.0.0.1 -bad.local\n", true},
		{"空内容", "", false},
		{"纯注释", "# hello\n\n", false},
		{"CRLF", "127.0.0.1 a.local\r\n192.168.1.1 b.local\r\n", false},
		{"IPv6", "::1 localhost\nfe80::1 router.local\n", false},
	}
	for _, c := range cases {
		issues := ValidateHosts(c.content)
		if got := HasError(issues); got != c.wantErr {
			t.Errorf("%s: HasError=%v, want %v (issues=%+v)", c.name, got, c.wantErr, issues)
		}
	}
}

func TestValidateHosts_Warnings(t *testing.T) {
	content := "127.0.0.1 dup.local\n192.168.1.1 dup.local\n127.0.0.1 dup.local\n10.0.0.1 under_score.local\n"
	issues := ValidateHosts(content)
	if HasError(issues) {
		t.Fatalf("want only warnings, got %+v", issues)
	}
	if len(issues) < 3 {
		t.Fatalf("want >=3 warnings (conflict+dup+underscore), got %+v", issues)
	}
}

// testStore 创建隔离的 Store（临时 hosts + 临时数据目录）。
func testStore(t *testing.T, hostsContent string) *Store {
	t.Helper()
	hosts := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hosts, []byte(hostsContent), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStoreWith(hosts, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFirstRunBackup(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	if s.OriginalSnapshotID() == "" {
		t.Fatal("首次运行应自动备份原始 hosts")
	}
	snap, err := s.GetSnapshot(s.OriginalSnapshotID())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Name != "系统原始备份" {
		t.Fatalf("备份名不对：%s", snap.Name)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	snap, err := s.CreateSnapshot("测试快照", "备注")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	// 1 个原始备份 + 1 个手动
	if len(list) != 2 {
		t.Fatalf("want 2 snapshots, got %d", len(list))
	}
	if err := s.RenameSnapshot(snap.ID, "改名后", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSnapshot("改名后"); err != nil {
		t.Fatal(err)
	}
	// 原始备份拒绝删除
	if err := s.DeleteSnapshot(s.OriginalSnapshotID()); err == nil {
		t.Fatal("原始备份应该删不掉")
	}
	if err := s.DeleteSnapshot("改名后"); err != nil {
		t.Fatal(err)
	}
}

func TestProfileApply(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	p, err := s.CreateProfile("开发环境", "", "127.0.0.1 mydev.local\n192.168.1.5 api.test\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyProfile(p.Name); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.HostsPath)
	if string(data) != "127.0.0.1 mydev.local\n192.168.1.5 api.test\n" {
		t.Fatalf("hosts 内容不对：\n%s", data)
	}
	// 应用坏内容应被拒绝，且原文件不动
	if _, err := s.CreateProfile("坏的", "", "not an ip at all\n"); err == nil {
		t.Fatal("坏内容入库应该被拒绝")
	}
	// 自动安全快照应已生成（至少：原始备份 + 安全快照）
	snaps, _ := s.ListSnapshots()
	auto := 0
	for _, sn := range snaps {
		if sn.Auto {
			auto++
		}
	}
	if auto == 0 {
		t.Fatal("写入前应自动生成安全快照")
	}
}

func TestRestoreOriginal(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	if _, err := s.CreateProfile("p1", "", "10.0.0.1 x.local\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyProfile("p1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreOriginal(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.HostsPath)
	if string(data) != "127.0.0.1 localhost\n" {
		t.Fatalf("紧急恢复后内容不对：\n%s", data)
	}
}

func TestExportImport(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	if _, err := s.CreateProfile("开发", "", "127.0.0.1 d.local\n"); err != nil {
		t.Fatal(err)
	}
	data, err := s.ExportAll()
	if err != nil {
		t.Fatal(err)
	}
	s2 := testStore(t, "127.0.0.1 localhost\n")
	ns, np, err := s2.ImportAll(data)
	if err != nil {
		t.Fatal(err)
	}
	if np == 0 || ns == 0 {
		t.Fatalf("导入数量不对：snap=%d prof=%d", ns, np)
	}
	// 再导一次：重名应自动改名，不报错
	if _, _, err := s2.ImportAll(data); err != nil {
		t.Fatal(err)
	}
	profs, _ := s2.ListProfiles()
	if len(profs) < 2 {
		t.Fatalf("重名导入应保留两份，got %d", len(profs))
	}
}

func TestLog(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	s.appendLog("测试动作")
	lines, err := s.ReadLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("日志应有内容")
	}
	if err := s.ClearLog(); err != nil {
		t.Fatal(err)
	}
	lines, _ = s.ReadLog()
	if len(lines) != 0 {
		t.Fatal("清空后应无日志")
	}
}
