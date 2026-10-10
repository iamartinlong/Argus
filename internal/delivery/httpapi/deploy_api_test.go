package httpapi

import (
	"net/http"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestDeployTargetsCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 创建 local 目标
	rec := s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "nginx-1", "type": "local",
		"config": map[string]any{
			"cert_path": "/etc/nginx/certs/example.pem", "key_path": "/etc/nginx/certs/example.key",
			"post_command": "nginx -s reload",
		},
		"enabled": true,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target: %d %s", rec.Code, rec.Body.String())
	}
	var target struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	decodeData(t, rec, &target)

	// 非法类型
	rec = s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "bad", "type": "ftp", "config": map[string]any{},
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid type, got %d", rec.Code)
	}

	// 列表
	rec = s.do(http.MethodGet, "/api/v1/deploy-targets", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var targets []map[string]any
	decodeData(t, rec, &targets)

	// 查看单条（含解密后的 config）
	rec = s.do(http.MethodGet, "/api/v1/deploy-targets/"+target.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Name   string         `json:"name"`
		Config map[string]any `json:"config"`
	}
	decodeData(t, rec, &got)
	if got.Name != "nginx-1" {
		t.Fatalf("get name: %q", got.Name)
	}
	if got.Config["cert_path"] != "/etc/nginx/certs/example.pem" {
		t.Fatalf("get config: %+v", got.Config)
	}

	// 更新（改名 + 修改路径）
	rec = s.do(http.MethodPut, "/api/v1/deploy-targets/"+target.ID, map[string]any{
		"name": "nginx-1-renamed", "type": "local",
		"config": map[string]any{
			"cert_path": "/etc/nginx/certs/example2.pem", "key_path": "/etc/nginx/certs/example2.key",
		},
		"enabled": true,
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	rec = s.do(http.MethodGet, "/api/v1/deploy-targets/"+target.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after update: %d", rec.Code)
	}
	var got2 struct {
		Name   string         `json:"name"`
		Config map[string]any `json:"config"`
	}
	decodeData(t, rec, &got2)
	if got2.Name != "nginx-1-renamed" || got2.Config["cert_path"] != "/etc/nginx/certs/example2.pem" {
		t.Fatalf("updated target: %+v %+v", got2.Name, got2.Config)
	}

	// 删除
	rec = s.do(http.MethodDelete, "/api/v1/deploy-targets/"+target.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeployTargetsRequireRole(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")

	rec := s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "x", "type": "local", "config": map[string]any{"cert_path": "/tmp/x.pem"},
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
	// 只读可查看列表
	rec = s.do(http.MethodGet, "/api/v1/deploy-targets", nil, viewerToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("readonly list: %d", rec.Code)
	}
	// 只读不可查看单条（config 含敏感凭据）
	rec = s.do(http.MethodGet, "/api/v1/deploy-targets/"+targetIDOf(t, s, adminToken), nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly single get, got %d", rec.Code)
	}
}

// targetIDOf 创建并返回一个目标 ID（供权限测试）
func targetIDOf(t *testing.T, s *testServer, token string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "perm", "type": "local", "config": map[string]any{"cert_path": "/tmp/x.pem"},
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed target: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &out)
	return out.ID
}

func TestSSHIISDeployTargetCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 创建 SSH 通道 + IIS 场景目标
	rec := s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "iis-01", "type": "ssh",
		"config": map[string]any{
			"scene": "iis",
			"host":  "10.0.0.6", "user": "Administrator",
			"pfx_path": `C:\certs\example.pfx`,
			"bindings": []int{443},
			"site_name": "Default Web Site",
		},
		"enabled": true,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create iis target: %d %s", rec.Code, rec.Body.String())
	}
	var target struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	decodeData(t, rec, &target)

	// 创建本身不校验配置内容（Test/Deploy 时才校验）
	rec = s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "iis-bad", "type": "ssh",
		"config": map[string]any{"scene": "iis", "host": "10.0.0.6", "user": "Administrator", "pfx_path": "C:/x.pfx"},
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create iis (config not validated at create): %d", rec.Code)
	}
	_ = target.ID
}

func TestCertDeployRecordsAndBind(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	certID := importCert(t, s, adminToken, "deploy-api.example.com")

	// 未绑定目标时部署返回 400
	rec := s.do(http.MethodPost, "/api/v1/certificates/"+certID+"/deploy", nil, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without targets, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 绑定一个不存在的目标 → 部署不报错但记录失败或跳过
	rec = s.do(http.MethodPost, "/api/v1/certificates/"+certID+"/deploy", map[string]any{
		"target_ids": []string{"nonexistent"},
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy: %d %s", rec.Code, rec.Body.String())
	}

	// 部署记录接口可用
	rec = s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/deploy-records", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy-records: %d %s", rec.Code, rec.Body.String())
	}
	var records []any
	decodeData(t, rec, &records)
}
