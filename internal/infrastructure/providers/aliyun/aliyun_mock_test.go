package aliyun

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
)

type mockCryptor struct{}

func (mockCryptor) EncryptString(s string) (string, error) { return s, nil }
func (mockCryptor) DecryptString(enc string) (string, error) {
	return enc, nil
}

const mockCertPEM = "-----BEGIN CERTIFICATE-----\nMIIBmock\n-----END CERTIFICATE-----\n"
const mockKeyPEM = "-----BEGIN RSA PRIVATE KEY-----\nMIIE mock key\n-----END RSA PRIVATE KEY-----\n"

// aliyunMockServer 模拟阿里云 CAS API（RPC 风格：?Action=xxx）
func aliyunMockServer(t *testing.T, stateCalls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAll(r.Body)
		t.Logf("ALIYUN MOCK %s %s body=%s", r.Method, r.URL.String(), string(body))
		action := r.URL.Query().Get("Action")
		if action == "" {
			action = r.Header.Get("x-acs-action")
		}
		if action == "" {
			params, _ := parseForm(string(body))
			action = params["Action"]
		}
		t.Logf("ALIYUN MOCK action=%q", action)
		switch action {
		case "CreateCertificateRequest":
			writeAliyunJSON(w, map[string]any{"OrderId": 12345})
		case "DescribeCertificateState":
			// 第一次验证中，第二次签发成功（模拟异步）
			if atomic.AddInt32(stateCalls, 1) <= 1 {
				writeAliyunJSON(w, map[string]any{"Type": "VERIFYING", "Uri": "http://verify.example.com"})
			} else {
				writeAliyunJSON(w, map[string]any{"Type": "CERT_ISSUED"})
			}
		case "ListUserCertificateOrder":
			writeAliyunJSON(w, map[string]any{
				"CertificateOrderList": []map[string]any{
					{"Domain": "mock.example.com", "Status": "ISSUED", "CertificateId": 67890},
				},
			})
		case "GetUserCertificateDetail":
			writeAliyunJSON(w, map[string]any{
				"Cert":      mockCertPEM,
				"Key":       mockKeyPEM,
				"StartDate": "2026-01-01",
				"EndDate":   "2027-01-01",
			})
		default:
			t.Errorf("unexpected action: %s", action)
			w.WriteHeader(400)
		}
	}))
}

func writeAliyunJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func parseForm(body string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(body, "&") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out, nil
}

func mockAccount(credentials string) *domain.ProviderAccount {
	return &domain.ProviderAccount{
		ID:             "acc-cloud",
		Provider:       "aliyun",
		CredentialsEnc: credentials,
	}
}

func aliyunConfigJSON() string {
	cfg := aliyunConfig{
		AccessKeyID:     "test-ak",
		AccessKeySecret: "test-sk",
		Region:          "cn-hangzhou",
		ContactEmail:    "ops@example.com",
		ContactPhone:    "13800000000",
		ContactName:     "ops",
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

func TestAliyunTestConnection(t *testing.T) {
	server := aliyunMockServer(t, new(int32))
	defer server.Close()

	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(server.URL, "http://")
	if err := p.TestConnection(context.Background(), mockAccount(aliyunConfigJSON())); err != nil {
		t.Fatalf("test connection: %v", err)
	}
}

func TestAliyunIssueFlow(t *testing.T) {
	stateCalls := new(int32)
	server := aliyunMockServer(t, stateCalls)
	defer server.Close()

	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(server.URL, "http://")

	issued, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(aliyunConfigJSON()),
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
	if issued.NotAfter.IsZero() || !issued.NotAfter.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("有效期解析异常: %v", issued.NotAfter)
	}
	if atomic.LoadInt32(stateCalls) < 2 {
		t.Fatalf("应至少轮询 2 次状态，实际 %d", atomic.LoadInt32(stateCalls))
	}
}

func TestAliyunMissingCredentials(t *testing.T) {
	p := New(mockCryptor{})
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(""),
		Domains: []string{"x.example.com"},
	})
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

func TestAliyunMultiDomainRejected(t *testing.T) {
	p := New(mockCryptor{})
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(aliyunConfigJSON()),
		Domains: []string{"a.example.com", "b.example.com"},
	})
	if err == nil {
		t.Fatal("expected error for multi-domain")
	}
}

func TestAliyunMissingContactInfo(t *testing.T) {
	cfg := aliyunConfig{AccessKeyID: "ak", AccessKeySecret: "sk"}
	b, _ := json.Marshal(cfg)
	p := New(mockCryptor{})
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(string(b)),
		Domains: []string{"x.example.com"},
	})
	if err == nil {
		t.Fatal("expected error for missing contact info")
	}
}

func TestAliyunVerifyFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("x-acs-action")
		switch action {
		case "CreateCertificateRequest":
			writeAliyunJSON(w, map[string]any{"OrderId": 1})
		case "DescribeCertificateState":
			writeAliyunJSON(w, map[string]any{"Type": "VERIFY_FAILED", "Uri": "dns-verify-error"})
		default:
			t.Errorf("unexpected action: %s", action)
		}
	}))
	defer server.Close()

	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(server.URL, "http://")
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(aliyunConfigJSON()),
		Domains: []string{"fail.example.com"},
	})
	if err == nil {
		t.Fatal("expected verify failure")
	}
	if !strings.Contains(err.Error(), "VERIFY_FAILED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAliyunVerifyingHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("x-acs-action")
		switch action {
		case "CreateCertificateRequest":
			writeAliyunJSON(w, map[string]any{"OrderId": 1})
		case "DescribeCertificateState":
			writeAliyunJSON(w, map[string]any{"Type": "VERIFYING", "Uri": "http://verify"})
		default:
			t.Errorf("unexpected action: %s", action)
		}
	}))
	defer server.Close()

	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(server.URL, "http://")
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(aliyunConfigJSON()),
		Domains: []string{"pending.example.com"},
	})
	if err == nil {
		t.Fatal("expected verifying hint error")
	}
	if !strings.Contains(err.Error(), "域名验证未完成") {
		t.Fatalf("expected verifying hint, got: %v", err)
	}
}

func TestAliyunNoIssuedCertFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("x-acs-action")
		switch action {
		case "CreateCertificateRequest":
			writeAliyunJSON(w, map[string]any{"OrderId": 1})
		case "DescribeCertificateState":
			writeAliyunJSON(w, map[string]any{"Type": "CERT_ISSUED"})
		case "ListUserCertificateOrder":
			writeAliyunJSON(w, map[string]any{"CertificateOrderList": []map[string]any{}})
		default:
			t.Errorf("unexpected action: %s", action)
		}
	}))
	defer server.Close()

	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(server.URL, "http://")
	_, err := p.Issue(context.Background(), providers.IssueRequest{
		Account: mockAccount(aliyunConfigJSON()),
		Domains: []string{"missing.example.com"},
	})
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestAliyunRenewReusesIssue(t *testing.T) {
	server := aliyunMockServer(t, new(int32))
	defer server.Close()

	p := New(mockCryptor{})
	p.endpoint = strings.TrimPrefix(server.URL, "http://")
	issued, err := p.Renew(context.Background(), providers.RenewRequest{
		Account: mockAccount(aliyunConfigJSON()),
		Domains: []string{"mock.example.com"},
	})
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if issued.CertificatePEM == "" {
		t.Fatal("renew should return certificate")
	}
}
