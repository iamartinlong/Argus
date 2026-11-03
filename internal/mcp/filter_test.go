package mcp

import (
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// filterCertificates 表驱动测试。now 显式传入，断言完全确定。
// 本地与远程两条通路共用此函数——覆盖它即覆盖两侧的过滤行为。
func TestFilterCertificates(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	// 构造样本：id 即期望标识，NotAfter 相对 now 设置
	cert := func(id string, after time.Time, domains ...string) *domain.Certificate {
		return &domain.Certificate{
			ID:             id,
			Domains:        domains,
			PrimaryDomain:  domains[0],
			Status:         domain.StatusValid,
			NotAfter:       after,
			CertificatePEM: "SECRET-PEM",
			PrivateKeyEnc:  "SECRET-ENC",
		}
	}
	sample := []*domain.Certificate{
		cert("far", now.Add(90*day), "far.example.com"),
		cert("near", now.Add(10*day), "near.example.com", "www.near.example.com"),
		cert("expired", now.Add(-5*day), "expired.example.com"),
		cert("noexpiry", time.Time{}, "pending.example.com"),
		cert("revoked", now.Add(90*day), "revoked.example.com"),
	}
	// revoked 为终态，UpdateStatus 不得改写
	sample[4].Status = domain.StatusRevoked

	ids := func(certs []*domain.Certificate) []string {
		out := make([]string, 0, len(certs))
		for _, c := range certs {
			out = append(out, c.ID)
		}
		return out
	}
	same := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name   string
		filter ListFilter
		want   []string
	}{
		{"空过滤返回全部", ListFilter{}, []string{"far", "near", "expired", "noexpiry", "revoked"}},
		{"状态 valid（仅远期；near 10 天已被刷成 expiring）", ListFilter{Status: "valid"}, []string{"far"}},
		{"状态 expiring", ListFilter{Status: "expiring"}, []string{"near"}},
		{"状态 expired", ListFilter{Status: "expired"}, []string{"expired"}},
		{"状态 pending（无有效期）", ListFilter{Status: "pending"}, []string{"noexpiry"}},
		{"状态 revoked（终态保持）", ListFilter{Status: "revoked"}, []string{"revoked"}},
		{"不存在的状态", ListFilter{Status: "failed"}, nil},
		{"临期 30 天", ListFilter{ExpiringWithinDays: 30}, []string{"near", "expired"}},
		{"临期 7 天", ListFilter{ExpiringWithinDays: 7}, []string{"expired"}},
		{"临期 0 天不过滤", ListFilter{ExpiringWithinDays: 0}, []string{"far", "near", "expired", "noexpiry", "revoked"}},
		{"关键字命中主域", ListFilter{Keyword: "near"}, []string{"near"}},
		{"关键字命中 SAN", ListFilter{Keyword: "www.near"}, []string{"near"}},
		{"关键字大小写不敏感", ListFilter{Keyword: "FAR.EXAMPLE"}, []string{"far"}},
		{"关键字无匹配", ListFilter{Keyword: "nomatch"}, nil},
		{"状态+临期组合", ListFilter{Status: "expiring", ExpiringWithinDays: 30}, []string{"near"}},
		{"状态+关键字组合", ListFilter{Status: "valid", Keyword: "far"}, []string{"far"}},
		{"三者组合无命中", ListFilter{Status: "expired", Keyword: "far"}, nil},
	}

	for _, c := range cases {
		got := ids(filterCertificates(sample, c.filter, now))
		if !same(got, c.want...) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// 过滤必须刷新过渡态（pending/valid 的证书在列表读取时按有效期重算）
func TestFilterCertificatesUpdatesStatus(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	// 库里存的是 valid，但实际已过期 → 过滤后应体现为 expired
	cert := &domain.Certificate{
		ID:            "c1",
		PrimaryDomain: "a.example.com",
		Status:        domain.StatusValid,
		NotAfter:      now.Add(-48 * time.Hour),
	}
	got := filterCertificates([]*domain.Certificate{cert}, ListFilter{}, now)
	if len(got) != 1 {
		t.Fatalf("expected 1 cert, got %d", len(got))
	}
	if got[0].Status != domain.StatusExpired {
		t.Errorf("status not refreshed: %q, want expired", got[0].Status)
	}
	// 原对象也应被刷新（与 REST 列表行为一致）
	if cert.Status != domain.StatusExpired {
		t.Errorf("input cert not refreshed in place: %q", cert.Status)
	}
}

// 终态证书（revoked/failed）不得被改写，即使已过期
func TestFilterCertificatesTerminalPreserved(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, s := range []domain.CertificateStatus{domain.StatusRevoked, domain.StatusFailed} {
		cert := &domain.Certificate{
			ID:            "c1",
			PrimaryDomain: "a.example.com",
			Status:        s,
			NotAfter:      now.Add(-48 * time.Hour),
		}
		filterCertificates([]*domain.Certificate{cert}, ListFilter{}, now)
		if cert.Status != s {
			t.Errorf("terminal status %q rewritten to %q", s, cert.Status)
		}
	}
}

// nil 元素不得 panic（脏数据防御）
func TestFilterCertificatesNilSafe(t *testing.T) {
	now := time.Now()
	got := filterCertificates([]*domain.Certificate{nil, {ID: "c1", PrimaryDomain: "a.com"}}, ListFilter{}, now)
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("nil handling broken: %+v", got)
	}
}

// 空输入返回空切片（非 nil，便于 JSON 序列化为 []）
func TestFilterCertificatesEmptyInput(t *testing.T) {
	got := filterCertificates(nil, ListFilter{}, time.Now())
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// ---------- ops_local 纯函数（此前 0% 覆盖） ----------

func TestCertMatchesKeyword(t *testing.T) {
	cert := &domain.Certificate{
		PrimaryDomain: "WWW.Example.COM",
		Domains:       []string{"www.example.com", "api.example.com", "*.wild.example.com"},
	}
	cases := []struct {
		keyword string
		want    bool
	}{
		{"", true},                   // 空关键字恒真
		{"   ", true},                // 空白按空处理
		{"example", true},            // 子串命中
		{"EXAMPLE", true},            // 大小写不敏感
		{"  api  ", true},            // 前后空白被 trim
		{"api.example.com", true},    // 完整域名
		{"*.wild.example.com", true}, // SAN 精确
		{"wild.example.com", true},   // SAN 子串
		{"notexample.com", false},    // 不匹配
		{"www.example.org", false},   // 后缀不同
	}
	for _, c := range cases {
		if got := certMatchesKeyword(cert, c.keyword); got != c.want {
			t.Errorf("certMatchesKeyword(%q) = %v, want %v", c.keyword, got, c.want)
		}
	}
}

func TestNormalizeDomainName(t *testing.T) {
	cases := map[string]string{
		"  Example.COM ": "example.com",
		"example.com":    "example.com",
		"":               "",
		"   ":            "",
		"*.EXAMPLE.com":  "*.example.com",
	}
	for in, want := range cases {
		if got := normalizeDomainName(in); got != want {
			t.Errorf("normalizeDomainName(%q) = %q, want %q", in, got, want)
		}
	}
}
