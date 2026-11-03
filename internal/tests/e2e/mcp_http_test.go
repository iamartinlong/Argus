//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestMCPHTTPEndToEnd 验证 M2：MCP over Streamable HTTP。
// 起真实 argus serve（mcp.enabled=true）+ 创建 API Token，
// 用裸 HTTP JSON-RPC 走 initialize → tools/list → tools/call，
// 并验证未认证被拒、scope 收敛、跨源防护。
func TestMCPHTTPEndToEnd(t *testing.T) {
	exe := os.Getenv("ARGUS_E2E_BIN")
	if exe == "" {
		t.Skip("ARGUS_E2E_BIN 未设置")
	}
	work := t.TempDir()
	port := 18801
	cfg := filepath.Join(work, "config.yaml")
	cfgBody := fmt.Sprintf(`server:
  listen: "127.0.0.1:%d"
storage:
  driver: sqlite
  dsn: %s
mcp:
  enabled: true
  allow_key_download: false
`, port, filepath.Join(work, "argus.db"))
	if err := os.WriteFile(cfg, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"ARGUS_JWT_SECRET=mcp-http-jwt-0123456789abcdef0123456789",
		"ARGUS_MASTER_KEY=mcp-http-master-0123456789abcdef0123456789",
	)
	srv := exec.Command(exe, "serve", "--config", cfg)
	srv.Env = env
	srv.Dir = work
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Process.Kill() }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitHealthy(t, base)

	// 初始管理员 + 登录
	admin := exec.Command(exe, "--config", cfg, "init-admin", "--username", "admin", "--password", "McpHttp12345")
	admin.Env = env
	admin.Dir = work
	_, _ = admin.CombinedOutput()

	login := doJSON(t, http.MethodPost, base+"/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "McpHttp12345",
	}, nil)
	token := nestedString(t, login, "access_token")
	if token == "" {
		t.Fatalf("登录失败: %s", mustJSON(t, login))
	}

	// 首次登录强制改密：改为同值后重新登录（与真实流程一致）
	pwStatus, pwBody := mcpRestPut(t, base+"/api/v1/users/me/password", "Bearer "+token, map[string]any{
		"old_password": "McpHttp12345", "new_password": "McpHttp12345",
	})
	if pwStatus != http.StatusOK {
		t.Fatalf("强制改密失败: status=%d body=%s", pwStatus, pwBody)
	}
	login = doJSON(t, http.MethodPost, base+"/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "McpHttp12345",
	}, nil)
	token = nestedString(t, login, "access_token")
	if token == "" {
		t.Fatalf("改密后重新登录失败: %s", mustJSON(t, login))
	}

	// 只读 token（无 certificates:write）
	roToken := createToken(t, base, token, "mcp-http-ro", []string{"certificates:read", "deploy:read", "accounts:read", "admin:read"})

	// ---------- 1. 未认证必须被拒 ----------
	status, _ := mcpPost(t, base+"/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("未认证请求应 401，got %d", status)
	}

	// ---------- 2. 管理员 JWT：initialize + tools/list ----------
	resp := mcpCall(t, base+"/mcp", token, 1, "initialize", map[string]any{
		"protocolVersion": "2026-07-28",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "e2e", "version": "0"},
	})
	if errMsg := nestedString(t, resp, "error.message"); errMsg != "" {
		t.Fatalf("initialize 失败: %s", errMsg)
	}
	if name := nestedString(t, resp, "result.serverInfo.name"); name != "argus" {
		t.Fatalf("serverInfo.name = %q", name)
	}

	resp = mcpCall(t, base+"/mcp", token, 2, "tools/list", nil)
	tools := nestedArray(t, resp, "result.tools")
	if len(tools) != 12 {
		t.Fatalf("管理员应看到 12 个工具，got %d", len(tools))
	}
	// 工具名集合应含查询与写操作
	names := map[string]bool{}
	for _, tv := range tools {
		if m, ok := tv.(map[string]any); ok {
			names[fmt.Sprint(m["name"])] = true
		}
	}
	for _, want := range []string{"list_certificates", "issue_certificate", "revoke_certificate"} {
		if !names[want] {
			t.Fatalf("管理员工具集缺少 %s", want)
		}
	}

	// ---------- 3. tools/call 实调 ----------
	resp = mcpCall(t, base+"/mcp", token, 3, "tools/call", map[string]any{
		"name": "list_certificates", "arguments": map[string]any{},
	})
	if nestedBool(t, resp, "result.isError") {
		t.Fatalf("list_certificates 不应报错: %s", mustJSON(t, resp))
	}

	// ---------- 4. 只读 token：工具集收敛，写工具不可见 ----------
	_ = mcpCall(t, base+"/mcp", token, 4, "initialize", map[string]any{
		"protocolVersion": "2026-07-28", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "e2e", "version": "0"},
	})
	resp = mcpCallAPI(t, base+"/mcp", roToken, 5, "tools/list", nil)
	tools = nestedArray(t, resp, "result.tools")
	roNames := map[string]bool{}
	for _, tv := range tools {
		if m, ok := tv.(map[string]any); ok {
			roNames[fmt.Sprint(m["name"])] = true
		}
	}
	if !roNames["list_certificates"] {
		t.Fatal("只读 token 应能看到 list_certificates")
	}
	for _, banned := range []string{"issue_certificate", "renew_certificate", "revoke_certificate", "import_certificate", "deploy_certificate"} {
		if roNames[banned] {
			t.Fatalf("只读 token 不应看到 %s", banned)
		}
	}

	// ---------- 5. API Token 经 X-API-Token 头也能用 ----------
	status, body := mcpPost(t, base+"/mcp", "", `{"jsonrpc":"2.0","id":6,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}`)
	_ = body
	if status != http.StatusUnauthorized {
		t.Fatalf("空 token 应 401，got %d", status)
	}
	// 带 X-API-Token
	req, _ := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Token", roToken)
	req.Header.Set("Accept", "application/json, text/event-stream")
	client := &http.Client{Timeout: 30 * time.Second}
	r, err := client.Do(req)
	if err != nil {
		t.Fatalf("X-API-Token 请求失败: %v", err)
	}
	defer r.Body.Close()
	rb, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("X-API-Token 应 200，got %d: %s", r.StatusCode, rb)
	}

	// ---------- 6. 跨源防护：恶意 Origin 应被拒 ----------
	req2, _ := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":8,"method":"initialize","params":{}}`)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Origin", "https://evil.example.com")
	r2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("跨源请求失败: %v", err)
	}
	defer r2.Body.Close()
	if r2.StatusCode == http.StatusOK {
		t.Fatal("恶意 Origin 未被跨源防护拦截")
	}
	t.Logf("跨源请求状态: %d（非 200 即符合预期）", r2.StatusCode)

	// ---------- 7. mcp.enabled=false 时应 404 ----------
	cfg2 := filepath.Join(work, "off.yaml")
	_ = os.WriteFile(cfg2, []byte(fmt.Sprintf(`server:
  listen: "127.0.0.1:%d"
storage:
  driver: sqlite
  dsn: %s
mcp:
  enabled: false
`, port+1, filepath.Join(work, "off.db"))), 0o600)
	srv2 := exec.Command(exe, "serve", "--config", cfg2)
	srv2.Env = env
	srv2.Dir = work
	if err := srv2.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv2.Process.Kill() }()
	waitHealthy(t, fmt.Sprintf("http://127.0.0.1:%d", port+1))
	status, _ = mcpPost(t, fmt.Sprintf("http://127.0.0.1:%d/mcp", port+1), "Bearer "+token, `{"jsonrpc":"2.0","id":9,"method":"initialize","params":{}}`)
	if status != http.StatusNotFound {
		t.Fatalf("mcp.enabled=false 时应 404，got %d", status)
	}
}

