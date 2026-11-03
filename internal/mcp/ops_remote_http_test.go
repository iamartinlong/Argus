package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

// newTestServer 起一个记录请求的 httptest 服务端，按 handler 返回响应。
func newTestServer(t *testing.T, handler http.HandlerFunc) (*RemoteOps, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Clone(context.Background()))
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewRemoteOps(RemoteConfig{BaseURL: srv.URL, Token: "st_testtoken"}), &got
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// 鉴权头与基础路径：每次请求都必须带 X-API-Token
func TestRemoteOpsAuthHeader(t *testing.T) {
	ops, got := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, []certDTO{})
	})
	if _, err := ops.ListCertificates(context.Background(), ListFilter{}); err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*got))
	}
	r := (*got)[0]
	if r.Header.Get("X-API-Token") != "st_testtoken" {
		t.Fatalf("X-API-Token = %q, want st_testtoken", r.Header.Get("X-API-Token"))
	}
	if r.URL.Path != "/api/v1/certificates" {
		t.Fatalf("path = %q, want /api/v1/certificates", r.URL.Path)
	}
}

// 信封解包：REST 的 {code,data} 结构应正确取出 data
func TestRemoteOpsEnvelopeUnpack(t *testing.T) {
	ops, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, []certDTO{{
			ID:            "c1",
			Domains:       []string{"a.example.com"},
			PrimaryDomain: "a.example.com",
			Status:        "valid",
			NotAfter:      "2027-02-01T06:08:57Z",
		}})
	})
	certs, err := ops.ListCertificates(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(certs))
	}
	// 下划线字段必须经 DTO 正确转换（这是曾出过 bug 的地方）
	if certs[0].PrimaryDomain != "a.example.com" {
		t.Errorf("primary_domain lost: %q", certs[0].PrimaryDomain)
	}
	if certs[0].NotAfter.IsZero() || certs[0].NotAfter.Year() != 2027 {
		t.Errorf("not_after lost: %v", certs[0].NotAfter)
	}
}

// 服务端过滤不存在：status/expiring/keyword 过滤在客户端完成
func TestRemoteOpsClientSideFilter(t *testing.T) {
	ops, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, []certDTO{
			{ID: "c1", PrimaryDomain: "keep.example.com", Status: "valid", NotAfter: "2027-02-01T00:00:00Z"},
			{ID: "c2", PrimaryDomain: "drop.example.com", Status: "expired", NotAfter: "2026-01-01T00:00:00Z"},
		})
	})
	certs, err := ops.ListCertificates(context.Background(), ListFilter{Status: "valid"})
	if err != nil {
		t.Fatalf("ListCertificates: %v", err)
	}
	if len(certs) != 1 || certs[0].ID != "c1" {
		t.Fatalf("status filter failed: %+v", certs)
	}

	// 关键字过滤（主域或任一 SAN）
	certs, err = ops.ListCertificates(context.Background(), ListFilter{Keyword: "drop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 || certs[0].ID != "c2" {
		t.Fatalf("keyword filter failed: %+v", certs)
	}
}

// 错误映射：HTTP 状态 → domain 错误语义（供上层工具统一处理）
func TestRemoteOpsErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusForbidden, `{"message":"token 权限不足"}`, domain.ErrForbidden},
		{http.StatusUnauthorized, `{"message":"bad token"}`, domain.ErrUnauthorized},
		{http.StatusNotFound, `{"message":"not found"}`, domain.ErrNotFound},
		{http.StatusConflict, `{"message":"dup"}`, domain.ErrConflict},
		{http.StatusBadRequest, `{"message":"invalid"}`, domain.ErrInvalidArgument},
	}
	for _, c := range cases {
		ops, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		})
		_, err := ops.ListCertificates(context.Background(), ListFilter{})
		if err == nil {
			t.Fatalf("status %d: expected error", c.status)
		}
		// 错误链上应能匹配到对应 domain 错误
		if !isDomainErr(err, c.want) {
			t.Errorf("status %d: err = %v, want wraps %v", c.status, err, c.want)
		}
	}
}

