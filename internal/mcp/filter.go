package mcp

import (
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// filterCertificates 证书列表过滤：刷新过渡态后按状态/临期天数/关键字筛选。
//
// 本地（直连数据库）与远程（REST 拉全量）两条通路共用此函数——
// 过滤规则曾有两份各自演化的实现（正是远程联调出 bug 的同类风险），
// 收敛为单一实现后只需改一处、只需测一处。
//
// now 显式传入（与 domain.ComputeCoverage 同风格），保证可确定性测试。
func filterCertificates(certs []*domain.Certificate, f ListFilter, now time.Time) []*domain.Certificate {
	out := make([]*domain.Certificate, 0, len(certs))
	for _, c := range certs {
		if c == nil {
			continue
		}
		c.UpdateStatus(now) // 与 REST 列表一致：读取时刷新过渡态
		if f.Status != "" && string(c.Status) != f.Status {
			continue
		}
		if f.ExpiringWithinDays > 0 {
			// 无有效期的证书（CSR 模式/导入中）不参与临期过滤
			if c.NotAfter.IsZero() {
				continue
			}
			if c.NotAfter.After(now.Add(time.Duration(f.ExpiringWithinDays) * 24 * time.Hour)) {
				continue
			}
		}
		if f.Keyword != "" && !certMatchesKeyword(c, f.Keyword) {
			continue
		}
		out = append(out, c)
	}
	return out
}
