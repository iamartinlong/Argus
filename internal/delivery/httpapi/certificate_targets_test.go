package httpapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

// TestUpdateCertificateTargets 签发后可通过 PUT /certificates/{id}/targets 更新部署目标绑定
func TestUpdateCertificateTargets(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "adminpass123456")

	// 创建部署目标
	rec := s.do(http.MethodPost, "/api/v1/deploy-targets", map[string]any{
		"name": "tgt-1", "type": "local",
		"config": map[string]any{
			"cert_path": "C:\\certs\\example.pem", "key_path": "C:\\certs\\example.key",
		},
		"enabled": true,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target: %d %s", rec.Code, rec.Body.String())
	}
	var tgt struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &tgt)
	if tgt.ID == "" {
		t.Fatal("empty target id")
	}

	// 导入证书（未绑定部署目标）
	certID := importCert(t, s, adminToken, "targets.example.com")

	// 更新证书绑定该部署目标
	rec = s.do(http.MethodPut, "/api/v1/certificates/"+certID+"/targets", map[string]any{
		"deploy_target_ids": []string{tgt.ID},
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("update targets: %d %s", rec.Code, rec.Body.String())
	}
	var cert struct {
		DeployTargetIDs []string `json:"deploy_target_ids"`
	}
	decodeData(t, rec, &cert)
	if len(cert.DeployTargetIDs) != 1 || cert.DeployTargetIDs[0] != tgt.ID {
		t.Fatalf("expected bound target, got %+v", cert.DeployTargetIDs)
	}

	// 再次查询确认持久化
	rec = s.do(http.MethodGet, "/api/v1/certificates/"+certID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cert: %d", rec.Code)
	}
	decodeData(t, rec, &cert)
	if len(cert.DeployTargetIDs) != 1 || cert.DeployTargetIDs[0] != tgt.ID {
		t.Fatalf("persisted target mismatch: %+v", cert.DeployTargetIDs)
	}

	// 清空绑定
	rec = s.do(http.MethodPut, "/api/v1/certificates/"+certID+"/targets", map[string]any{
		"deploy_target_ids": []string{},
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear targets: %d %s", rec.Code, rec.Body.String())
	}
	decodeData(t, rec, &cert)
	if len(cert.DeployTargetIDs) != 0 {
		t.Fatalf("expected no targets, got %+v", cert.DeployTargetIDs)
	}
}

// TestUpdateCertificateTargetsRequiresRole 非 admin/operator 不可更新部署目标绑定
func TestUpdateCertificateTargetsRequiresRole(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "adminpass123456")
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")

	certID := importCert(t, s, adminToken, "ro-targets.example.com")

	rec := s.do(http.MethodPut, "/api/v1/certificates/"+certID+"/targets", map[string]any{
		"deploy_target_ids": []string{},
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}

	// 未认证
	rec = s.do(http.MethodPut, "/api/v1/certificates/"+certID+"/targets", map[string]any{
		"deploy_target_ids": []string{},
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %d", rec.Code)
	}
}

// TestImportPFXAPI 通过 API 导入 PFX：多域名从 SAN 自动提取、provider=import
func TestImportPFXAPI(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "adminpass123456")

	// 生成证书 + 私钥，合成 PFX
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "pfx-api.example.com"},
		DNSNames:     []string{"pfx-api.example.com", "www.pfx-api.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	pfxData, err := deploy.BuildPFX(certPEM, keyPEM, "secret123")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.do(http.MethodPost, "/api/v1/certificates/import", map[string]any{
		"pfx":          base64.StdEncoding.EncodeToString(pfxData),
		"pfx_password": "secret123",
		"verification": "dns01",
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import pfx: %d %s", rec.Code, rec.Body.String())
	}
	var cert struct {
		ID       string   `json:"id"`
		Provider string   `json:"provider"`
		Domains  []string `json:"domains"`
	}
	decodeData(t, rec, &cert)
	if cert.Provider != "import" {
		t.Fatalf("expected provider import, got %s", cert.Provider)
	}
	if len(cert.Domains) != 2 {
		t.Fatalf("expected 2 domains from SAN, got %+v", cert.Domains)
	}
}

// TestImportPFXAPIRejectsMismatchedKey 私钥与证书不匹配的 PFX 经 API 导入应返回 400 及清晰错误
func TestImportPFXAPIRejectsMismatchedKey(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "adminpass123456")

	mkCert := func(key *ecdsa.PrivateKey, cn string) string {
		t.Helper()
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
	mkKeyPEM := func(key *ecdsa.PrivateKey) string {
		t.Helper()
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	}

	keyA, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pfxData, err := deploy.BuildPFX(mkCert(keyA, "mismatch.example.com"), mkKeyPEM(keyB), "secret123")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.do(http.MethodPost, "/api/v1/certificates/import", map[string]any{
		"pfx":          base64.StdEncoding.EncodeToString(pfxData),
		"pfx_password": "secret123",
		"verification": "dns01",
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched key, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不匹配") {
		t.Fatalf("expected mismatch message, got %s", rec.Body.String())
	}
}