// 5xx 不透出服务端细节（只给状态码与消息）
func TestRemoteOpsServerError(t *testing.T) {
	ops, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"panic at internal/secrets.go:42"}`))
	})
	_, err := ops.ListCertificates(context.Background(), ListFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should carry status: %v", err)
	}
}

// 连接失败（服务端不可达）应给出可读错误而非 panic
func TestRemoteOpsConnectionRefused(t *testing.T) {
	ops := NewRemoteOps(RemoteConfig{BaseURL: "http://127.0.0.1:1", Token: "st_x"})
	if _, err := ops.ListCertificates(context.Background(), ListFilter{}); err == nil {
		t.Fatal("expected connection error")
	}
}

// GetCertificate 按 ID 走单条端点
func TestRemoteOpsGetByID(t *testing.T) {
	ops, got := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, certDTO{ID: "c1", PrimaryDomain: "a.example.com", NotAfter: "2027-02-01T00:00:00Z"})
	})
	cert, err := ops.GetCertificate(context.Background(), "c1", "")
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert.ID != "c1" || cert.PrimaryDomain != "a.example.com" {
		t.Fatalf("unexpected cert: %+v", cert)
	}
	if (*got)[0].URL.Path != "/api/v1/certificates/c1" {
		t.Fatalf("path = %q", (*got)[0].URL.Path)
	}
}

// 按域名查询：先拉列表再在客户端匹配（远程无服务端按域名端点）
func TestRemoteOpsGetByDomain(t *testing.T) {
	ops, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/certificates" {
			writeEnvelope(t, w, []certDTO{
				{ID: "old", PrimaryDomain: "a.example.com", NotAfter: "2026-06-01T00:00:00Z"},
				{ID: "new", PrimaryDomain: "a.example.com", NotAfter: "2027-06-01T00:00:00Z"},
			})
			return
		}
		writeEnvelope(t, w, []certDTO{})
	})
	cert, err := ops.GetCertificate(context.Background(), "", "a.example.com")
	if err != nil {
		t.Fatalf("GetCertificate(domain): %v", err)
	}
	// 取到期最晚的一张
	if cert.ID != "new" {
		t.Fatalf("expected latest cert, got %q", cert.ID)
	}
}

// 写操作应带正确请求体（issue）
func TestRemoteOpsIssueBody(t *testing.T) {
	var body map[string]any
	ops, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeEnvelope(t, w, certDTO{ID: "c1", PrimaryDomain: "a.example.com"})
	})
	cert, err := ops.IssueCertificate(context.Background(), IssueParams{
		Domains:         []string{"a.example.com"},
		AccountID:       "acc1",
		Verification:    "dns01",
		DeployTargetIDs: []string{"t1"},
		NotifyEvents:    []string{"cert_expiring"},
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if cert.ID != "c1" {
		t.Fatalf("unexpected cert: %+v", cert)
	}
	if body["domains"] == nil || body["account_id"] != "acc1" || body["verification"] != "dns01" {
		t.Fatalf("request body missing fields: %v", body)
	}
	if body["deploy_target_ids"] == nil || body["notify_events"] == nil {
		t.Fatalf("request body missing optional arrays: %v", body)
	}
}

// 远程模式不提供私钥下载（最小暴露原则）
func TestRemoteOpsDownloadForbidden(t *testing.T) {
	ops := NewRemoteOps(RemoteConfig{BaseURL: "http://127.0.0.1:1", Token: "st_x"})
	data, name, err := ops.DownloadCertificate(context.Background(), "c1", "nginx")
	if err == nil {
		t.Fatal("remote download must be refused")
	}
	if data != nil || name != "" {
		t.Fatalf("expected nil data/name, got %q", name)
	}
	if !isDomainErr(err, domain.ErrForbidden) {
		t.Errorf("want ErrForbidden, got %v", err)
	}
}

// ---------- 辅助 ----------

func isDomainErr(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
