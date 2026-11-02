package domain

import (
	"strings"
	"time"
)

// ExpectedStatus 域名台账的期望状态
type ExpectedStatus string

const (
	ExpectedMustHaveCert ExpectedStatus = "must_have_cert" // 必须有证书，缺失即异常
	ExpectedMonitorOnly  ExpectedStatus = "monitor_only"   // 仅关注，不要求证书
)

func (e ExpectedStatus) Valid() bool {
	return e == ExpectedMustHaveCert || e == ExpectedMonitorOnly
}

// DomainSource 台账项来源
type DomainSource string

const (
	DomainSourceManual DomainSource = "manual" // 手工登记
)

func (s DomainSource) Valid() bool { return s == DomainSourceManual }

// Domain 域名台账项（主数据）：用户维护的"我关心的域名"清单。
// 与证书不建映射表——关联通过 certificates.domain_id 引用 + domain_name 副本，
// 读取时按关联规则计算覆盖状态（见 MatchesDomain / ComputeCoverage）。
type Domain struct {
	ID             string
	Name           string // 规范化后的域名（唯一）
	ExpectedStatus ExpectedStatus
	Source         DomainSource
	Note           string
	Tags           []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Coverage 域名覆盖状态（计算字段，不落库）
type Coverage string

const (
	CoverageMissing  Coverage = "missing"  // 必须有证书但没有
	CoverageExpiring Coverage = "expiring" // 有证书且临近到期
	CoverageExpired  Coverage = "expired"  // 当前证书已过期
	CoverageCovered  Coverage = "covered"  // 有证书且到期日尚远
	CoverageMonitor  Coverage = "monitor"  // 仅关注且无证书
)

// NormalizeDomain 域名规范化：小写、去首尾空白、去单个尾点。
// 全仓域名唯一规范化入口（原 certificate_handler.normalize /
// certificate_service.Import / validImportDomain 三处重复实现统一至此）。
func NormalizeDomain(s string) string {
	d := strings.ToLower(strings.TrimSpace(s))
	return strings.TrimSuffix(d, ".")
}

// ValidDomain 域名格式校验：字母数字/点/连字符，允许通配符前缀 *.，≤253。
// 目的是防注入部署 hook 命令（{domain} 模板拼接 shell）。
func ValidDomain(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	rest := d
	if strings.HasPrefix(rest, "*.") {
		rest = rest[2:]
	}
	for _, r := range rest {
		if r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// MatchesDomain 证书 SAN 是否覆盖指定域名：精确匹配，或通配符父域匹配。
//
//	certDomains=["*.example.com"], name="www.example.com" → true
//	certDomains=["www.example.com"], name="example.com" → false（子域不覆盖父域）
func MatchesDomain(certDomains []string, name string) bool {
	if name == "" {
		return false
	}
	// 通配符父域：去掉首个标签
	parent := ""
	if i := strings.Index(name, "."); i > 0 && i+1 < len(name) {
		parent = "*." + name[i+1:]
	}
	for _, d := range certDomains {
		cd := NormalizeDomain(d)
		if cd == name {
			return true
		}
		if parent != "" && cd == parent {
			return true
		}
	}
	return false
}

// ComputeCoverage 计算单个域名的覆盖状态。
//
//	certs:     关联到该域名的证书（顺序无关，内部取 NotAfter 最大者为"当前证书"）
//	expected:  期望状态（monitor_only 时不要求证书）
//	now:       当前时间（调用方注入，便于测试）
//	renewDays: renew_days_before；<=0 时按 30 天（与调度器默认一致）
func ComputeCoverage(certs []*Certificate, expected ExpectedStatus, now time.Time, renewDays int) Coverage {
	if renewDays <= 0 {
		renewDays = 30
	}
	var current *Certificate
	for _, c := range certs {
		if c.NotAfter.IsZero() {
			continue // pending 等无有效期的证书不计入覆盖
		}
		if current == nil || c.NotAfter.After(current.NotAfter) ||
			(c.NotAfter.Equal(current.NotAfter) && c.UpdatedAt.After(current.UpdatedAt)) {
			current = c
		}
	}
	if current == nil {
		if expected == ExpectedMonitorOnly {
			return CoverageMonitor
		}
		return CoverageMissing
	}
	switch {
	case now.After(current.NotAfter):
		return CoverageExpired
	case now.Add(time.Duration(renewDays) * 24 * time.Hour).After(current.NotAfter):
		return CoverageExpiring
	default:
		return CoverageCovered
	}
}

// DaysToExpire 证书剩余天数（向下取整；已过期为负）。零值 NotAfter 返回 0。
func DaysToExpire(notAfter, now time.Time) int {
	if notAfter.IsZero() {
		return 0
	}
	return int(notAfter.Sub(now).Hours() / 24)
}
