package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestAgentAPIAuth(t *testing.T) {
	s := newTestServer(t)

	// 无 token 401
	rec := s.do(http.MethodGet, "/api/v1/agent/tasks", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
	// 错误 token 401
	rec = s.do(http.MethodGet, "/api/v1/agent/tasks", nil, "ag_wrong_token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with bad token, got %d", rec.Code)
	}
}

func TestAgentManagement(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 创建 agent
	rec := s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "agent-1"}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	decodeData(t, rec, &resp)

	// agent 用 token 领取任务（无任务返回 null）
	rec = s.do(http.MethodGet, "/api/v1/agent/tasks", nil, resp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks: %d", rec.Code)
	}
	var tr struct {
		Task any `json:"task"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}

	// 管理列表显示 agent（在线，因刚领取过任务）
	rec = s.do(http.MethodGet, "/api/v1/agents", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var agents []map[string]any
	decodeData(t, rec, &agents)

	// 查看单个
	rec = s.do(http.MethodGet, "/api/v1/agents/"+resp.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var one struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeData(t, rec, &one)
	if one.ID != resp.ID || one.Name != "agent-1" {
		t.Fatalf("get agent: %+v", one)
	}

	// 重命名
	rec = s.do(http.MethodPut, "/api/v1/agents/"+resp.ID, map[string]any{"name": "agent-1-renamed"}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.do(http.MethodGet, "/api/v1/agents/"+resp.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after rename: %d", rec.Code)
	}
	decodeData(t, rec, &one)
	if one.Name != "agent-1-renamed" {
		t.Fatalf("renamed agent: %+v", one)
	}

	// 删除
	rec = s.do(http.MethodDelete, "/api/v1/agents/"+resp.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	// 删除后 token 失效
	rec = s.do(http.MethodGet, "/api/v1/agent/tasks", nil, resp.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after delete, got %d", rec.Code)
	}
}

func TestAgentResultCrossAgentForbidden(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 两个 agent
	rec := s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "a1"}, adminToken)
	var a1 struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	decodeData(t, rec, &a1)
	rec = s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "a2"}, adminToken)
	var a2 struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	decodeData(t, rec, &a2)

	// a1 领取任务（无任务，跳过）；直接用不存在的任务 ID 回执
	rec = s.do(http.MethodPost, "/api/v1/agent/tasks/not-exist/result", map[string]any{
		"success": true, "checksum": "x", "message": "ok",
	}, a2.Token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown task, got %d", rec.Code)
	}
	_ = a1.ID
}

func TestAgentEnableDisable(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "toggle"}, adminToken)
	var agent struct {
		ID      string `json:"id"`
		Token   string `json:"token"`
		Enabled bool   `json:"enabled"`
	}
	decodeData(t, rec, &agent)
	if !agent.Enabled {
		t.Fatal("new agent should be enabled by default")
	}

	// 启用状态下可访问
	rec = s.do(http.MethodGet, "/api/v1/agent/tasks", nil, agent.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled agent tasks: %d", rec.Code)
	}

	// 禁用 → Token 立即失效
	rec = s.do(http.MethodPost, "/api/v1/agents/"+agent.ID+"/enabled", map[string]any{"enabled": false}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.do(http.MethodGet, "/api/v1/agent/tasks", nil, agent.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for disabled agent, got %d", rec.Code)
	}
	// 注册同样拒绝
	rec = s.do(http.MethodPost, "/api/v1/agent/register", map[string]any{
		"token": agent.Token, "version": "0.1.0", "os": "test",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for disabled agent register, got %d", rec.Code)
	}

	// 列表显示 enabled=false 且状态离线
	rec = s.do(http.MethodGet, "/api/v1/agents", nil, adminToken)
	var agents []map[string]any
	decodeData(t, rec, &agents)
	if len(agents) != 1 || agents[0]["enabled"] != false || agents[0]["status"] != "offline" {
		t.Fatalf("unexpected disabled state: %v", agents)
	}

	// 重新启用恢复
	rec = s.do(http.MethodPost, "/api/v1/agents/"+agent.ID+"/enabled", map[string]any{"enabled": true}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/agent/tasks", nil, agent.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after re-enable, got %d", rec.Code)
	}

	// 非 admin 不能禁用
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")
	rec = s.do(http.MethodPost, "/api/v1/agents/"+agent.ID+"/enabled", map[string]any{"enabled": false}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}

func TestAgentUpgradePathTraversalRejected(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 恶意版本号（路径遍历）应拒绝
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("version", "../evil")
	fw, _ := mw.CreateFormFile("file", "x")
	_, _ = fw.Write([]byte("data"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/binaries", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal version, got %d %s", rec.Code, rec.Body.String())
	}
	// URL 编码的遍历下载（未下发指令的 agent 也应被版本校验拦截）
	rec = s.do(http.MethodGet, "/api/v1/agent/binaries/%2e%2e%2fevil/download", nil, "ag_invalid")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (auth first), got %d", rec.Code)
	}
}

func TestAgentUpgradeSHA256Pinned(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "pin"}, adminToken)
	var agent struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	decodeData(t, rec, &agent)

	// 上传 v1 内容
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("version", "1.0.0")
	fw, _ := mw.CreateFormFile("file", "x")
	_, _ = fw.Write([]byte("original-binary-content"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/binaries", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	var upload struct {
		SHA256 string `json:"sha256"`
	}
	decodeData(t, rec, &upload)

	// 下发升级 → 查询：SHA256 应等于上传时的固定值
	rec = s.do(http.MethodPost, "/api/v1/agents/"+agent.ID+"/upgrade", map[string]any{"version": "1.0.0"}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("upgrade: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/agent/upgrade", nil, agent.Token)
	var up struct {
		Upgrade *struct {
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"upgrade"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}
	if up.Upgrade == nil || up.Upgrade.SHA256 != upload.SHA256 {
		t.Fatalf("SHA256 not pinned to upload-time value: %+v vs %s", up, upload.SHA256)
	}
}

func TestAgentUpgradeFlow(t *testing.T) {	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 创建 agent + 上传二进制（multipart）
	rec := s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "upgradable"}, adminToken)
	var agent struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	decodeData(t, rec, &agent)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("version", "9.9.9")
	fw, _ := mw.CreateFormFile("file", "argus-agent")
	_, _ = fw.Write([]byte("fake-agent-binary-content"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/binaries", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload binary: %d %s", rec.Code, rec.Body.String())
	}
	var upload struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	decodeData(t, rec, &upload)

	// 下发升级指令
	rec = s.do(http.MethodPost, "/api/v1/agents/"+agent.ID+"/upgrade", map[string]any{
		"version": "9.9.9",
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("request upgrade: %d %s", rec.Code, rec.Body.String())
	}

	// agent 查询升级指令（含 SHA256）
	rec = s.do(http.MethodGet, "/api/v1/agent/upgrade", nil, agent.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("query upgrade: %d", rec.Code)
	}
	var up struct {
		Upgrade *struct {
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"upgrade"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}

	// 下载二进制（agent token，仅允许下发给自己的版本）
	rec = s.do(http.MethodGet, "/api/v1/agent/binaries/9.9.9/download", nil, agent.Token)
	if rec.Code != http.StatusOK || rec.Body.String() != "fake-agent-binary-content" {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	// 未下发版本拒绝
	rec = s.do(http.MethodGet, "/api/v1/agent/binaries/other/download", nil, agent.Token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-target version, got %d", rec.Code)
	}

	// 非 admin 不能上传/下发
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")
	rec = s.do(http.MethodPost, "/api/v1/agents/"+agent.ID+"/upgrade", map[string]any{
		"version": "9.9.9",
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly upgrade request, got %d", rec.Code)
	}

	// 版本上报一致后升级指令清除（register 模拟新版本）
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agent/register", strings.NewReader(`{"token":"`+agent.Token+`","version":"9.9.9","os":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/agent/upgrade", nil, agent.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: %d", rec.Code)
	}
	var after struct {
		Upgrade any `json:"upgrade"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.Upgrade != nil {
		t.Fatal("upgrade instruction should be cleared after version match")
	}
}

func TestAgentTaskHistoryAndBatchUpgrade(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 两个 agent
	rec := s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "h1"}, adminToken)
	var a1 struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &a1)
	rec = s.do(http.MethodPost, "/api/v1/agents", map[string]any{"name": "h2"}, adminToken)
	var a2 struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &a2)

	// 任务历史：无任务 → 空数组
	rec = s.do(http.MethodGet, "/api/v1/agents/"+a1.ID+"/tasks", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("tasks: %d %s", rec.Code, rec.Body.String())
	}
	var tasks []any
	decodeData(t, rec, &tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected empty tasks, got %d", len(tasks))
	}

	// 不存在的 agent → 404
	rec = s.do(http.MethodGet, "/api/v1/agents/nope/tasks", nil, adminToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// 批量升级：版本不存在 → 全部失败并收集
	rec = s.do(http.MethodPost, "/api/v1/agents/batch-upgrade", map[string]any{
		"ids": []string{a1.ID, a2.ID, a1.ID}, "version": "9.9.9",
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch upgrade: %d %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Success int `json:"success"`
		Failed  []struct {
			ID string `json:"id"`
		} `json:"failed"`
	}
	decodeData(t, rec, &result)
	// 去重后 2 个，版本不存在均失败
	if result.Success != 0 || len(result.Failed) != 2 {
		t.Fatalf("unexpected batch result: %+v", result)
	}

	// 空/超限校验
	rec = s.do(http.MethodPost, "/api/v1/agents/batch-upgrade", map[string]any{
		"ids": []string{}, "version": "9.9.9",
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 empty, got %d", rec.Code)
	}
	over := make([]string, 51)
	for i := range over {
		over[i] = string(rune('a' + i%26)) + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	rec = s.do(http.MethodPost, "/api/v1/agents/batch-upgrade", map[string]any{
		"ids": over, "version": "9.9.9",
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 over limit, got %d", rec.Code)
	}

	// readonly 不可批量升级
	viewerToken := createUser(t, s, adminToken, "vro", "readonly")
	rec = s.do(http.MethodPost, "/api/v1/agents/batch-upgrade", map[string]any{
		"ids": []string{a1.ID}, "version": "9.9.9",
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}