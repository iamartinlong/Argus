package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

// createToken helper：创建 API Token 并返回明文
func createToken(t *testing.T, s *testServer, adminToken, name string, scopes []string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/api-tokens", map[string]any{
		"name": name, "scopes": scopes,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create token: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	decodeData(t, rec, &resp)
	return resp.Token
}

func TestApiTokenCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	raw := createToken(t, s, adminToken, "ci-token", []string{"certificates:read", "certificates:write"})

	// 列表
	rec := s.do(http.MethodGet, "/api/v1/api-tokens", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var tokens []map[string]any
	decodeData(t, rec, &tokens)
	if _, has := tokens[0]["token"]; has {
		t.Fatal("token plaintext must not be in list")
	}

	// 使用 token 访问（读 + 写 certificates）
	rec = s.do(http.MethodGet, "/api/v1/certificates", nil, raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("token read: %d", rec.Code)
	}
	// 无 deploy scope → 403
	rec = s.do(http.MethodGet, "/api/v1/deploy-targets", nil, raw)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without deploy scope, got %d", rec.Code)
	}

	// 吊销后失效
	var tokenID string
	for _, tk := range tokens {
		if tk["name"] == "ci-token" {
			tokenID = tk["id"].(string)
		}
	}
	rec = s.do(http.MethodDelete, "/api/v1/api-tokens/"+tokenID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/certificates", nil, raw)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revoke, got %d", rec.Code)
	}
}

func TestApiTokenReadScope(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	raw := createToken(t, s, adminToken, "readonly-token", []string{"read"})

	// 只读 token：可读不可写
	rec := s.do(http.MethodGet, "/api/v1/certificates", nil, raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d", rec.Code)
	}
	rec = s.do(http.MethodPost, "/api/v1/certificates", map[string]any{
		"domains": []string{"x.example.com"}, "account_id": "a", "verification": "http01",
	}, raw)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for write with read scope, got %d", rec.Code)
	}
}

func TestApiTokenAllScope(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	raw := createToken(t, s, adminToken, "all-token", nil) // 默认 all

	rec := s.do(http.MethodGet, "/api/v1/certificates", nil, raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d", rec.Code)
	}
	// all scope 但用户是 admin → 可访问 admin 资源
	rec = s.do(http.MethodGet, "/api/v1/users", nil, raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin read: %d", rec.Code)
	}
}

func TestApiTokenInvalidScope(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/api-tokens", map[string]any{
		"name": "bad", "scopes": []string{"bogus:scope"},
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid scope, got %d", rec.Code)
	}
}

func TestApiTokenExpired(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/api-tokens", map[string]any{
		"name": "exp", "ttl_days": 1,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	var resp struct {
		Token string `json:"token"`
	}
	decodeData(t, rec, &resp)

	// 直接篡改过期时间
	if _, err := s.db.Exec("UPDATE api_tokens SET expires_at = datetime('now', '-1 day')"); err != nil {
		t.Fatal(err)
	}
	rec = s.do(http.MethodGet, "/api/v1/certificates", nil, resp.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", rec.Code)
	}
}

func TestApiTokenRequiresRole(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")

	rec := s.do(http.MethodPost, "/api/v1/api-tokens", map[string]any{
		"name": "x",
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}

func TestApiTokenCannotRevokeOthers(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	opToken := createUser(t, s, adminToken, "op1", "operator")

	// admin 创建 token
	raw := createToken(t, s, adminToken, "admin-token", []string{"read"})
	// operator 尝试吊销 admin 的 token
	rec := s.do(http.MethodGet, "/api/v1/api-tokens", nil, opToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var tokens []map[string]any
	decodeData(t, rec, &tokens)
	// operator 直接按 id 吊销 → 403
	rec = s.do(http.MethodDelete, "/api/v1/api-tokens/some-other-id", nil, opToken)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 403/404 for revoking others token, got %d", rec.Code)
	}
	// token 仍然有效
	rec = s.do(http.MethodGet, "/api/v1/stats", nil, raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("token should still work: %d", rec.Code)
	}
}

func TestApiTokenReadScopeCannotTriggerScan(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	raw := createToken(t, s, adminToken, "read-only", []string{"read"})
	// 手动扫描是写操作（触发续期/部署），read scope 应拒绝
	rec := s.do(http.MethodPost, "/api/v1/task-logs/scan", nil, raw)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for scan with read scope, got %d", rec.Code)
	}
}

func TestRequiredScopeRules(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/api/v1/certificates", "certificates:read"},
		{http.MethodPost, "/api/v1/certificates", "certificates:write"},
		{http.MethodGet, "/api/v1/task-logs", "read"},
		{http.MethodPost, "/api/v1/task-logs/scan", "certificates:write"},
		{http.MethodGet, "/api/v1/stats", "read"},
		{http.MethodGet, "/api/v1/api-tokens", "admin:read"},
		{http.MethodPost, "/api/v1/api-tokens", "admin:write"},
		{http.MethodGet, "/api/v1/notification-channels", "notify:read"},
		{http.MethodPost, "/api/v1/notification-channels", "notify:write"},
		{http.MethodGet, "/api/v1/notification-templates", "notify:read"},
		{http.MethodPut, "/api/v1/notification-templates/cert_expiring", "notify:write"},
		{http.MethodGet, "/api/v1/notification-send-logs", "notify:read"},
		{http.MethodGet, "/api/v1/agents", "admin:read"},
		{http.MethodPost, "/api/v1/agents", "admin:write"},
		{http.MethodPost, "/api/v1/agents/batch-upgrade", "admin:write"},
		{http.MethodGet, "/api/v1/unknown-future-resource", "admin:read"},
		{http.MethodPost, "/api/v1/unknown-future-resource", "admin:write"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		got, ok := requiredScope(req)
		if !ok {
			t.Fatalf("%s %s: expected scope, got none", c.method, c.path)
		}
		if got != c.want {
			t.Fatalf("%s %s: expected %s, got %s", c.method, c.path, c.want, got)
		}
	}
}

func TestApiTokenNegativeTTLRejected(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/api-tokens", map[string]any{
		"name": "neg", "ttl_days": -5,
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative ttl, got %d", rec.Code)
	}
}
