package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wangzi5151/autohostswitch/core"
)

// apiReq 构造带本机 token 的 API 请求（无 token 会 401）。
func apiReq(sv *Server, method, path string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.Host = "127.0.0.1"
	req.Header.Set("X-AutoHostSwitch-Token", sv.Token)
	return req
}

// 新建一个带临时数据目录的测试 Server。
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := core.NewStoreWith("/nonexistent-hosts-test", dir)
	if err != nil {
		// NewStoreWith 在 hosts 不存在时也应能建 store（只影响首次备份）
		t.Logf("store: %v", err)
	}
	return NewServer(st, "test")
}

func TestHostGuard(t *testing.T) {
	sv := newTestServer(t)
	h := hostGuard(sv.Handler())

	for _, host := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.1", "localhost", "[::1]", "::1", ""} {
		req := apiReq(sv, "GET", "/api/status", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == 403 || rec.Code == 401 {
			t.Errorf("Host %q 不应被拦截（code=%d）", host, rec.Code)
		}
	}
	for _, host := range []string{"evil.com", "attacker.com:8080", "127.0.0.1.evil.com", "[::1].evil.com", "0.0.0.0"} {
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("Host %q 应被 403 拦截，实际 %d", host, rec.Code)
		}
	}
}

func TestHostOnly(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":          "127.0.0.1",
		"127.0.0.1:8080":     "127.0.0.1",
		"localhost":          "localhost",
		"localhost:8080":     "localhost",
		"[::1]":              "::1",
		"[::1]:8080":         "::1",
		"::1":                "::1",
		"evil.com":           "evil.com",
		"evil.com:8080":      "evil.com",
		"[::1].evil.com":     "[::1].evil.com",
		"127.0.0.1.evil.com": "127.0.0.1.evil.com",
	}
	for in, want := range cases {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestValidID(t *testing.T) {
	ok := []string{"20261004-120000-a1b2c3d4", "abc_123", "name.v1"}
	for _, id := range ok {
		if !validID(id) {
			t.Errorf("合法 ID %q 被拒绝", id)
		}
	}
	bad := []string{"", "../x", "..\\x", "a/b", "a b", "a!b", "a$b", strings.Repeat("a", 200)}
	for _, id := range bad {
		if validID(id) {
			t.Errorf("非法 ID %q 未被拒绝", id)
		}
	}
}

func TestReadJSONStrict(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}
	// 未知字段应被拒绝
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x","hacker":1}`))
	rec := httptest.NewRecorder()
	if err := readJSON(rec, req, &v); err == nil {
		t.Error("未知字段 hacker 未被拒绝")
	}
	// 合法 JSON 通过
	req = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x"}`))
	rec = httptest.NewRecorder()
	if err := readJSON(rec, req, &v); err != nil || v.Name != "x" {
		t.Errorf("合法 JSON 被拒绝：%v", err)
	}
}

func TestReadJSONBodyTooLarge(t *testing.T) {
	sv := newTestServer(t)
	// 正好 4MB：应通过（JSON 本身合法）
	body := `{"name":"` + strings.Repeat("a", maxJSONBody-20) + `"}`
	req := apiReq(sv, "POST", "/api/snapshots", strings.NewReader(body))
	rec := httptest.NewRecorder()
	sv.Handler().ServeHTTP(rec, req)
	if rec.Code == 413 {
		t.Error("正好 4MB 的请求不应被 413")
	}
	// 超过 4MB：必须 413
	body = `{"name":"` + strings.Repeat("a", maxJSONBody) + `"}`
	req = apiReq(sv, "POST", "/api/snapshots", strings.NewReader(body))
	rec = httptest.NewRecorder()
	sv.Handler().ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Errorf("超大请求体应 413，实际 %d", rec.Code)
	}
	// chunked 超大请求：同样 413
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(`{"name":"`))
		chunk := []byte(strings.Repeat("a", 1<<20))
		for i := 0; i < 6; i++ {
			pw.Write(chunk)
		}
		pw.Write([]byte(`"}`))
		pw.Close()
	}()
	req = apiReq(sv, "POST", "/api/snapshots", pr)
	rec = httptest.NewRecorder()
	sv.Handler().ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Errorf("chunked 超大请求应 413，实际 %d", rec.Code)
	}
}

