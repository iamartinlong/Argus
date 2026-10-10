package httpapi

import (
	"strings"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

func generateTestCert(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

const testPrivKeyPEM = "-----BEGIN EC PRIVATE KEY-----\nMHQCAQEEIFv1Up3Hk4QbWq5x8vM3n2zT0uQpJ9tXyQ7kYwqBm5Cz\n-----END EC PRIVATE KEY-----\n"

func importCert(t *testing.T, s *testServer, token, domain string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/certificates/import", map[string]any{
		"domains":      []string{domain},
		"certificate":  generateTestCert(t, domain),
		"private_key":  testPrivKeyPEM,
		"verification": "dns01",
	}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import %s: %d %s", domain, rec.Code, rec.Body.String())
	}
	var cert struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &cert)
	return cert.ID
}

func createUser(t *testing.T, s *testServer, adminToken, username, role string) string {
	t.Helper()
	password := username + "Pass123456"
	rec := s.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": username, "password": password, "role": role,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user %s: %d %s", username, rec.Code, rec.Body.String())
	}
	return s.login(username, password)
}

func TestStatsRequiresAuth(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/api/v1/stats", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for anonymous stats, got %d", rec.Code)
	}
}

func TestReadonlyCannotWriteCertificates(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")

	// 只读可查列表
	rec := s.do(http.MethodGet, "/api/v1/certificates", nil, viewerToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("readonly list: %d", rec.Code)
	}
	// 只读不可签发
	rec = s.do(http.MethodPost, "/api/v1/certificates", map[string]any{
		"domains": []string{"example.com"}, "account_id": "x", "verification": "http01",
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly issue, got %d", rec.Code)
	}
	// 只读不可删除
	rec = s.do(http.MethodDelete, "/api/v1/certificates/whatever", nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly delete, got %d", rec.Code)
	}
	// 只读不可建账户
	rec = s.do(http.MethodPost, "/api/v1/acme-accounts", map[string]any{
		"provider": "acme", "name": "x",
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly create account, got %d", rec.Code)
	}
	// 只读不可测试账户
	rec = s.do(http.MethodPost, "/api/v1/acme-accounts/x/test", nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly test account, got %d", rec.Code)
	}
}

func TestOperatorCanWriteCertificates(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	opToken := createUser(t, s, adminToken, "op", "operator")

	rec := s.do(http.MethodPost, "/api/v1/acme-accounts", map[string]any{
		"provider": "acme", "name": "op-account", "directory_url": "https://acme-v02.api.letsencrypt.org/directory",
	}, opToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("operator create account: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMustChangePasswordBlocksAPI(t *testing.T) {
	s := newTestServer(t)
	u := s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	u.MustChangePassword = true
	if err := s.userStore.Update(s.t.Context(), u); err != nil {
		t.Fatal(err)
	}
	rec := s.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "Adminpass123456",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d", rec.Code)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	decodeData(t, rec, &resp)
	// 改密前调业务 API 被拦截
	rec = s.do(http.MethodGet, "/api/v1/certificates", nil, resp.AccessToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 before password change, got %d", rec.Code)
	}
	// 改密接口可访问
	rec = s.do(http.MethodPut, "/api/v1/users/me/password", map[string]any{
		"old_password": "Adminpass123456", "new_password": "NewAdminpass456",
	}, resp.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}
	// 改密后可访问
	rec = s.do(http.MethodGet, "/api/v1/certificates", nil, resp.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after password change, got %d", rec.Code)
	}
}

func TestReadonlyCannotDownloadKey(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")
	opToken := createUser(t, s, adminToken, "op", "operator")

	certID := importCert(t, s, adminToken, "dl.example.com")

	// 只读可下载证书本体
	rec := s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?kind=crt", nil, viewerToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("readonly download crt: %d", rec.Code)
	}
	// 只读不可下载私钥
	rec = s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?kind=key", nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly key download, got %d", rec.Code)
	}
	// operator 可下载私钥
	rec = s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?kind=key", nil, opToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator download key: %d", rec.Code)
	}

	// 格式 zip：readonly 403（nginx/iis 含私钥）；用真实密钥对保证 iis 合成 PFX 成功
	certID = importRealCert(t, s, adminToken, "zip.example.com")
	for _, format := range []string{"nginx", "iis"} {
		rec = s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?format="+format, nil, viewerToken)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("readonly download %s: expected 403, got %d", format, rec.Code)
		}
		rec = s.do(http.MethodGet, "/api/v1/certificates/"+certID+"/download?format="+format, nil, opToken)
		if rec.Code != http.StatusOK {
			t.Fatalf("operator download %s: expected 200, got %d", format, rec.Code)
		}
	}
}

