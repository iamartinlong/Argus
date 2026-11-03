package mcp

import (
	"strings"
	"testing"
)

// DTO 转换必须覆盖 REST 响应的下划线字段（domain 实体无 json tag，
// 直接 unmarshal 会丢 not_after/primary_domain 等——曾导致远程导入的证书
// 显示零值有效期）。
func TestCertDTOToDomain(t *testing.T) {
	d := certDTO{
		ID:              "c1",
		Domains:         []string{"a.example.com", "www.a.example.com"},
		PrimaryDomain:   "a.example.com",
		Provider:        "import",
		Status:          "valid",
		NotBefore:       "2026-11-03T00:00:00Z",
		NotAfter:        "2027-02-01T06:08:57Z",
		Verification:    "dns01",
		DeployTargetIDs: []string{"t1"},
		NotifyEvents:    []string{"cert_expiring"},
		DomainID:        "d1",
		DomainName:      "a.example.com",
	}
	c := d.toDomain()
	if c.PrimaryDomain != "a.example.com" {
		t.Fatalf("primary_domain lost: %q", c.PrimaryDomain)
	}
	if c.NotAfter.IsZero() || c.NotAfter.Year() != 2027 {
		t.Fatalf("not_after lost: %v", c.NotAfter)
	}
	if c.NotBefore.IsZero() {
		t.Fatalf("not_before lost: %v", c.NotBefore)
	}
	if len(c.NotifyEvents) != 1 || c.NotifyEvents[0] != "cert_expiring" {
		t.Fatalf("notify_events lost: %v", c.NotifyEvents)
	}
	if c.DomainID != "d1" || c.DomainName != "a.example.com" {
		t.Fatalf("domain link lost: %q/%q", c.DomainID, c.DomainName)
	}
}

// 空时间字符串 → 零值（不 panic）
func TestParseTimeEmpty(t *testing.T) {
	if !parseTime("").IsZero() {
		t.Fatal("empty should be zero")
	}
	if got := parseTime("2026-11-03T00:00:00Z"); got.IsZero() || got.Year() != 2026 {
		t.Fatalf("parse failed: %v", got)
	}
	// 非法字符串兜底为零值而非 panic
	if !parseTime("not-a-time").IsZero() {
		t.Fatal("invalid should be zero")
	}
}

// 目标/Agent/任务日志 DTO 转换
func TestOtherDTOs(t *testing.T) {
	tg := targetDTO{ID: "t1", Name: "web", Type: "ssh", Enabled: true, LastStatus: "success", CreatedAt: "2026-11-03T00:00:00Z"}.toDomain()
	if tg.Name != "web" || tg.Type != "ssh" || tg.CreatedAt.IsZero() {
		t.Fatalf("target dto: %+v", tg)
	}
	ag := agentDTO{ID: "a1", Name: "agent1", Status: "online", Version: "1.3.0", OS: "linux"}.toDomain()
	if ag.Name != "agent1" || ag.Version != "1.3.0" {
		t.Fatalf("agent dto: %+v", ag)
	}
	tl := taskLogDTO{ID: "l1", Type: "auto_renew", Status: "success", StartedAt: "2026-11-03T00:00:00Z"}.toDomain()
	if tl.Type != "auto_renew" || tl.StartedAt.IsZero() {
		t.Fatalf("tasklog dto: %+v", tl)
	}
}

// 远程错误映射：HTTP 状态 → domain 错误语义
func TestRemoteErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		code   string
		msg    string
		want   string
	}{
		{404, "not_found", "x", "not found"},
		{409, "conflict", "x", "conflict"},
		{400, "invalid_argument", "x", "invalid argument"},
		{401, "unauthorized", "x", "unauthorized"},
		{403, "forbidden", "x", "forbidden"},
		{429, "too_many_requests", "x", "locked"},
		{500, "internal_error", "boom", "服务端错误 500"},
	}
	for _, c := range cases {
		err := remoteError(c.status, c.code, c.msg)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("remoteError(%d) = %v, want contains %q", c.status, err, c.want)
		}
	}
}