func TestTokenGuard(t *testing.T) {
	sv := newTestServer(t)
	h := sv.Handler()
	// 无 token → 401
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("无 token 应 401，实际 %d", rec.Code)
	}
	// 错误 token → 401
	req = httptest.NewRequest("GET", "/api/status", nil)
	req.Host = "127.0.0.1"
	req.Header.Set("X-AutoHostSwitch-Token", "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("错误 token 应 401，实际 %d", rec.Code)
	}
	// 正确 token → 放行
	req = apiReq(sv, "GET", "/api/status", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("正确 token 应 200，实际 %d", rec.Code)
	}
	// 首页不需要 token（它是 token 的载体），且 HTML 里注入了 token
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "127.0.0.1"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("首页应 200，实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), sv.Token) {
		t.Error("首页 HTML 未注入 token")
	}
	// 每次启动 token 不同
	if sv2 := newTestServer(t); sv2.Token == sv.Token {
		t.Error("两次启动的 token 不应相同")
	}
}

func TestPathIDRejectsTraversal(t *testing.T) {
	sv := newTestServer(t)
	for _, p := range []string{"/api/snapshots/..%2f..%2fetc", "/api/snapshots/a!b"} {
		req := apiReq(sv, "GET", p, nil)
		rec := httptest.NewRecorder()
		sv.Handler().ServeHTTP(rec, req)
		if rec.Code != 400 && rec.Code != 404 {
			t.Errorf("%s 应被 400/404 拦截，实际 %d", p, rec.Code)
		}
	}
}

func TestOptionsAPIRejected(t *testing.T) {
	sv := newTestServer(t)
	req := apiReq(sv, "OPTIONS", "/api/hosts", nil)
	rec := httptest.NewRecorder()
	sv.Handler().ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("OPTIONS /api/hosts 应 405，实际 %d", rec.Code)
	}
}

func TestHostsStatsCache(t *testing.T) {
	dir := t.TempDir()
	hosts := dir + "/hosts"
	os.WriteFile(hosts, []byte("127.0.0.1 a\n127.0.0.1 b c\n"), 0o644)
	st, _ := core.NewStoreWith(hosts, dir+"/data")
	sv := NewServer(st, "test")

	fi1, _ := os.Stat(hosts)
	t1, a1, d1 := sv.cachedHostsStats(fi1)
	if t1 != 2 || a1 != 2 || d1 != 3 {
		t.Fatalf("统计不对：%d/%d/%d", t1, a1, d1)
	}
	// 文件不变 → 缓存命中（即使文件内容被外部改了但 mtime/size 没变，这里只验证命中逻辑）
	t2, _, _ := sv.cachedHostsStats(fi1)
	if t2 != t1 {
		t.Fatal("缓存未命中")
	}
	// 文件变化 → 缓存失效重算
	os.WriteFile(hosts, []byte("127.0.0.1 a\n127.0.0.1 b\n127.0.0.1 d\n"), 0o644)
	fi2, _ := os.Stat(hosts)
	t3, _, _ := sv.cachedHostsStats(fi2)
	if t3 != 3 {
		t.Fatalf("文件变化后应重算，got %d", t3)
	}
}

func TestIsLocalOrigin(t *testing.T) {
	good := []string{
		"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080",
		"http://127.0.0.1", "https://localhost", "http://[::1]",
	}
	for _, o := range good {
		if !isLocalOrigin(o) {
			t.Errorf("%q 应判为本地", o)
		}
	}
	bad := []string{
		"http://evil.com", "http://127.0.0.1.evil.com", "http://[::1].evil.com",
		"https://localhost.evil.com", "http://0.0.0.0:8080",
	}
	for _, o := range bad {
		if isLocalOrigin(o) {
			t.Errorf("%q 不应判为本地", o)
		}
	}
}
