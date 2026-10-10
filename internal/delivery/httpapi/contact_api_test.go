package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

type contactResp struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func TestContactsCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "Adminpass123456")

	// 列表初始为空
	rec := s.do(http.MethodGet, "/api/v1/settings/contacts", nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}
	var list []contactResp
	decodeData(t, rec, &list)
	if len(list) != 0 {
		t.Fatalf("expected empty contacts, got %d", len(list))
	}

	// 创建
	rec = s.do(http.MethodPost, "/api/v1/settings/contacts", map[string]string{
		"name": "张三", "email": "zhangsan@example.com", "phone": "13800138000",
	}, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body.String())
	}
	var created contactResp
	decodeData(t, rec, &created)
	if created.ID == "" || created.Name != "张三" {
		t.Fatalf("create decode: %+v", created)
	}

	// 必填校验：缺 phone → 400 invalid_argument
	rec = s.do(http.MethodPost, "/api/v1/settings/contacts", map[string]string{
		"name": "李四", "email": "lisi@example.com", "phone": "",
	}, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errEnv struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errEnv); err != nil {
		t.Fatal(err)
	}
	if errEnv.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument, got %q", errEnv.Code)
	}

	// 更新
	rec = s.do(http.MethodPut, "/api/v1/settings/contacts/"+created.ID, map[string]string{
		"name": "张三丰", "email": "zhangsanfeng@example.com", "phone": "13900139000",
	}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", rec.Code, rec.Body.String())
	}
	var updated contactResp
	decodeData(t, rec, &updated)
	if updated.Name != "张三丰" || updated.Phone != "13900139000" {
		t.Fatalf("update decode: %+v", updated)
	}

	// 删除
	rec = s.do(http.MethodDelete, "/api/v1/settings/contacts/"+created.ID, nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", rec.Code, rec.Body.String())
	}

	// 列表回到空
	rec = s.do(http.MethodGet, "/api/v1/settings/contacts", nil, admin)
	decodeData(t, rec, &list)
	if len(list) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(list))
	}
}

func TestContactsPermission(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("viewer", "Viewerpass123456", domain.RoleReadonly)
	viewer := s.login("viewer", "Viewerpass123456")

	// viewer 可读
	rec := s.do(http.MethodGet, "/api/v1/settings/contacts", nil, viewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer list status %d: %s", rec.Code, rec.Body.String())
	}

	// viewer 不可写
	rec = s.do(http.MethodPost, "/api/v1/settings/contacts", map[string]string{
		"name": "x", "email": "x@example.com", "phone": "13800138000",
	}, viewer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create status %d, want 403", rec.Code)
	}
}