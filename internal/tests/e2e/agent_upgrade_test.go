//go:build integration

package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/testutil"
)

// TestAgentUpgradeE2E 自升级闭环：上传 -> 下发 -> Agent 查询 -> 下载 -> SHA256 校验 -> 文件替换模拟
func TestAgentUpgradeE2E(t *testing.T) {
	s := testutil.NewLiveTestServer(t)
	s.SeedUser(t, "admin", "Admin@2026Argus!", domain.RoleAdmin)

	var login loginData
	doRequest(t, "POST", s.BaseURL+"/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "Admin@2026Argus!",
	}, &login)
	adminToken := login.AccessToken

	// 创建 agent
	var ag agentCreateData
	doRequest(t, "POST", s.BaseURL+"/api/v1/agents", adminToken, map[string]any{"name": "upgrade-e2e"}, &ag)

	// 上传二进制 v2
	originalContent := []byte("fake-agent-binary-v2-content-" + testutil.RandSuffix())
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("version", "2.0.0")
	fw, _ := mw.CreateFormFile("file", "argus-agent")
	_, _ = fw.Write(originalContent)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/binaries", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, req)
	t.Logf("upload rec: code=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var upload struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	// Try envelope first, fallback to direct
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err == nil && env.Data != nil && len(env.Data) > 2 {
		if err := json.Unmarshal(env.Data, &upload); err != nil {
			t.Fatalf("decode upload data: %v body=%s", err, rec.Body.String())
		}
	} else {
		if err := json.Unmarshal(rec.Body.Bytes(), &upload); err != nil {
			t.Fatalf("decode upload direct: %v body=%s", err, rec.Body.String())
		}
	}
	expectedSHA := sha256.Sum256(originalContent)
	if upload.SHA256 != hex.EncodeToString(expectedSHA[:]) {
		t.Fatalf("upload sha mismatch: got %s want %s", upload.SHA256, hex.EncodeToString(expectedSHA[:]))
	}

	// 下发升级指令
	doRequest(t, "POST", s.BaseURL+"/api/v1/agents/"+ag.ID+"/upgrade", adminToken, map[string]any{"version": "2.0.0"}, nil)

	// Agent 查询升级指令（含 SHA256 固定值）- agent 接口为 raw JSON，非信封
	var up struct {
		Upgrade *struct {
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"upgrade"`
	}
	{
		recUp := doRawRequest(t, s, "GET", "/api/v1/agent/upgrade", ag.Token)
		if recUp.Code != http.StatusOK {
			t.Fatalf("query upgrade: %d %s", recUp.Code, recUp.Body.String())
		}
		if err := json.Unmarshal(recUp.Body.Bytes(), &up); err != nil {
			t.Fatalf("decode upgrade: %v body=%s", err, recUp.Body.String())
		}
	}
	if up.Upgrade == nil || up.Upgrade.Version != "2.0.0" || up.Upgrade.SHA256 != upload.SHA256 {
		t.Fatalf("query upgrade failed: %+v vs %s", up, upload.SHA256)
	}

	// Agent 下载二进制（仅允许下发给自己的版本）
	var downloaded []byte
	// Use doRequest helper that expects JSON envelope — download returns raw bytes, so use raw http
	downloadReq, _ := http.NewRequest("GET", s.BaseURL+"/api/v1/agent/binaries/2.0.0/download", nil)
	downloadReq.Header.Set("X-Agent-Token", ag.Token)
	// LiveTestServer's agent auth uses X-Agent-Token? Check agent_api_test: uses ag.Token via do() helper which sets Authorization Bearer for admin but for agent uses ag.Token via s.do with token param
	// For direct HTTP, we need to use the same helper: s.do with agent token
	// So use s.do helper path that sets agent token header
	rec2 := doRawRequest(t, s, "GET", "/api/v1/agent/binaries/2.0.0/download", ag.Token)
	downloaded = rec2.Body.Bytes()
	if string(downloaded) != string(originalContent) {
		t.Fatalf("download content mismatch: got %q want %q", downloaded, originalContent)
	}
	// SHA256 校验（模拟 agent 的 performUpgrade 校验）
	sum := sha256.Sum256(downloaded)
	if hex.EncodeToString(sum[:]) != up.Upgrade.SHA256 {
		t.Fatal("SHA256 verification failed - tampered")
	}

	// 未下发版本拒绝
	rec3 := doRawRequest(t, s, "GET", "/api/v1/agent/binaries/1.0.0/download", ag.Token)
	if rec3.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-target version, got %d", rec3.Code)
	}

	t.Logf("agent upgrade e2e passed: version=%s sha256=%s", up.Upgrade.Version, up.Upgrade.SHA256)
}

// doRawRequest 直接返回 recorder，适用于下载接口（非 JSON 信封）
func doRawRequest(t *testing.T, s *testutil.LiveTestServer, method, path, agentToken string) *httptest.ResponseRecorder {
	t.Helper()
	// 复用 testutil 的 do 逻辑：直接通过 handler ServeHTTP
	req := httptest.NewRequest(method, path, nil)
	if agentToken != "" {
		// Agent 接口使用 X-Agent-Token 或 Authorization Bearer? 检查 agent_api_test: s.do with token param sets Authorization Bearer for admin, but for agent it may set X-Agent-Token
		// 查看 agent_handler: AgentRoutes uses token from header "X-Agent-Token" or "Authorization: Bearer ag_..."
		// 为了兼容，设置两者
		req.Header.Set("X-Agent-Token", agentToken)
		req.Header.Set("Authorization", "Bearer "+agentToken)
	}
	rec := httptest.NewRecorder()
	s.Handler.ServeHTTP(rec, req)
	return rec
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Code != 0 {
		t.Fatalf("code !=0: %d body=%s", env.Code, rec.Body.String())
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("decode data: %v", err)
	}
}
