package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

// TestIssue422ErrorEnvelope 签发失败（fake provider）返回 422 + 错误信封（code=unprocessable, data.certificate）
func TestIssue422ErrorEnvelope(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 创建 fake acme 账户
	rec := s.do(http.MethodPost, "/api/v1/acme-accounts", map[string]any{
		"provider": "acme", "name": "env-acc",
		"directory_url": "https://acme.example.com/dir",
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: %d %s", rec.Code, rec.Body.String())
	}
	var acc struct{ ID string }
	decodeData(t, rec, &acc)

	// 签发：fake provider 必然失败 → 422 + 证书记录
	rec = s.do(http.MethodPost, "/api/v1/certificates", map[string]any{
		"domains":      []string{"env-422.example.com"},
		"account_id":   acc.ID,
		"verification": "dns01",
	}, adminToken)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (%s)", rec.Code, rec.Body.String())
	}
	// 错误信封：code=unprocessable + message 含失败原因 + data.certificate
	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
		Data    struct {
			Certificate struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"certificate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Code != "unprocessable" {
		t.Fatalf("expected code=unprocessable, got %q", env.Code)
	}
	if env.Message == "" || env.TraceID == "" {
		t.Fatalf("missing message/trace_id: %+v", env)
	}
	if env.Data.Certificate.ID == "" || env.Data.Certificate.Status != "failed" {
		t.Fatalf("unexpected certificate in data: %+v", env.Data.Certificate)
	}
}

var _ = domain.RoleAdmin
