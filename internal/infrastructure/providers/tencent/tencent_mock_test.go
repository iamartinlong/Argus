package tencent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

type mockCryptor struct{}

func (mockCryptor) EncryptString(s string) (string, error) { return s, nil }
func (mockCryptor) DecryptString(enc string) (string, error) {
	return enc, nil
}

const tcCertPEM = "-----BEGIN CERTIFICATE-----\nMIIBtencent\n-----END CERTIFICATE-----\n"
const tcKeyPEM = "-----BEGIN PRIVATE KEY-----\nMIIE tencent key\n-----END PRIVATE KEY-----\n"

// tencentMockServer 模拟腾讯云 SSL API（TC3 协议：Action 在 X-TC-Action header）
func tencentMockServer(t *testing.T, statusCalls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		action := r.Header.Get("X-TC-Action")
		t.Logf("TENCENT MOCK action=%q body=%s", action, string(body))
		switch action {
		case "DescribeCertificates":
			writeTencentJSON(w, map[string]any{"Certificates": []any{}})
		case "ApplyCertificate":
			writeTencentJSON(w, map[string]any{"CertificateId": "cert-tc-001"})
		case "DescribeCertificateDetail":
			// 第一次审核中，之后已签发（模拟异步）
			if atomic.AddInt32(statusCalls, 1) <= 1 {
				writeTencentJSON(w, map[string]any{"Status": 0, "StatusMsg": "审核中"})
			} else {
				writeTencentJSON(w, map[string]any{
					"Status":                 1,
					"StatusMsg":              "已签发",
					"CertificatePublicKey":   tcCertPEM,
					"CertificatePrivateKey":  tcKeyPEM,
					"Domain":                 "mock.example.com",
					"SubjectAltName":         []string{"mock.example.com"},
					"CertBeginTime":          "2026-01-01 00:00:00",
					"CertEndTime":            "2027-01-01 00:00:00",
				})
			}
		default:
			t.Errorf("unexpected action: %s", action)
			w.WriteHeader(400)
		}
	}))
}

func writeTencentJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"Response": v})
}

func tencentConfigJSON() string {
	cfg := tencentConfig{
		SecretID:     "test-secret-id",
		SecretKey:    "test-secret-key",
		ContactEmail: "ops@example.com",
		ContactPhone: "13800000000",
		DvAuthMethod: "DNS_AUTO",
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

func tcAccount() *domain.ProviderAccount {
	return &domain.ProviderAccount{
		ID:             "acc-tc",
		Provider:       "tencent",
		CredentialsEnc: tencentConfigJSON(),
	}
}

func newMockTencentProvider(serverURL string) *Provider {
	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(serverURL, "http://")
	p.scheme = "http"
	return p
}

func TestTencentTestConnection(t *testing.T) {
	server := tencentMockServer(t, new(int32))
	defer server.Close()

	p := newMockTencentProvider(server.URL)
	if err := p.TestConnection(context.Background(), tcAccount()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
}

func TestTencentIssueFlow(t *testing.T) {
	statusCalls := new(int32)
	server := tencentMockServer(t, statusCalls)
	defer server.Close()

	p := newMockTencentProvider(server.URL)
	issued, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: tcAccount(),
		Domains: []string{"mock.example.com"},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.Contains(issued.CertificatePEM, "BEGIN CERTIFICATE") {
		t.Fatal("证书内容缺失")
	}
	if !strings.Contains(issued.PrivateKeyPEM, "PRIVATE KEY") {
		t.Fatal("私钥内容缺失")
	}
	if issued.NotAfter.IsZero() {
		t.Fatal("有效期未解析")
	}
	if atomic.LoadInt32(statusCalls) < 2 {
		t.Fatalf("应至少轮询 2 次状态，实际 %d", atomic.LoadInt32(statusCalls))
	}
	if issued.NotAfter.Year() != 2027 {
		t.Fatalf("有效期解析异常: %v", issued.NotAfter)
	}
}

func TestTencentMissingCredentials(t *testing.T) {
	p := newMockTencentProvider("127.0.0.1:1")
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: &domain.ProviderAccount{ID: "x", Provider: "tencent", CredentialsEnc: ""},
		Domains: []string{"x.example.com"},
	})
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestTencentIssueFailedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-TC-Action") {
		case "ApplyCertificate":
			writeTencentJSON(w, map[string]any{"CertificateId": "cert-fail"})
		case "DescribeCertificateDetail":
			writeTencentJSON(w, map[string]any{"Status": 2, "StatusMsg": "资料审核失败"})
		}
	}))
	defer server.Close()

	p := newMockTencentProvider(server.URL)
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: tcAccount(),
		Domains: []string{"fail.example.com"},
	})
	if err == nil {
		t.Fatal("expected issue failure")
	}
	if !strings.Contains(err.Error(), "审核失败") && !strings.Contains(err.Error(), "状态码 2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTencentDnsAutoPendingHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-TC-Action") {
		case "ApplyCertificate":
			writeTencentJSON(w, map[string]any{"CertificateId": "cert-pending"})
		case "DescribeCertificateDetail":
			writeTencentJSON(w, map[string]any{"Status": 4, "StatusMsg": "自动添加DNS记录中"})
		}
	}))
	defer server.Close()

	p := newMockTencentProvider(server.URL)
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: tcAccount(),
		Domains: []string{"pending.example.com"},
	})
	if err == nil {
		t.Fatal("expected DNS pending hint")
	}
	if !strings.Contains(err.Error(), "域名验证未完成") {
		t.Fatalf("expected DNS hint, got: %v", err)
	}
}

func TestTencentRenewReusesIssue(t *testing.T) {
	server := tencentMockServer(t, new(int32))
	defer server.Close()

	p := newMockTencentProvider(server.URL)
	issued, err := p.Renew(context.Background(), providers.RenewRequest{
		Account: tcAccount(),
		Domains: []string{"mock.example.com"},
	})
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if issued.CertificatePEM == "" {
		t.Fatal("renew should return certificate")
	}
}
