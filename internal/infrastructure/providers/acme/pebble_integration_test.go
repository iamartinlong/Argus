//go:build integration

package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

// testCryptor 明文中转（集成测试用）
type testCryptor struct{}

func (testCryptor) EncryptString(s string) (string, error) { return s, nil }
func (testCryptor) DecryptString(enc string) (string, error) {
	return enc, nil
}

func startPebble(t *testing.T) (caFile string) {
	t.Helper()
	pebbleBin := os.Getenv("PEBBLE_BIN")
	if pebbleBin == "" {
		t.Skip("PEBBLE_BIN 未设置，跳过 Pebble 集成测试")
	}
	testdata := os.Getenv("PEBBLE_TESTDATA")
	if testdata == "" {
		testdata = filepath.Join("..", "..", "..", "..", "build", "testdata")
	}
	configFile := filepath.Join(testdata, "pebble-config.json")
	caFile = filepath.Join(testdata, "certs", "pebble.minica.pem")

	// pebble 配置使用相对路径 test/certs/...，需在 testdata 下构造对应结构
	ensureDir(t, filepath.Join(testdata, "test", "certs", "localhost"))
	for _, f := range []string{"cert.pem", "key.pem"} {
		src := filepath.Join(testdata, "certs", "localhost", f)
		dst := filepath.Join(testdata, "test", "certs", "localhost", f)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatalf("读取测试证书 %s: %v", src, err)
			}
			if err := os.WriteFile(dst, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(testdata, "test", "certs", "pebble.minica.pem")); os.IsNotExist(err) {
		data, err := os.ReadFile(caFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(testdata, "test", "certs", "pebble.minica.pem"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(pebbleBin, "-config", configFile, "-strict")
	cmd.Dir = testdata
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 pebble: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	// 等待目录就绪
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "localhost:14000", 2*time.Second)
		if err == nil {
			conn.Close()
			return caFile
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("pebble 未在 30s 内就绪")
	return ""
}

func ensureDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
}

func newTestProvider(t *testing.T, caFile string) *Provider {
	t.Helper()
	p, err := NewWithCA(testCryptor{}, caFile)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPebbleHTTP01IssueRenewRevoke(t *testing.T) {
	caFile := startPebble(t)
	provider := newTestProvider(t, caFile)
	ctx := context.Background()

	account := &domain.ProviderAccount{
		ID:           "pebble-acc",
		Provider:     "acme",
		Name:         "pebble",
		DirectoryURL: "https://localhost:14000/dir",
		Contact:      "admin@example.com",
	}

	// 1. 账户注册（TestConnection 会注册并保存凭据）
	if err := provider.TestConnection(ctx, account); err != nil {
		t.Fatalf("注册账户: %v", err)
	}
	if account.CredentialsEnc == "" {
		t.Fatal("账户凭据未保存")
	}

	// 2. 签发（HTTP-01，验证端口 5002 对应 pebble-config httpPort）
	issued, err := provider.Issue(ctx, providers.IssueRequest{
		Account:      account,
		Domains:      []string{"localhost"},
		Verification: domain.VerifyHTTP01,
		HTTP01Port:   5002,
	})
	if err != nil {
		t.Fatalf("签发: %v", err)
	}
	if !strings.Contains(issued.CertificatePEM, "BEGIN CERTIFICATE") {
		t.Fatal("证书内容缺失")
	}
	if !strings.Contains(issued.PrivateKeyPEM, "PRIVATE KEY") {
		t.Fatal("私钥内容缺失")
	}
	if issued.NotAfter.Before(time.Now()) {
		t.Fatalf("证书有效期异常: %v", issued.NotAfter)
	}
	firstKey := issued.PrivateKeyPEM

	// 3. 续期（复用私钥）
	renewed, err := provider.Renew(ctx, providers.RenewRequest{
		Account:        account,
		Domains:        []string{"localhost"},
		Verification:   domain.VerifyHTTP01,
		CertificatePEM: issued.CertificatePEM,
		PrivateKeyPEM:  firstKey,
	})
	if err != nil {
		t.Fatalf("续期: %v", err)
	}
	if !strings.Contains(renewed.CertificatePEM, "BEGIN CERTIFICATE") {
		t.Fatal("续期证书缺失")
	}

	// 4. 吊销
	if err := provider.Revoke(ctx, providers.RevokeRequest{
		Account:        account,
		CertificatePEM: renewed.CertificatePEM,
	}); err != nil {
		t.Fatalf("吊销: %v", err)
	}
}

func TestPebbleInvalidDomainFails(t *testing.T) {
	caFile := startPebble(t)
	provider := newTestProvider(t, caFile)
	ctx := context.Background()

	account := &domain.ProviderAccount{
		ID:           "pebble-acc-2",
		Provider:     "acme",
		Name:         "pebble2",
		DirectoryURL: "https://localhost:14000/dir",
		Contact:      "admin@example.com",
	}
	if err := provider.TestConnection(ctx, account); err != nil {
		t.Fatalf("注册账户: %v", err)
	}

	// pebble-config 中 domainBlocklist 包含 blocked-domain.example
	_, err := provider.Issue(ctx, providers.IssueRequest{
		Account:      account,
		Domains:      []string{"blocked-domain.example"},
		Verification: domain.VerifyHTTP01,
		HTTP01Port:   5002,
	})
	if err == nil {
		t.Fatal("预期 blocked 域名签发失败，但成功了")
	}
}

// TestPebbleCSRIssue 外部 CSR 签发：私钥保留在客户端，签发证书公钥与 CSR 匹配
func TestPebbleCSRIssue(t *testing.T) {
	caFile := startPebble(t)
	provider := newTestProvider(t, caFile)
	ctx := context.Background()

	account := &domain.ProviderAccount{
		ID:           "pebble-csr-acc",
		Provider:     "acme",
		Name:         "pebble-csr",
		DirectoryURL: "https://localhost:14000/dir",
		Contact:      "admin@example.com",
	}
	if err := provider.TestConnection(ctx, account); err != nil {
		t.Fatalf("注册账户: %v", err)
	}

	// 客户端生成密钥对与 CSR（模拟外部密钥保留场景）
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	issued, err := provider.Issue(ctx, providers.IssueRequest{
		Account:      account,
		Domains:      []string{"localhost"},
		Verification: domain.VerifyHTTP01,
		HTTP01Port:   5002,
		CSRPEM:       csrPEM,
	})
	if err != nil {
		t.Fatalf("CSR 签发: %v", err)
	}
	if !strings.Contains(issued.CertificatePEM, "BEGIN CERTIFICATE") {
		t.Fatal("证书内容缺失")
	}
	// CSR 模式：服务端不返回私钥
	if issued.PrivateKeyPEM != "" {
		t.Fatal("CSR 模式不应返回私钥")
	}
	// 校验：签发证书的公钥与 CSR 公钥一致（密钥确实保留在客户端）
	block, _ := pem.Decode([]byte(issued.CertificatePEM))
	if block == nil {
		t.Fatal("解析证书失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.PublicKey.(*ecdsa.PublicKey).Equal(&key.PublicKey) {
		t.Fatal("签发证书公钥与 CSR 不匹配")
	}
	// CN 或 SAN 必须包含 localhost（Pebble 可能用 SAN 作为 CN）
	domains := append([]string{cert.Subject.CommonName}, cert.DNSNames...)
	found := false
	for _, d := range domains {
		if d == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("证书未包含 localhost: CN=%q SAN=%v", cert.Subject.CommonName, cert.DNSNames)
	}

	// 无效 CSR 拒绝
	_, err = provider.Issue(ctx, providers.IssueRequest{
		Account:      account,
		Domains:      []string{"localhost"},
		Verification: domain.VerifyHTTP01,
		HTTP01Port:   5002,
		CSRPEM:       "not-a-csr",
	})
	if err == nil {
		t.Fatal("预期无效 CSR 被拒绝")
	}

	// 篡改签名 CSR 拒绝（公钥与签名不匹配）
	badKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	badDER, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"},
	}, badKey)
	// 直接篡改 DER：翻转一个字节使签名校验失败
	if len(badDER) > 10 {
		badDER[len(badDER)-1] ^= 0xFF
		tampered := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: badDER}))
		_, err = provider.Issue(ctx, providers.IssueRequest{
			Account:      account,
			Domains:      []string{"localhost"},
			Verification: domain.VerifyHTTP01,
			HTTP01Port:   5002,
			CSRPEM:       tampered,
		})
		if err == nil {
			t.Fatal("预期篡改签名的 CSR 被拒绝")
		}
	}
}
