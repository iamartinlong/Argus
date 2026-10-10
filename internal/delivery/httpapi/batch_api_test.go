package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestBatchRenewAndDeploy(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 造两个证书（fake provider 签发成功）
	importCert(t, s, adminToken, "batch1.example.com")
	importCert(t, s, adminToken, "batch2.example.com")
	var certs []map[string]any
	rec := s.do(http.MethodGet, "/api/v1/certificates", nil, adminToken)
	decodeData(t, rec, &certs)
	if len(certs) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(certs))
	}
	ids := []string{certs[0]["id"].(string), certs[1]["id"].(string)}

	// 建 local 目标
	rec = s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "batch-target", "type": "local",
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target: %d %s", rec.Code, rec.Body.String())
	}
	var target struct{ ID string `json:"id"` }
	decodeData(t, rec, &target)

	// 批量续期（fake provider 不支持续期 → 全部计入 failed，验证失败收集）
	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-renew", map[string]any{"ids": ids}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch renew: %d %s", rec.Code, rec.Body.String())
	}
	var renewResult struct {
		Success int `json:"success"`
		Failed  []struct {
			ID string `json:"id"`
		} `json:"failed"`
	}
	decodeData(t, rec, &renewResult)
	if renewResult.Success != 0 || len(renewResult.Failed) != 2 {
		t.Fatalf("unexpected renew result (expected 2 failures): %+v", renewResult)
	}

	// 证书绑定目标：通过 import API 重建（带 deploy_target_ids）
	for _, domain := range []string{"batch1.example.com", "batch2.example.com"} {
		rec = s.do(http.MethodPost, "/api/v1/certificates/import", map[string]any{
			"domains":          []string{domain},
			"certificate":      generateTestCert(t, domain),
			"private_key":      testPrivKeyPEM,
			"verification":     "dns01",
			"deploy_target_ids": []string{target.ID},
		}, adminToken)
		if rec.Code != http.StatusCreated {
			t.Fatalf("import with target: %d %s", rec.Code, rec.Body.String())
		}
	}
	var boundCerts []map[string]any
	rec = s.do(http.MethodGet, "/api/v1/certificates", nil, adminToken)
	decodeData(t, rec, &boundCerts)
	ids = []string{boundCerts[0]["id"].(string), boundCerts[1]["id"].(string)}

	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-deploy", map[string]any{
		"ids": ids, "target_id": target.ID,
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch deploy: %d %s", rec.Code, rec.Body.String())
	}
	var deployResult struct {
		Success int `json:"success"`
		Failed  []struct {
			ID string `json:"id"`
		} `json:"failed"`
	}
	decodeData(t, rec, &deployResult)
	if deployResult.Success != 2 || len(deployResult.Failed) != 0 {
		t.Fatalf("unexpected deploy result: %+v", deployResult)
	}

	// 校验：空 ids / 超限 / 目标不存在 / 不存在的证书
	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-renew", map[string]any{"ids": []string{}}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 empty ids, got %d", rec.Code)
	}
	over := make([]string, 21)
	for i := range over {
		over[i] = fmt.Sprintf("id-%d", i)
	}
	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-renew", map[string]any{"ids": over}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 over limit, got %d", rec.Code)
	}
	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-deploy", map[string]any{
		"ids": ids, "target_id": "nope",
	}, adminToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 unknown target, got %d", rec.Code)
	}
	// 不存在的证书 → 计入 failed
	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-renew", map[string]any{
		"ids": []string{"no-such-cert"},
	}, adminToken)
	var failResult struct {
		Success int `json:"success"`
		Failed  []struct {
			ID string `json:"id"`
		} `json:"failed"`
	}
	decodeData(t, rec, &failResult)
	if rec.Code != http.StatusOK || failResult.Success != 0 || len(failResult.Failed) != 1 {
		t.Fatalf("expected 1 failure, got %d %+v", rec.Code, failResult)
	}
}

// TestBatchDedup 重复 ID 只执行一次；空/空字符串 ID 过滤
func TestBatchDedup(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/certificates/batch-renew", map[string]any{
		"ids": []string{"dup-id", "dup-id", "", "x"},
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("dedup: %d %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Success int `json:"success"`
		Failed  []struct {
			ID string `json:"id"`
		} `json:"failed"`
	}
	decodeData(t, rec, &result)
	// 去重后 2 个有效 ID，均失败（fake provider）——不得出现重复 ID 失败条目
	if len(result.Failed) != 2 {
		t.Fatalf("expected 2 failures after dedup, got %+v", result)
	}
	if result.Failed[0].ID == result.Failed[1].ID {
		t.Fatalf("duplicate ID processed twice: %+v", result.Failed)
	}

	// 全部无效/空 → 400
	rec = s.do(http.MethodPost, "/api/v1/certificates/batch-renew", map[string]any{
		"ids": []string{"", ""},
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty after dedup, got %d", rec.Code)
	}
}

var _ = domain.RoleAdmin
