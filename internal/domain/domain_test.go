package domain

import (
	"testing"
	"time"
)

func TestNormalizeDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Example.COM ", "example.com"},
		{"www.example.com.", "www.example.com"},
		{"*.EXAMPLE.com", "*.example.com"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := NormalizeDomain(c.in); got != c.want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidDomain(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"example.com", true},
		{"www.example.com", true},
		{"*.example.com", true},
		{"my-site.example.com", true},
		{"", false},
		{"example.com; rm -rf /", false},
		{"example.com/../../etc", false},
		{"exa mple.com", false},
		{"*.*.com", false},
		{"example.com ", false}, // 空格不在允许字符集（调用方先规范化）
	}
	for _, c := range cases {
		if got := ValidDomain(c.in); got != c.want {
			t.Errorf("ValidDomain(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	long := ""
	for len(long) < 254 {
		long += "a"
	}
	if ValidDomain(long + ".com") {
		t.Error("ValidDomain(>253) should be false")
	}
}

func TestMatchesDomain(t *testing.T) {
	cases := []struct {
		certDomains []string
		name        string
		want        bool
	}{
		{[]string{"www.example.com"}, "www.example.com", true},
		{[]string{"WWW.Example.com"}, "www.example.com", true}, // 规范化后比较
		{[]string{"*.example.com"}, "www.example.com", true},   // 通配符父域覆盖子域
		{[]string{"*.example.com"}, "api.example.com", true},
		{[]string{"*.example.com"}, "example.com", false}, // 通配符不覆盖顶级域
		{[]string{"www.example.com"}, "example.com", false}, // 子域不覆盖父域
		{[]string{"a.com", "b.com"}, "b.com", true},
		{[]string{}, "b.com", false},
		{[]string{"b.com"}, "", false},
	}
	for _, c := range cases {
		if got := MatchesDomain(c.certDomains, c.name); got != c.want {
			t.Errorf("MatchesDomain(%v, %q) = %v, want %v", c.certDomains, c.name, got, c.want)
		}
	}
}

func certWith(notAfter time.Time) *Certificate {
	return &Certificate{ID: "c", Domains: []string{"x"}, NotAfter: notAfter}
}

func TestComputeCoverage(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	far := now.Add(90 * 24 * time.Hour)  // 90 天后
	near := now.Add(10 * 24 * time.Hour) // 10 天后
	past := now.Add(-24 * time.Hour)     // 昨天过期

	cases := []struct {
		name     string
		certs    []*Certificate
		expected ExpectedStatus
		renew    int
		want     Coverage
	}{
		{"无证书+必须有", nil, ExpectedMustHaveCert, 30, CoverageMissing},
		{"无证书+仅关注", nil, ExpectedMonitorOnly, 30, CoverageMonitor},
		{"pending 证书不算覆盖", []*Certificate{certWith(time.Time{})}, ExpectedMustHaveCert, 30, CoverageMissing},
		{"远到期=covered", []*Certificate{certWith(far)}, ExpectedMustHaveCert, 30, CoverageCovered},
		{"临近=expiring", []*Certificate{certWith(near)}, ExpectedMustHaveCert, 30, CoverageExpiring},
		{"已过期=expired", []*Certificate{certWith(past)}, ExpectedMustHaveCert, 30, CoverageExpired},
		{"renewDays=0 默认 30", []*Certificate{certWith(near)}, ExpectedMustHaveCert, 0, CoverageExpiring},
		{"多证书取最晚到期", []*Certificate{certWith(past), certWith(far)}, ExpectedMustHaveCert, 30, CoverageCovered},
		{"monitor_only 有证书仍算 expiring", []*Certificate{certWith(near)}, ExpectedMonitorOnly, 30, CoverageExpiring},
	}
	for _, c := range cases {
		if got := ComputeCoverage(c.certs, c.expected, now, c.renew); got != c.want {
			t.Errorf("%s: ComputeCoverage = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDaysToExpire(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	if got := DaysToExpire(now.Add(72*time.Hour), now); got != 3 {
		t.Errorf("DaysToExpire(3d) = %d, want 3", got)
	}
	if got := DaysToExpire(time.Time{}, now); got != 0 {
		t.Errorf("DaysToExpire(zero) = %d, want 0", got)
	}
	if got := DaysToExpire(now.Add(-48*time.Hour), now); got != -2 {
		t.Errorf("DaysToExpire(past) = %d, want -2", got)
	}
}
