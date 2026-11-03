package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// 路径与方法映射是远程模式最易出错的地方（历史上两个 bug 都出在这类映射：
// 漏接分支、DTO 字段丢失）。逐个工具断言实际发出的 method + path + body。
func TestRemoteOpsPathMapping(t *testing.T) {
	type req struct {
		method string
		path   string
		body   map[string]any
	}
	var captured req

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.body = nil
		if r.Body != nil {
			var b map[string]any
			if json.NewDecoder(r.Body).Decode(&b) == nil && len(b) > 0 {
				captured.body = b
			}
		}
		w.Header().Set("Content-Type", "application/json")
		// 按端点返回对应形态：列表类给数组，单条/写操作给带 ID 的对象
		switch r.URL.Path {
		case "/api/v1/deploy-targets", "/api/v1/agents", "/api/v1/task-logs", "/api/v1/acme-accounts", "/api/v1/dns-accounts":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"id":"c1"}}`))
		}
	}))
	defer srv.Close()

	ops := NewRemoteOps(RemoteConfig{BaseURL: srv.URL, Token: "st_x"})
	ctx := context.Background()

	cases := []struct {
		name   string
		call   func() error
		method string
		path   string
	}{
		{"RenewCertificate", func() error {
			_, err := ops.RenewCertificate(ctx, "c1", "")
			return err
		}, "POST", "/api/v1/certificates/c1/renew"},
		{"RevokeCertificate", func() error {
			return ops.RevokeCertificate(ctx, "c1", "")
		}, "POST", "/api/v1/certificates/c1/revoke"},
		{"ImportCertificate", func() error {
			_, err := ops.ImportCertificate(ctx, ImportParams{Domains: []string{"a.com"}, Certificate: "c", PrivateKey: "k"})
			return err
		}, "POST", "/api/v1/certificates/import"},
		{"ListTargets", func() error {
			_, err := ops.ListTargets(ctx)
			return err
		}, "GET", "/api/v1/deploy-targets"},
		{"DeployCertificate", func() error {
			return ops.DeployCertificate(ctx, "c1", "", []string{"t1"})
		}, "POST", "/api/v1/certificates/c1/deploy"},
		{"TestTarget", func() error {
			return ops.TestTarget(ctx, "t1")
		}, "POST", "/api/v1/deploy-targets/t1/test"},
		{"ListAgents", func() error {
			_, err := ops.ListAgents(ctx)
			return err
		}, "GET", "/api/v1/agents"},
		{"TaskLogs", func() error {
			_, err := ops.TaskLogs(ctx, 20)
			return err
		}, "GET", "/api/v1/task-logs"}, // limit 查询串由 TestRemoteOpsTaskLogsLimitNormalized 覆盖
	}

	for _, c := range cases {
		captured = req{}
		if err := c.call(); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if captured.method != c.method || captured.path != c.path {
			t.Errorf("%s: got %s %s, want %s %s", c.name, captured.method, captured.path, c.method, c.path)
		}
	}
}

// ListAccounts 两个渠道各拉一次，且 type 非法时报错
func TestRemoteOpsListAccounts(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	ops := NewRemoteOps(RemoteConfig{BaseURL: srv.URL, Token: "st_x"})

	// type=acme 只拉 acme
	paths = nil
	if _, err := ops.ListAccounts(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/api/v1/acme-accounts" {
		t.Fatalf("acme: got %v", paths)
	}

	// type=dns 只拉 dns
	paths = nil
	if _, err := ops.ListAccounts(context.Background(), "dns"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/api/v1/dns-accounts" {
		t.Fatalf("dns: got %v", paths)
	}

	// 空 type 两个都拉
	paths = nil
	if _, err := ops.ListAccounts(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("empty type should fetch both, got %v", paths)
	}

	// 非法 type 报错且不发请求
	paths = nil
	if _, err := ops.ListAccounts(context.Background(), "bogus"); err == nil {
		t.Fatal("expected error for invalid type")
	}
	if len(paths) != 0 {
		t.Fatalf("invalid type should not issue requests, got %v", paths)
	}
}

// TaskLogs 的 limit 归一化（<=0 → 20）
func TestRemoteOpsTaskLogsLimitNormalized(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	ops := NewRemoteOps(RemoteConfig{BaseURL: srv.URL, Token: "st_x"})

	for _, in := range []int{0, -5} {
		if _, err := ops.TaskLogs(context.Background(), in); err != nil {
			t.Fatal(err)
		}
		if query != "limit=20" {
			t.Errorf("limit %d: query = %q, want limit=20", in, query)
		}
	}
}

// certBrief：结构化摘要不得泄露证书/私钥 PEM，且天数计算正确。
// now 显式传入，故断言是确定性的。
func TestCertBriefNoSecretLeak(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cert := &domain.Certificate{
		ID:              "c1",
		Domains:         []string{"a.example.com", "www.a.example.com"},
		PrimaryDomain:   "a.example.com",
		Provider:        "acme",
		Status:          domain.StatusValid,
		NotAfter:        now.Add(45 * 24 * time.Hour),
		CertificatePEM:  "-----BEGIN CERTIFICATE-----\nSECRET\n-----END CERTIFICATE-----",
		PrivateKeyEnc:   "encrypted-blob",
		DeployTargetIDs: []string{"t1"},
	}
	brief := certBrief(cert, now)

	// 关键字段齐全
	if brief["id"] != "c1" || brief["primary_domain"] != "a.example.com" {
		t.Fatalf("brief missing identity: %v", brief)
	}
	if brief["days_to_expire"] != int64(45) {
		t.Errorf("days_to_expire = %v, want 45", brief["days_to_expire"])
	}
	// 已过期证书天数为负
	expired := certBrief(&domain.Certificate{NotAfter: now.Add(-72 * time.Hour)}, now)
	if expired["days_to_expire"] != int64(-3) {
		t.Errorf("expired days_to_expire = %v, want -3", expired["days_to_expire"])
	}
	// 无有效期时天数归零（不出现负零或垃圾值）
	zero := certBrief(&domain.Certificate{}, now)
	if zero["days_to_expire"] != int64(0) {
		t.Errorf("zero-value days_to_expire = %v, want 0", zero["days_to_expire"])
	}
	// 敏感字段不得出现
	for k := range brief {
		switch k {
		case "certificate_pem", "CertificatePEM", "private_key_enc", "PrivateKeyEnc", "pem", "key":
			t.Errorf("brief leaks sensitive field %q", k)
		}
	}
	// 序列化后也不得含 PEM 标记或密文
	blob, _ := json.Marshal(brief)
	if strings.Contains(string(blob), "BEGIN CERTIFICATE") || strings.Contains(string(blob), "encrypted-blob") {
		t.Fatalf("brief serialization leaks secrets: %s", blob)
	}
}