// ---------- 辅助 ----------

func waitHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("服务端未就绪: %s", base)
}

// doJSON 普通 REST 调用，返回解析后的 data 字段
func doJSON(t *testing.T, method, url, auth string, body any, out any) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(rb, &env)
	var m map[string]any
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &m)
	}
	return m
}

func createToken(t *testing.T, base, jwt, name string, scopes []string) string {
	t.Helper()
	status, body := mcpRestPost(t, base+"/api/v1/api-tokens", "Bearer "+jwt,
		map[string]any{"name": name, "scopes": scopes})
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("创建 token 失败: status=%d body=%s", status, body)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("解析 token 响应失败: %v body=%s", err, body)
	}
	tok, _ := env.Data["token"].(string)
	if tok == "" {
		t.Fatalf("token 响应无 token 字段: %s", body)
	}
	return tok
}

// mcpRestPost 带认证的 POST，返回状态码与原始响应体
func mcpRestPost(t *testing.T, url, auth string, body any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(rb)
}

// mcpRestPut 带认证的 PUT，返回状态码（用于强制改密流程）
func mcpRestPut(t *testing.T, url, auth string, body any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(rb)
}

// mcpPost 裸 POST 到 /mcp，返回状态码与响应体
func mcpPost(t *testing.T, url, auth, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(rb)
}

