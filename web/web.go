// Package web 提供内置的轻量 Web UI：只监听 127.0.0.1，
// 前端是 go:embed 打进二进制的单文件 ui.html，无需任何额外依赖。
//
// 安全说明：
//   - 默认只绑本地回环地址，不对外暴露；
//   - 任何写入 hosts 的操作都走 core.ApplyHosts（校验+安全快照+日志）；
//   - 本服务本身不产生外发网络请求（订阅拉取是用户手动触发的独立功能）。
package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wangzi5151/autohostswitch/core"
	"github.com/wangzi5151/autohostswitch/platform"
)

//go:embed ui.html
var uiHTML []byte

// Server 持有数据存储并对外提供 HTTP API。
type Server struct {
	Store   *Store2
	Version string
}

// Store2 是 core.Store 的别名，保持包面干净。
// （直接用 core.Store 亦可，这里显式声明便于以后扩展。）
type Store2 = core.Store

// NewServer 创建服务实例。
func NewServer(store *core.Store, version string) *Server {
	return &Server{Store: store, Version: version}
}

// Handler 返回配置好路由的 http.Handler。
// 安全：所有“写”请求（POST/PUT/DELETE）经过 csrfGuard，
// 校验 Origin/Referer 防止恶意网页借浏览器偷调本地 API。
func (sv *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", sv.handleIndex)
	mux.HandleFunc("GET /api/status", sv.handleStatus)

	mux.HandleFunc("GET /api/hosts", sv.handleGetHosts)
	mux.HandleFunc("POST /api/hosts", sv.handlePostHosts)
	mux.HandleFunc("POST /api/validate", sv.handleValidate)

	mux.HandleFunc("GET /api/snapshots", sv.handleListSnapshots)
	mux.HandleFunc("POST /api/snapshots", sv.handleCreateSnapshot)
	mux.HandleFunc("GET /api/snapshots/{id}", sv.handleDownloadSnapshot)
	mux.HandleFunc("PUT /api/snapshots/{id}", sv.handleRenameSnapshot)
	mux.HandleFunc("DELETE /api/snapshots/{id}", sv.handleDeleteSnapshot)
	mux.HandleFunc("POST /api/snapshots/{id}/restore", sv.handleRestoreSnapshot)

	mux.HandleFunc("GET /api/profiles", sv.handleListProfiles)
	mux.HandleFunc("POST /api/profiles", sv.handleCreateProfile)
	mux.HandleFunc("GET /api/profiles/{id}", sv.handleGetProfile)
	mux.HandleFunc("PUT /api/profiles/{id}", sv.handleUpdateProfile)
	mux.HandleFunc("DELETE /api/profiles/{id}", sv.handleDeleteProfile)
	mux.HandleFunc("POST /api/profiles/{id}/apply", sv.handleApplyProfile)

	mux.HandleFunc("GET /api/export", sv.handleExport)
	mux.HandleFunc("POST /api/import", sv.handleImport)
	mux.HandleFunc("POST /api/restore-original", sv.handleRestoreOriginal)

	mux.HandleFunc("GET /api/log", sv.handleGetLog)
	mux.HandleFunc("POST /api/log/clear", sv.handleClearLog)

	mux.HandleFunc("POST /api/subscribe", sv.handleSubscribe)
	mux.HandleFunc("GET /api/diff", sv.handleDiff)
	mux.HandleFunc("GET /api/snapshots/usage", sv.handleSnapshotsUsage)
	mux.HandleFunc("POST /api/snapshots/prune", sv.handlePruneSnapshots)

	mux.HandleFunc("GET /api/undo-state", sv.handleUndoState)
	mux.HandleFunc("POST /api/undo", sv.handleUndo)
	return csrfGuard(mux)
}

