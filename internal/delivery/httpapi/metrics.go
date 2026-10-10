package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

// buildVersion 由 cmd 注入（构建时 ldflags 可覆盖）
var buildVersion = "0.5.0"

// metricsHandler Prometheus 文本格式指标（无敏感数据，供监控抓取）
type metricsHandler struct {
	deps  HandlerDeps
	start time.Time
}

func (m *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var out []string
	push := func(name, help string, value int64, labels map[string]string) {
		out = append(out, fmt.Sprintf("# HELP %s %s", name, help))
		out = append(out, fmt.Sprintf("# TYPE %s gauge", name))
		if len(labels) > 0 {
			keys := make([]string, 0, len(labels))
			for k := range labels {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			ls := ""
			for _, k := range keys {
				ls += fmt.Sprintf(`%s="%s",`, k, labels[k])
			}
			out = append(out, fmt.Sprintf(`%s{%s} %d`, name, ls[:len(ls)-1], value))
		} else {
			out = append(out, fmt.Sprintf("%s %d", name, value))
		}
	}
	ctx := r.Context()

	// 版本信息
	out = append(out, `# HELP argus_build_info Build info`, `# TYPE argus_build_info gauge`)
	out = append(out, fmt.Sprintf(`argus_build_info{version="%s"} 1`, buildVersion))

	// 证书
	total, _ := m.deps.CertStore.Count(ctx)
	expiring, _ := m.deps.CertStore.CountExpiring(ctx, 30*24*time.Hour)
	expired, _ := m.deps.CertStore.CountExpired(ctx)
	push("argus_certificates_total", "Certificates total", total, nil)
	push("argus_certificates_expiring_30d", "Certificates expiring within 30 days", expiring, nil)
	push("argus_certificates_expired", "Certificates expired", expired, nil)

	// 续期统计（订单）
	if success, totalRenew, err := m.deps.Orders.RenewStats(ctx, time.Time{}); err == nil {
		push("argus_renewals_total", "Renewal orders total", totalRenew, nil)
		push("argus_renewals_success_total", "Renewal orders success", success, nil)
	}

	// 部署
	deployStatuses := []string{"success", "failed", "running", "pending"}
	for _, st := range deployStatuses {
		if n, err := m.deps.DeployRecords.CountByStatus(ctx, st); err == nil {
			push("argus_deploy_records_total", "Deploy records", n, map[string]string{"status": st})
		}
	}

	// 通知发送
	for _, st := range []string{"success", "failed"} {
		if n, err := m.deps.SendLogs.CountByStatus(ctx, st); err == nil {
			push("argus_notify_sent_total", "Notification sends", n, map[string]string{"status": st})
		}
	}

	// Agent
	for _, st := range []string{"online", "offline", "disabled"} {
		if n, err := m.deps.Agents.CountByStatus(ctx, st); err == nil {
			push("argus_agents_total", "Agents", n, map[string]string{"status": st})
		}
	}

	// 用户
	if n, err := m.deps.Users.Count(ctx); err == nil {
		push("argus_users_total", "Users total", n, nil)
	}

	// 运行时长
	push("argus_uptime_seconds", "Process uptime in seconds", int64(time.Since(m.start).Seconds()), nil)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	for _, line := range out {
		fmt.Fprintln(w, line)
	}
}