// mcpCall 发一次 JSON-RPC 调用并解析响应（支持 SSE 与 JSON 两种响应形态）
// mcpCall 用 Authorization: Bearer（JWT 会话）发起 MCP 调用。
func mcpCall(t *testing.T, url, auth string, id int, method string, params map[string]any) map[string]any {
	t.Helper()
	return mcpCallAuth(t, url, "bearer", auth, id, method, params)
}

// mcpCallAPI 用 X-API-Token 头发起 MCP 调用（API Token 的标准用法）。
// 注意：API Token 不能放 Authorization: Bearer——那会进 JWT 校验分支被拒。
func mcpCallAPI(t *testing.T, url, apiToken string, id int, method string, params map[string]any) map[string]any {
	t.Helper()
	return mcpCallAuth(t, url, "apitoken", apiToken, id, method, params)
}

func mcpCallAuth(t *testing.T, url, authType, auth string, id int, method string, params map[string]any) map[string]any {
	t.Helper()
	reqBody := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		reqBody["params"] = params
	}
	b, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		if authType == "apitoken" {
			req.Header.Set("X-API-Token", auth)
		} else {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s 状态 %d: %s", method, resp.StatusCode, rb)
	}
	// SSE 形态：取 data: 行的 JSON；JSON 形态：直接解析
	text := string(rb)
	if bytes.Contains(rb, []byte("data:")) {
		for _, line := range bytes.Split(rb, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if bytes.HasPrefix(line, []byte("data:")) {
				text = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))))
				break
			}
		}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s 响应解析失败: %v\nraw: %s", method, err, rb)
	}
	return out
}

func nestedString(t *testing.T, m map[string]any, path string) string {
	t.Helper()
	cur := any(m)
	for _, part := range splitPath(path) {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = mm[part]
		if !ok {
			return ""
		}
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

func nestedBool(t *testing.T, m map[string]any, path string) bool {
	t.Helper()
	cur := any(m)
	for _, part := range splitPath(path) {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur, ok = mm[part]
		if !ok {
			return false
		}
	}
	b, _ := cur.(bool)
	return b
}

func nestedArray(t *testing.T, m map[string]any, path string) []any {
	t.Helper()
	cur := any(m)
	for _, part := range splitPath(path) {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = mm[part]
		if !ok {
			return nil
		}
	}
	a, _ := cur.([]any)
	return a
}

func splitPath(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '.' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	out = append(out, p[start:])
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// 保证 context 包被使用（辅助函数预留）
var _ = context.Background
