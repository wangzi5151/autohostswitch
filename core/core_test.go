package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	res, err := s.ApplyProfile(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("写入应返回执行阶段")
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
	if _, err := s.ApplyProfile("p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreOriginal(); err != nil {
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

func TestAtomicWriteNoTempLeft(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	if _, err := s.ApplyHosts("127.0.0.1 a.local\n", "测试写入"); err != nil {
		t.Fatal(err)
	}
	// 同目录不应残留临时文件
	entries, _ := os.ReadDir(filepath.Dir(s.HostsPath))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".autohostswitch-") {
			t.Fatalf("残留临时文件：%s", e.Name())
		}
	}
	data, _ := os.ReadFile(s.HostsPath)
	if string(data) != "127.0.0.1 a.local\n" {
		t.Fatalf("内容不对：%q", data)
	}
}

func TestConcurrentChangeGuard(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	cur, _ := s.ReadCurrentHosts()
	hash := HashHosts(cur)
	// 模拟：用户读取之后，别的程序改了 hosts
	_ = os.WriteFile(s.HostsPath, []byte("10.0.0.1 evil.local\n"), 0o644)
	_, err := s.ApplyHostsGuarded("127.0.0.1 mine.local\n", "测试", hash)
	if _, ok := err.(*ConcurrentChangeError); !ok {
		t.Fatalf("应报并发冲突，got %v", err)
	}
	// 外部修改不应被覆盖
	data, _ := os.ReadFile(s.HostsPath)
	if string(data) != "10.0.0.1 evil.local\n" {
		t.Fatalf("外部修改被覆盖了：%q", data)
	}
	// hash 对上时正常写入
	cur2, _ := s.ReadCurrentHosts()
	if _, err := s.ApplyHostsGuarded("127.0.0.1 mine.local\n", "测试", HashHosts(cur2)); err != nil {
		t.Fatalf("hash 一致时应能写入：%v", err)
	}
}

