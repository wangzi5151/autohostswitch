package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wangzi5151/autohostswitch/core"
)

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

	for _, host := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.1", "localhost", ""} {
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == 403 {
			t.Errorf("Host %q 不应被拦截", host)
		}
	}
	for _, host := range []string{"evil.com", "attacker.com:8080", "127.0.0.1.evil.com"} {
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("Host %q 应被 403 拦截，实际 %d", host, rec.Code)
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
	if err := readJSON(req, &v); err == nil {
		t.Error("未知字段 hacker 未被拒绝")
	}
	// 合法 JSON 通过
	req = httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"x"}`))
	if err := readJSON(req, &v); err != nil || v.Name != "x" {
		t.Errorf("合法 JSON 被拒绝：%v", err)
	}
}

func TestPathIDRejectsTraversal(t *testing.T) {
	sv := newTestServer(t)
	for _, p := range []string{"/api/snapshots/..%2f..%2fetc", "/api/snapshots/a!b"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Host = "127.0.0.1"
		rec := httptest.NewRecorder()
		sv.Handler().ServeHTTP(rec, req)
		if rec.Code != 400 && rec.Code != 404 {
			t.Errorf("%s 应被 400/404 拦截，实际 %d", p, rec.Code)
		}
	}
}

func TestOptionsAPIRejected(t *testing.T) {
	sv := newTestServer(t)
	req := httptest.NewRequest("OPTIONS", "/api/hosts", nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	sv.Handler().ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Errorf("OPTIONS /api/hosts 应 405，实际 %d", rec.Code)
	}
}
