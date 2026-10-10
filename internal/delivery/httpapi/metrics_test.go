package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestMetricsEndpoint(t *testing.T) {
	s := newTestServer(t)

	// 指标含运营计数：匿名拒绝
	rec := s.do(http.MethodGet, "/metrics", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for anonymous metrics, got %d", rec.Code)
	}

	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 只读角色无权限
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")
	rec = s.do(http.MethodGet, "/metrics", nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly metrics, got %d", rec.Code)
	}

	// 管理员可抓取
	rec = s.do(http.MethodGet, "/metrics", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics: %d", rec.Code)
	}
	body := rec.Body.String()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("unexpected content type: %s", ct)
	}
	for _, metric := range []string{
		"argus_build_info",
		"argus_certificates_total",
		"argus_certificates_expiring_30d",
		"argus_certificates_expired",
		"argus_renewals_total",
		"argus_deploy_records_total{status=\"success\"}",
		"argus_notify_sent_total{status=\"success\"}",
		"argus_agents_total{status=\"online\"}",
		"argus_users_total",
		"argus_uptime_seconds",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("missing metric %q in:\n%s", metric, body)
		}
	}
}

func TestOpenAPISpecEndpoint(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/openapi.yaml", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"openapi: 3.0.3",
		"title: Argus API",
		"/certificates:",
		"/auth/login:",
		"bearerAuth",
		"X-API-Token",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in openapi spec", want)
		}
	}
}