// csrfGuard：localhost 工具的 CSRF 防线。
// 浏览器发起的跨站请求一定会带 Origin（fetch POST）或 Referer（表单），
// 我们要求它必须是本地来源；curl 等非浏览器请求不带 Origin，直接放行。
// 注意：我们从不设置 CORS 头，浏览器默认的同源策略本就拦掉大部分跨站读取。
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		if origin != "" && !isLocalOrigin(origin) {
			writeErr(w, 403, "已拦截：检测到非本页面发起的修改请求（Origin 校验失败）。"+
				"如果你是在浏览器里点的按钮，请确认地址栏是 http://127.0.0.1:8080 本机地址。")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalOrigin 判断 origin 是否本地来源（http(s)://127.0.0.1|localhost|[::1][:port]）。
func isLocalOrigin(origin string) bool {
	rest := origin
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	host := rest
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		// 小心 IPv6：[::1]:8080
		if strings.HasPrefix(rest, "[") {
			if j := strings.Index(rest, "]"); j >= 0 {
				host = rest[1:j]
			}
		} else if strings.Count(rest, ":") == 1 {
			host = rest[:i]
		}
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// ListenAndServe 在 addr（默认 127.0.0.1:8080）上启动服务。
func (sv *Server) ListenAndServe(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	// 安全护栏：拒绝绑定到非本地地址，防止用户误配对外暴露
	if !isLoopbackAddr(addr) {
		return fmt.Errorf("为安全起见，Web UI 只允许绑定本地地址（如 127.0.0.1:8080），拒绝：%s", addr)
	}
	return http.ListenAndServe(addr, sv.Handler())
}

func isLoopbackAddr(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// ---------- 小工具 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// errCode 把写入类错误映射成 HTTP 状态码：409 留给并发冲突，
// 前端据此提示用户“重新读取后再操作”。
func errCode(err error) int {
	if _, ok := err.(*core.ConcurrentChangeError); ok {
		return 409
	}
	return 400
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4MB 上限
	if err != nil {
		return fmt.Errorf("读取请求失败：%w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("请求不是合法 JSON：%w", err)
	}
	return nil
}

// ---------- 页面与状态 ----------

func (sv *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(uiHTML)
}

func (sv *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writable, werr := platform.Writable(sv.Store.HostsPath)
	snaps, _ := sv.Store.ListSnapshots()
	profs, _ := sv.Store.ListProfiles()
	hint := ""
	if !writable {
		hint = platform.ElevateHint("autohostswitch")
		_ = werr
	}
	writeJSON(w, 200, map[string]any{
		"version":      sv.Version,
		"os":           platform.OSName(),
		"is_termux":    platform.IsTermux(),
		"hosts_path":   sv.Store.HostsPath,
		"writable":     writable,
		"elevate_hint": hint,
		"data_dir":     sv.Store.DataDir,
		"snapshots":    len(snaps),
		"profiles":     len(profs),
		"has_original": sv.Store.OriginalSnapshotID() != "",
	})
}

// ---------- hosts ----------

func (sv *Server) handleGetHosts(w http.ResponseWriter, r *http.Request) {
	content, err := sv.Store.ReadCurrentHosts()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writable, _ := platform.Writable(sv.Store.HostsPath)
	total, active := core.CountStats(content)
	writeJSON(w, 200, map[string]any{
		"path": content, "writable": writable,
		"total_lines": total, "active_lines": active,
		"content_hash": core.HashHosts(content),
	})
}

func (sv *Server) handlePostHosts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content      string `json:"content"`
		ExpectedHash string `json:"expected_hash"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	res, err := sv.Store.ApplyHostsGuarded(req.Content, "Web：直接编辑 hosts 并保存", req.ExpectedHash)
	if err != nil {
		writeErr(w, errCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": "true", "steps": res.Steps, "warnings": res.Warnings})
}

func (sv *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	issues := core.ValidateHosts(req.Content)
	writeJSON(w, 200, map[string]any{"issues": issues, "has_error": core.HasError(issues)})
}

// ---------- 快照 ----------

func snapJSON(s core.Snapshot) map[string]any {
	return map[string]any{
		"id": s.ID, "name": s.Name, "note": s.Note,
		"created_at": s.CreatedAt, "auto": s.Auto,
		"is_original": false,
	}
}

func (sv *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	list, err := sv.Store.ListSnapshots()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	orig := sv.Store.OriginalSnapshotID()
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		m := snapJSON(s)
		m["is_original"] = s.ID == orig
		// 行数：恢复前确认时展示“当前 N 行 vs 快照 M 行”
		if content, err := sv.Store.SnapshotContent(&s); err == nil {
			m["lines"] = core.CountLines(string(content))
		}
		out = append(out, m)
	}
	writeJSON(w, 200, out)
}

func (sv *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	snap, err := sv.Store.CreateSnapshot(req.Name, req.Note)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, snapJSON(*snap))
}

func (sv *Server) handleDownloadSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snap, err := sv.Store.GetSnapshot(id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	content, err := sv.Store.SnapshotContent(snap)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="snapshot-`+snap.ID+".hosts\"")
	_, _ = w.Write(content)
}

func (sv *Server) handleRenameSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := sv.Store.RenameSnapshot(id, req.Name, req.Note); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "true"})
}

func (sv *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := sv.Store.DeleteSnapshot(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "true"})
}

