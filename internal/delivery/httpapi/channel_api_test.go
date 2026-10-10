package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
	appcrypto "github.com/iamartinlong/Argus/internal/infrastructure/crypto"
)

func TestChannelCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "adminpass123456")

	// 创建
	rec := s.do(http.MethodPost, "/api/v1/notification-channels", map[string]any{
		"name": "测试渠道", "type": "webhook", "enabled": true,
		"config": map[string]any{"url": "http://example.com/hook", "secret": "s3cr3t"},
	}, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
	}
	decodeData(t, rec, &created)

	// 列表（响应不含敏感配置）
	rec = s.do(http.MethodGet, "/api/v1/notification-channels", nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	if body := rec.Body.String(); len(body) == 0 || containsStr(body, "s3cr3t") || containsStr(body, "example.com/hook") {
		t.Fatalf("list leaks config or empty: %s", body)
	}

	// 非法配置拒绝
	rec = s.do(http.MethodPost, "/api/v1/notification-channels", map[string]any{
		"name": "bad", "type": "webhook", "config": map[string]any{"url": ""},
	}, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid config, got %d", rec.Code)
	}

	// 更新（不传 enabled 保持原状态）
	rec = s.do(http.MethodPut, "/api/v1/notification-channels/"+created.ID, map[string]any{
		"name": "改名", "type": "webhook",
	}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	decodeData(t, rec, &updated)
	if updated.Enabled != true || updated.Name != "改名" {
		t.Fatalf("unexpected updated: %+v", updated)
	}

	// 停用（显式 enabled=false）
	rec = s.do(http.MethodPut, "/api/v1/notification-channels/"+created.ID, map[string]any{
		"name": "改名", "type": "webhook", "enabled": false,
	}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d", rec.Code)
	}
	decodeData(t, rec, &updated)
	if updated.Enabled {
		t.Fatal("expected disabled")
	}

	// 规则引用 + 删除后引用清理
	rec = s.do(http.MethodPost, "/api/v1/notification-rules", map[string]any{
		"name": "规则A", "event": "cert_expiring", "channel_ids": []string{created.ID},
		"days_before": []int{30, 7}, "enabled": true,
	}, admin)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.do(http.MethodDelete, "/api/v1/notification-channels/"+created.ID, nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete channel: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/notification-rules", nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("list rules: %d", rec.Code)
	}
	var rules []map[string]any
	decodeData(t, rec, &rules)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	ids, _ := rules[0]["channel_ids"].([]any)
	if len(ids) != 0 {
		t.Fatalf("expected channel ref cleaned, got %v", ids)
	}

	// 404：不存在的渠道
	rec = s.do(http.MethodPut, "/api/v1/notification-channels/nope", map[string]any{
		"name": "x", "type": "webhook",
	}, admin)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestChannelPermissions(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	s.seedUser("viewer", "Viewerpassword123", domain.RoleReadonly)
	viewer := s.login("viewer", "Viewerpassword123")

	// 普通用户可查看列表
	rec := s.do(http.MethodGet, "/api/v1/notification-channels", nil, viewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d", rec.Code)
	}
	// 普通用户不可创建
	rec = s.do(http.MethodPost, "/api/v1/notification-channels", map[string]any{
		"name": "x", "type": "webhook", "config": map[string]any{"url": "http://x"},
	}, viewer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestChannelEditUpdatesConfig(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "adminpass123456")

	// 创建渠道（url=A）
	rec := s.do(http.MethodPost, "/api/v1/notification-channels", map[string]any{
		"name": "c", "type": "webhook", "enabled": true,
		"config": map[string]any{"url": "https://a.example.com/hook", "secret": "s1"},
	}, admin)
	var created struct{ ID string `json:"id"` }
	decodeData(t, rec, &created)

	// 编辑：仅改 url（B）
	rec = s.do(http.MethodPut, "/api/v1/notification-channels/"+created.ID, map[string]any{
		"name": "c", "type": "webhook", "enabled": true,
		"config": map[string]any{"url": "https://b.example.com/hook", "secret": "s1"},
	}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	// 直接查存储验证 config 已更新为 B
	var configEnc string
	if err := s.db.QueryRow("SELECT config_enc FROM notification_channels WHERE id = ?", created.ID).Scan(&configEnc); err != nil {
		t.Fatal(err)
	}
	aes, _ := appcrypto.NewAESGCM("test-master-key-test-master-key-1234")
	plain, err := aes.DecryptString(configEnc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !strings.Contains(plain, "https://b.example.com/hook") || strings.Contains(plain, "a.example.com") {
		t.Fatalf("config not updated: %s", plain)
	}

	// 编辑：不传 config → 保持原配置
	rec = s.do(http.MethodPut, "/api/v1/notification-channels/"+created.ID, map[string]any{
		"name": "c2", "type": "webhook", "enabled": true,
	}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("update2: %d", rec.Code)
	}
	if err := s.db.QueryRow("SELECT config_enc FROM notification_channels WHERE id = ?", created.ID).Scan(&configEnc); err != nil {
		t.Fatal(err)
	}
	plain, _ = aes.DecryptString(configEnc)
	if !strings.Contains(plain, "https://b.example.com/hook") {
		t.Fatalf("config lost after update without config: %s", plain)
	}
}

func TestChannelDetailReturnsMaskedConfig(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "adminpass123456")

	// 创建带敏感字段的渠道
	rec := s.do(http.MethodPost, "/api/v1/notification-channels", map[string]any{
		"name": "detail", "type": "webhook", "enabled": true,
		"config": map[string]any{"url": "https://x.example.com/hook", "secret": "super-secret"},
	}, admin)
	var created struct{ ID string `json:"id"` }
	decodeData(t, rec, &created)

	// 详情：config 回传，url 保留、secret 置空
	rec = s.do(http.MethodGet, "/api/v1/notification-channels/"+created.ID, nil, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Config map[string]any `json:"config"`
	}
	decodeData(t, rec, &detail)
	if detail.Config["url"] != "https://x.example.com/hook" {
		t.Fatalf("url not returned: %v", detail.Config)
	}
	if detail.Config["secret"] != "" {
		t.Fatalf("secret leaked: %v", detail.Config["secret"])
	}
	body := rec.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Fatalf("sensitive config leaked in response: %s", body)
	}

	// 不存在的渠道 → 404
	rec = s.do(http.MethodGet, "/api/v1/notification-channels/nope", nil, admin)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// readonly 不可读详情（配置敏感）
	viewerToken := createUser(t, s, admin, "vdetail", "readonly")
	rec = s.do(http.MethodGet, "/api/v1/notification-channels/"+created.ID, nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}

func TestRuleRejectsInvalidEvent(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	admin := s.login("admin", "adminpass123456")
	rec := s.do(http.MethodPost, "/api/v1/notification-rules", map[string]any{
		"name": "bad", "event": "not_a_real_event", "channel_ids": []string{"whatever"},
	}, admin)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid event, got %d", rec.Code)
	}
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

var _ = domain.RoleAdmin
