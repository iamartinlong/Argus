package domain

import (
	"testing"
	"time"
)

// ---------- 枚举 Valid() ----------

func TestRoleValid(t *testing.T) {
	cases := map[Role]bool{
		RoleAdmin: true, RoleOperator: true, RoleReadonly: true,
		Role("superadmin"): false, Role(""): false, Role("ADMIN"): false,
	}
	for r, want := range cases {
		if got := r.Valid(); got != want {
			t.Errorf("Role(%q).Valid() = %v, want %v", r, got, want)
		}
	}
}

func TestVerificationTypeValid(t *testing.T) {
	cases := map[VerificationType]bool{
		VerifyHTTP01: true, VerifyDNS01: true,
		VerificationType("tls-alpn"): false, VerificationType(""): false,
	}
	for v, want := range cases {
		if got := v.Valid(); got != want {
			t.Errorf("VerificationType(%q).Valid() = %v, want %v", v, got, want)
		}
	}
}

func TestDeploySceneAndTypeValid(t *testing.T) {
	for _, s := range []DeployScene{SceneGeneric, SceneNginx, SceneApache, SceneTomcat, SceneIIS} {
		if !s.Valid() {
			t.Errorf("DeployScene(%q) should be valid", s)
		}
	}
	if DeployScene("").Valid() || DeployScene("caddy").Valid() {
		t.Error("empty/unknown DeployScene must be invalid")
	}
	for _, tt := range []DeployTargetType{DeploySSH, DeployAgent, DeployLocal, DeployK8s, DeployWebhook} {
		if !tt.Valid() {
			t.Errorf("DeployTargetType(%q) should be valid", tt)
		}
	}
	if DeployTargetType("ftp").Valid() || DeployTargetType("").Valid() {
		t.Error("unknown DeployTargetType must be invalid")
	}
}

func TestNotificationEventAndChannelValid(t *testing.T) {
	events := []NotificationEvent{
		EventCertExpiring, EventRenewSuccess, EventRenewFailed,
		EventIssueSuccess, EventIssueFailed, EventDeploySuccess, EventDeployFailed,
	}
	for _, e := range events {
		if !e.Valid() {
			t.Errorf("NotificationEvent(%q) should be valid", e)
		}
	}
	if NotificationEvent("").Valid() || NotificationEvent("cert.exploded").Valid() {
		t.Error("unknown NotificationEvent must be invalid")
	}
	for _, c := range []NotificationChannel{ChannelSMTP, ChannelWebhook, ChannelWeCom, ChannelDingTalk, ChannelFeishu} {
		if !c.Valid() {
			t.Errorf("NotificationChannel(%q) should be valid", c)
		}
	}
	if NotificationChannel("sms").Valid() {
		t.Error("unknown NotificationChannel must be invalid")
	}
}

func TestExpectedStatusAndSourceValid(t *testing.T) {
	if !ExpectedMustHaveCert.Valid() || !ExpectedMonitorOnly.Valid() {
		t.Error("both expected statuses should be valid")
	}
	if ExpectedStatus("").Valid() || ExpectedStatus("maybe").Valid() {
		t.Error("unknown ExpectedStatus must be invalid")
	}
	if !DomainSourceManual.Valid() || DomainSource("auto").Valid() {
		t.Error("only manual DomainSource is valid")
	}
}

func TestDnsProviderValid(t *testing.T) {
	for _, p := range []DnsProvider{DnsAliDNS, DnsCloudflare, DnsDnspod, DnsGodaddy} {
		if !p.Valid() {
			t.Errorf("DnsProvider(%q) should be valid", p)
		}
	}
	if DnsProvider("route53").Valid() || DnsProvider("").Valid() {
		t.Error("unknown DnsProvider must be invalid")
	}
}

// ---------- API Token scope（安全关键） ----------