func TestAccountDeleteBlockedWhenInUse(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/acme-accounts", map[string]any{
		"provider": "acme", "name": "acc", "directory_url": "https://acme-v02.api.letsencrypt.org/directory",
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: %d", rec.Code)
	}
	var acc struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &acc)
	// 绑定证书到账户 → 删除应被拒绝（409）
	certID := importCert(t, s, adminToken, "inuse.example.com")
	if _, err := s.db.Exec("UPDATE certificates SET account_id = ? WHERE id = ?", acc.ID, certID); err != nil {
		t.Fatal(err)
	}
	rec = s.do(http.MethodDelete, "/api/v1/acme-accounts/"+acc.ID, nil, adminToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for account in use, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAuditLogsForCertOperations(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	importCert(t, s, adminToken, "audit.example.com")

	rec := s.do(http.MethodGet, "/api/v1/audit-logs?limit=200", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit logs: %d", rec.Code)
	}
	// 分页信封：解 items
	var page struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	decodeData(t, rec, &page)
	logs := page.Items
	var found bool
	for _, l := range logs {
		if l.Action == "certificate.import" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected certificate.import audit entry")
	}

	// 筛选：action 前缀
	rec = s.do(http.MethodGet, "/api/v1/audit-logs?action=certificate.", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter by action: %d", rec.Code)
	}
	var filteredPage struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	decodeData(t, rec, &filteredPage)
	filtered := filteredPage.Items
	if len(filtered) == 0 {
		t.Fatal("expected filtered audit entries")
	}
	for _, l := range filtered {
		if !strings.HasPrefix(l.Action, "certificate.") {
			t.Fatalf("unexpected action in filtered result: %s", l.Action)
		}
	}

	// 筛选：关键字（导入的域名出现在 resource）
	rec = s.do(http.MethodGet, "/api/v1/audit-logs?keyword=audit.example.com", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter by keyword: %d", rec.Code)
	}
	var kwPage struct {
		Items []struct {
			Resource string `json:"resource"`
		} `json:"items"`
	}
	decodeData(t, rec, &kwPage)
	kw := kwPage.Items
	if len(kw) == 0 {
		t.Fatal("expected keyword matched audit entries")
	}

	// 筛选：无结果（分页信封）
	rec = s.do(http.MethodGet, "/api/v1/audit-logs?username=no_such_user", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter by username: %d", rec.Code)
	}
	var emptyPage struct {
		Items []struct{} `json:"items"`
	}
	decodeData(t, rec, &emptyPage)
	if len(emptyPage.Items) != 0 {
		t.Fatalf("expected empty result, got %d", len(emptyPage.Items))
	}

	// LIKE 通配符转义：% 应只匹配字面字符，而非任意串（URL 编码 %25）
	rec = s.do(http.MethodGet, "/api/v1/audit-logs?keyword=%25", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("keyword escape: %d", rec.Code)
	}
	var literalPage struct {
		Items []struct {
			Resource string `json:"resource"`
			Detail   string `json:"detail"`
		} `json:"items"`
	}
	decodeData(t, rec, &literalPage)
	literal := literalPage.Items
	for _, l := range literal {
		if !strings.Contains(l.Resource, "%") && !strings.Contains(l.Detail, "%") {
			t.Fatalf("LIKE escaped: matched record without literal %%: %+v", l)
		}
	}
	// 对照：全部日志条数应多于字面 % 匹配数（证明 % 未通配全部）
	rec = s.do(http.MethodGet, "/api/v1/audit-logs?limit=200", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("all logs: %d", rec.Code)
	}
	var allPage struct {
		Items []struct{} `json:"items"`
	}
	decodeData(t, rec, &allPage)
	all := allPage.Items
	if len(all) <= len(literal) {
		t.Fatalf("expected wildcard NOT to match all logs: all=%d literal=%d", len(all), len(literal))
	}
}

func TestCloudAccountCredentials(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 云厂商账户：必须提供凭据
	rec := s.do(http.MethodPost, "/api/v1/acme-accounts", map[string]any{
		"provider": "aliyun", "name": "aliyun-1",
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without credentials, got %d", rec.Code)
	}
	// 提供凭据创建成功
	rec = s.do(http.MethodPost, "/api/v1/acme-accounts", map[string]any{
		"provider": "tencent", "name": "tencent-1",
		"credentials": map[string]string{
			"secret_id": "test-id", "secret_key": "test-key",
		},
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create cloud account: %d %s", rec.Code, rec.Body.String())
	}
	// 列表不含凭据内容
	rec = s.do(http.MethodGet, "/api/v1/acme-accounts", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var accounts []map[string]any
	decodeData(t, rec, &accounts)
	if _, has := accounts[0]["credentials"]; has {
		t.Fatal("credentials must not be exposed in list")
	}
}