func TestConcurrentChangeGuardEmpty(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	// 不传 hash = 不启用 guard（兼容老调用）
	if _, err := s.ApplyHostsGuarded("127.0.0.1 b.local\n", "测试", ""); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHosts_Compat(t *testing.T) {
	// 兼容性：非标准但可能有效的写法 → 警告，不阻止
	warnCases := []string{
		"127.0.0.1 -weird.local\n",      // 横杠开头
		"127.0.0.1 weird-.local\n",      // 横杠结尾
		"127.0.0.1 under_score.local\n", // 下划线
		"127.0.0.1 singlelabel\n",       // 单标签内网名
		"127.0.0.1 a.local.\n",          // FQDN 尾点
		"127.0.0.1\ttab.local\n",        // TAB 分隔
		"127.0.0.1 UPPER.LOCAL\n",       // 大写
	}
	for _, c := range warnCases {
		issues := ValidateHosts(c)
		if HasError(issues) {
			t.Errorf("应为警告而非错误 %q: %+v", c, issues)
		}
	}
	// IPv6 各种写法
	ipv6Cases := []string{
		"::1 localhost\n",
		"fe80::1 link.local\n",
		"2001:db8::ff00:42:8329 v6.local\n",
		"::ffff:192.168.1.1 mapped.local\n",
	}
	for _, c := range ipv6Cases {
		if issues := ValidateHosts(c); HasError(issues) {
			t.Errorf("合法 IPv6 被拒绝 %q: %+v", c, issues)
		}
	}
	// 真正的错误：照样阻止
	errCases := []string{
		"127.0.0.1 bad;host\n",
		"127.0.0.1\n",
		"999.999.999.999 x.local\n",
		"127.0.0.1 a..b.local\n", // 空节
	}
	for _, c := range errCases {
		if issues := ValidateHosts(c); !HasError(issues) {
			t.Errorf("应为错误 %q", c)
		}
	}
}

func TestDiffHosts(t *testing.T) {
	a := "127.0.0.1 a.local\n127.0.0.1 b.local\n"
	b := "127.0.0.1 a.local\n192.168.1.1 c.local\n"
	d := DiffHosts(a, b)
	added, removed := DiffSummary(d)
	if added != 1 || removed != 1 {
		t.Fatalf("added=%d removed=%d, want 1/1\n%s", added, removed, RenderDiff(d))
	}
	if d := DiffHosts(a, a); len(d) != 2 {
		t.Fatalf("相同内容应全为不变行，got %d", len(d))
	}
	for _, l := range DiffHosts(a, a) {
		if l.Op != ' ' {
			t.Fatalf("相同内容不应有增删：%+v", l)
		}
	}
}

func TestSnapshotRetention(t *testing.T) {
	t.Setenv("AUTOHOSTSWITCH_AUTO_SNAP_MAX", "5")
	s := testStore(t, "127.0.0.1 localhost\n")
	// 造 8 个自动快照（内容各不相同，避免被去重跳过）
	for i := 0; i < 8; i++ {
		if _, err := s.CreateSnapshotOfContent("auto", "", []byte("data"+string(rune('a'+i))), true); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.ListSnapshots()
	autos := 0
	for _, sn := range list {
		if sn.Auto {
			autos++
		}
	}
	if autos != 5 {
		t.Fatalf("自动快照应被裁到 5 个，实际 %d", autos)
	}
	// 手动快照不受影响
	if _, err := s.CreateSnapshot("手动", ""); err != nil {
		t.Fatal(err)
	}
	// 磁盘占用统计
	bytes, count := s.SnapshotsDiskUsage()
	if count == 0 || bytes == 0 {
		t.Fatal("磁盘占用统计应非零")
	}
	// 一键清理
	s2 := testStore(t, "127.0.0.1 localhost\n")
	d, f := s2.PruneAutoSnapshots()
	if d != 0 || f != 0 {
		t.Fatalf("没有可清理时应返回 0，got %d/%d", d, f)
	}
}

func TestWriteLockExclusive(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	l1, err := LockWrite(s.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Unlock()
	// 第二个锁必须拿不到
	if _, err := LockWrite(s.DataDir); !errors.Is(err, ErrWriteLocked) {
		t.Fatalf("第二个写入锁应该被拒绝，实际：%v", err)
	}
	// 释放后能拿到
	if err := l1.Unlock(); err != nil {
		t.Fatal(err)
	}
	l2, err := LockWrite(s.DataDir)
	if err != nil {
		t.Fatalf("释放后应能拿到锁：%v", err)
	}
	l2.Unlock()
}

func TestApplyWhileLocked(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	lk, err := LockWrite(s.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Unlock()
	// 锁被占时写入应直接拒绝，而不是卡住
	_, err = s.ApplyHosts("127.0.0.1 localhost\n1.2.3.4 example.com\n", "测试")
	if !errors.Is(err, ErrWriteLocked) {
		t.Fatalf("锁定时写入应报 ErrWriteLocked，实际：%v", err)
	}
}

func TestUndoRoundTrip(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	before := "127.0.0.1 localhost\n"
	after := "127.0.0.1 localhost\n9.9.9.9 dns.example\n"
	if _, err := s.ApplyHosts(after, "测试写入"); err != nil {
		t.Fatal(err)
	}
	// 有撤销点
	la, ok := s.LastApplyState()
	if !ok {
		t.Fatal("写入后应有撤销点")
	}
	if la.Actor != "测试写入" {
		t.Fatalf("撤销点记录的操作不对：%s", la.Actor)
	}
	// 撤销回到之前
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.ReadCurrentHosts()
	if cur != NormalizeHosts(before) {
		t.Fatalf("撤销后内容不对：%q", cur)
	}
	// 撤销后产生新撤销点（可来回切换 = 重做）
	la2, ok := s.LastApplyState()
	if !ok {
		t.Fatal("撤销后应产生新的撤销点")
	}
	if la2.SnapshotID == la.SnapshotID {
		t.Fatal("撤销后的撤销点应该指向新快照")
	}
	if _, err := s.Undo(); err != nil {
		t.Fatal(err)
	}
	cur, _ = s.ReadCurrentHosts()
	if cur != NormalizeHosts(after) {
		t.Fatalf("再次撤销（重做）后内容不对：%q", cur)
	}
}

func TestUndoNothing(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	if _, err := s.Undo(); err == nil {
		t.Fatal("没有写入过时撤销应报错")
	}
}

func TestUndoAfterSnapshotDeleted(t *testing.T) {
	s := testStore(t, "127.0.0.1 localhost\n")
	if _, err := s.ApplyHosts("127.0.0.1 localhost\n1.1.1.1 one.example\n", "测试"); err != nil {
		t.Fatal(err)
	}
	la, ok := s.LastApplyState()
	if !ok {
		t.Fatal("应有撤销点")
	}
	// 手动删掉撤销用的快照
	if err := s.DeleteSnapshot(la.SnapshotID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LastApplyState(); ok {
		t.Fatal("快照被删后撤销点应失效")
	}
	if _, err := s.Undo(); err == nil {
		t.Fatal("快照被删后撤销应报错")
	}
}