func TestValidScope(t *testing.T) {
	valid := []string{
		ScopeAll, ScopeRead,
		ScopeCertRead, ScopeCertWrite,
		ScopeAccountRead, ScopeAccountWrite,
		ScopeDeployRead, ScopeDeployWrite,
		ScopeNotifyRead, ScopeNotifyWrite,
		ScopeAdminRead, ScopeAdminWrite,
	}
	for _, s := range valid {
		if !ValidScope(s) {
			t.Errorf("ValidScope(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "certificates", "admin", "cert:read", "ALL", "read "} {
		if ValidScope(s) {
			t.Errorf("ValidScope(%q) = true, want false", s)
		}
	}
}

func TestHasScope(t *testing.T) {
	cases := []struct {
		name     string
		scopes   []string
		required string
		want     bool
	}{
		{"all 覆盖一切", []string{ScopeAll}, ScopeAdminWrite, true},
		{"all 覆盖只读", []string{ScopeAll}, ScopeCertRead, true},
		{"精确匹配", []string{ScopeCertWrite}, ScopeCertWrite, true},
		{"精确不匹配", []string{ScopeCertWrite}, ScopeCertRead, false},
		{"read 覆盖只读", []string{ScopeRead}, ScopeCertRead, true},
		{"read 不覆盖写", []string{ScopeRead}, ScopeCertWrite, false},
		{"read 覆盖 admin:read", []string{ScopeRead}, ScopeAdminRead, true},
		{"多 scope 命中其一", []string{ScopeDeployRead, ScopeCertRead}, ScopeCertRead, true},
		{"空 scopes", nil, ScopeCertRead, false},
		{"空 required", []string{ScopeCertRead}, "", false},
	}
	for _, c := range cases {
		tok := &ApiToken{Scopes: c.scopes}
		if got := tok.HasScope(c.required); got != c.want {
			t.Errorf("%s: HasScope(%q) with %v = %v, want %v", c.name, c.required, c.scopes, got, c.want)
		}
	}
}

// nil token 一律拒绝（防止 panic 被当成放行）
func TestHasScopeNilToken(t *testing.T) {
	var tok *ApiToken
	if tok.HasScope(ScopeAll) || tok.HasScope(ScopeCertRead) {
		t.Fatal("nil ApiToken must not have any scope")
	}
}

func TestIsReadScope(t *testing.T) {
	for _, s := range []string{ScopeCertRead, ScopeAdminRead, ScopeDeployRead} {
		if !isReadScope(s) {
			t.Errorf("isReadScope(%q) = false, want true", s)
		}
	}
	for _, s := range []string{ScopeCertWrite, ScopeAll, ScopeRead, "read", ""} {
		if isReadScope(s) {
			t.Errorf("isReadScope(%q) = true, want false", s)
		}
	}
}

// ---------- 证书状态机（核心业务） ----------

func TestCertificateUpdateStatus(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status CertificateStatus
		after  time.Time
		want   CertificateStatus
	}{
		{"远期有效", StatusPending, now.Add(90 * 24 * time.Hour), StatusValid},
		{"30 天内到期", StatusValid, now.Add(10 * 24 * time.Hour), StatusExpiring},
		{"恰好 30 天（边界：未入窗口，仍有效）", StatusValid, now.Add(30 * 24 * time.Hour), StatusValid},
		{"差 1 秒入窗口", StatusValid, now.Add(30*24*time.Hour - time.Second), StatusExpiring},
		{"31 天仍有效", StatusValid, now.Add(31 * 24 * time.Hour), StatusValid},
		{"已过期", StatusValid, now.Add(-time.Hour), StatusExpired},
		{" revoked 终态不被改写", StatusRevoked, now.Add(-time.Hour), StatusRevoked},
		{"failed 终态不被改写", StatusFailed, now.Add(90 * 24 * time.Hour), StatusFailed},
		{"无有效期归 pending", StatusValid, time.Time{}, StatusPending},
	}
	for _, c := range cases {
		cert := &Certificate{Status: c.status, NotAfter: c.after}
		cert.UpdateStatus(now)
		if cert.Status != c.want {
			t.Errorf("%s: got %q, want %q", c.name, cert.Status, c.want)
		}
	}
}

// 终态证书即使无有效期也不得被改写为 pending
func TestCertificateUpdateStatusTerminalWins(t *testing.T) {
	for _, s := range []CertificateStatus{StatusRevoked, StatusFailed} {
		cert := &Certificate{Status: s}
		cert.UpdateStatus(time.Now())
		if cert.Status != s {
			t.Fatalf("terminal status %q was rewritten to %q", s, cert.Status)
		}
	}
}