func (sv *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	res, err := sv.Store.RestoreSnapshot(r.PathValue("id"))
	if err != nil {
		writeErr(w, errCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": "true", "steps": res.Steps, "warnings": res.Warnings})
}

// ---------- 配置集 ----------

func (sv *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	list, err := sv.Store.ListProfiles()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (sv *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Note    string `json:"note"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	p, err := sv.Store.CreateProfile(req.Name, req.Note, req.Content)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (sv *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	p, err := sv.Store.GetProfile(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	content, err := sv.Store.ProfileContent(p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"profile": p, "content": content})
}

func (sv *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id := r.PathValue("id")
	if req.Name != "" {
		if err := sv.Store.RenameProfile(id, req.Name); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		// 重命名后用新名字继续更新内容
		id = req.Name
	}
	if err := sv.Store.UpdateProfileContent(id, req.Content); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "true"})
}

func (sv *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := sv.Store.DeleteProfile(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "true"})
}

func (sv *Server) handleApplyProfile(w http.ResponseWriter, r *http.Request) {
	res, err := sv.Store.ApplyProfile(r.PathValue("id"))
	if err != nil {
		writeErr(w, errCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": "true", "steps": res.Steps, "warnings": res.Warnings})
}

// ---------- 备份 / 恢复 / 日志 ----------

func (sv *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	data, err := sv.Store.ExportAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="autohostswitch-backup.json"`)
	_, _ = w.Write(data)
}

func (sv *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeErr(w, 400, "读取上传文件失败")
		return
	}
	ns, np, err := sv.Store.ImportAll(data)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": "true", "snapshots": ns, "profiles": np})
}

func (sv *Server) handleRestoreOriginal(w http.ResponseWriter, r *http.Request) {
	res, err := sv.Store.RestoreOriginal()
	if err != nil {
		writeErr(w, errCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": "true", "steps": res.Steps, "warnings": res.Warnings})
}

func (sv *Server) handleGetLog(w http.ResponseWriter, r *http.Request) {
	lines, err := sv.Store.ReadLog()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, lines)
}

func (sv *Server) handleClearLog(w http.ResponseWriter, r *http.Request) {
	if err := sv.Store.ClearLog(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "true"})
}

// ---------- 订阅（手动触发，默认关闭） ----------

func (sv *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	content, err := core.PullSubscription(req.URL)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	issues := core.ValidateHosts(content)
	writeJSON(w, 200, map[string]any{
		"content":   content,
		"issues":    issues,
		"has_error": core.HasError(issues),
		"warning":   core.SubscribeWarning,
	})
}

// ---------- Diff ----------

// handleDiff 对两个来源做 diff。
// 参数：from / to，取值为 current、snapshot:<名>、profile:<名>。
func (sv *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d, err := sv.Store.DiffSources(q.Get("from"), q.Get("to"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	added, removed := core.DiffSummary(d)
	out := make([]map[string]any, 0, len(d))
	for _, l := range d {
		out = append(out, map[string]any{"op": string(l.Op), "text": l.Text})
	}
	writeJSON(w, 200, map[string]any{
		"lines": out, "added": added, "removed": removed,
	})
}

// ---------- 快照磁盘占用 ----------

func (sv *Server) handleSnapshotsUsage(w http.ResponseWriter, r *http.Request) {
	bytes, count := sv.Store.SnapshotsDiskUsage()
	writeJSON(w, 200, map[string]any{"bytes": bytes, "count": count})
}

func (sv *Server) handlePruneSnapshots(w http.ResponseWriter, r *http.Request) {
	d, f := sv.Store.PruneAutoSnapshots()
	writeJSON(w, 200, map[string]any{"ok": "true", "deleted": d, "freed_bytes": f})
}

// handleUndoState 返回当前是否可撤销（GET，无副作用）。
func (sv *Server) handleUndoState(w http.ResponseWriter, r *http.Request) {
	la, ok := sv.Store.LastApplyState()
	if !ok {
		writeJSON(w, 200, map[string]any{"can_undo": false})
		return
	}
	writeJSON(w, 200, map[string]any{
		"can_undo":      true,
		"actor":         la.Actor,
		"at":            la.At,
		"snapshot_name": la.SnapshotName,
	})
}

// handleUndo 撤销上一次写入（POST，副作用操作，受 CSRF 保护）。
func (sv *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	res, err := sv.Store.Undo()
	if err != nil {
		writeErr(w, errCode(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": "true", "steps": res.Steps, "warnings": res.Warnings})
}
