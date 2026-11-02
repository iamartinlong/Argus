package httpapi

import (
	"net/http"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

// TestDomainsCRUD 台账增删改查 + 富化行覆盖状态
func TestDomainsCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "Adminpass123456")

	// 匿名不可访问
	if rec := s.do(http.MethodGet, "/api/v1/domains", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: %d", rec.Code)
	}

	// 空列表
	rec := s.do(http.MethodGet, "/api/v1/domains", nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("list empty: %d %s", rec.Code, rec.Body.String())
	}

	// 创建（名称规范化）
	rec = s.do(http.MethodPost, "/api/v1/domains", map[string]any{
		"name": "  WWW.Example.COM. ", "note": "主站", "tags": []string{"prod"},
	}, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	decodeData(t, rec, &created)
	if created.Name != "www.example.com" {
		t.Fatalf("created name = %q, want www.example.com", created.Name)
	}

	// 重复 → 409
	rec = s.do(http.MethodPost, "/api/v1/domains", map[string]any{"name": "www.example.com"}, admin)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create: %d, want 409", rec.Code)
	}

	// 非法域名 → 400
	rec = s.do(http.MethodPost, "/api/v1/domains", map[string]any{"name": "bad; rm -rf /"}, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid domain: %d, want 400", rec.Code)
	}

	// 列表（富化行）：无证书 → missing
	rec = s.do(http.MethodGet, "/api/v1/domains", nil, admin)
	var items []struct {
		Name           string `json:"name"`
		Coverage       string `json:"coverage"`
		CertCount      int    `json:"cert_count"`
		DaysToExpire   *int   `json:"days_to_expire"`
		ExpectedStatus string `json:"expected_status"`
		CurrentCert    *struct {
			PrimaryDomain string `json:"primary_domain"`
		} `json:"current_cert"`
	}
	decodeData(t, rec, &items)
	if len(items) != 1 || items[0].Coverage != "missing" || items[0].CurrentCert != nil || items[0].DaysToExpire != nil {
		t.Fatalf("ledger item = %+v", items)
	}

	// 更新（改名 + 期望状态）
	rec = s.do(http.MethodPut, "/api/v1/domains/"+created.ID, map[string]any{
		"name": "new.example.com", "expected_status": "monitor_only", "tags": []string{"a", "a"},
	}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.do(http.MethodGet, "/api/v1/domains", nil, admin)
	decodeData(t, rec, &items)
	if items[0].Name != "new.example.com" || items[0].ExpectedStatus != "monitor_only" || items[0].Coverage != "monitor" {
		t.Fatalf("after update = %+v", items[0])
	}

	// 删除
	rec = s.do(http.MethodDelete, "/api/v1/domains/"+created.ID, nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.do(http.MethodGet, "/api/v1/domains", nil, admin)
	decodeData(t, rec, &items)
	if len(items) != 0 {
		t.Fatalf("after delete = %d items", len(items))
	}
}

// TestDomainsPermissions readonly 可读不可写
func TestDomainsPermissions(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "Adminpass123456")
	s.do(http.MethodPost, "/api/v1/domains", map[string]any{"name": "a.example.com"}, admin)

	s.seedUser("viewer", "Viewerpass123456", domain.RoleReadonly)
	viewer := s.login("viewer", "Viewerpass123456")

	if rec := s.do(http.MethodGet, "/api/v1/domains", nil, viewer); rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d", rec.Code)
	}
	if rec := s.do(http.MethodPost, "/api/v1/domains", map[string]any{"name": "b.example.com"}, viewer); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create: %d, want 403", rec.Code)
	}
}
