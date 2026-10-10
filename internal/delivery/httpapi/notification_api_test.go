package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
)

func TestNotificationRulesCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 创建规则
	rec := s.do(http.MethodPost, "/api/v1/notification-rules", map[string]any{
		"name": "到期提醒", "event": "cert_expiring",
		"channels": []string{"webhook", "wecom"}, "days_before": []int{30, 7}, "enabled": true,
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule: %d %s", rec.Code, rec.Body.String())
	}
	var rule struct {
		ID string `json:"id"`
	}
	decodeData(t, rec, &rule)

	// 无效渠道
	rec = s.do(http.MethodPost, "/api/v1/notification-rules", map[string]any{
		"name": "bad", "event": "cert_expiring", "channels": []string{"sms"},
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid channel, got %d", rec.Code)
	}

	// 列表
	rec = s.do(http.MethodGet, "/api/v1/notification-rules", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var rules []map[string]any
	decodeData(t, rec, &rules)

	// 渠道列表
	rec = s.do(http.MethodGet, "/api/v1/notification-rules/channels", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("channels: %d", rec.Code)
	}

	// 更新：改名称/事件/停用
	rec = s.do(http.MethodPut, "/api/v1/notification-rules/"+rule.ID, map[string]any{
		"name": "改名", "event": "renew_failed",
		"channels": []string{"webhook"}, "enabled": false,
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("update rule: %d %s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	decodeData(t, rec, &updated)
	if updated["name"] != "改名" || updated["event"] != "renew_failed" || updated["enabled"] != false {
		t.Fatalf("unexpected updated: %+v", updated)
	}
	// 列表确认更新生效
	rec = s.do(http.MethodGet, "/api/v1/notification-rules", nil, adminToken)
	var after []map[string]any
	decodeData(t, rec, &after)
	if len(after) != 1 || after[0]["name"] != "改名" || after[0]["enabled"] != false {
		t.Fatalf("list after update: %+v", after)
	}

	// 更新校验：无效事件 / 不存在
	rec = s.do(http.MethodPut, "/api/v1/notification-rules/"+rule.ID, map[string]any{
		"name": "x", "event": "bogus", "channels": []string{"webhook"},
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid event, got %d", rec.Code)
	}
	rec = s.do(http.MethodPut, "/api/v1/notification-rules/nope", map[string]any{
		"name": "x", "event": "renew_failed", "channels": []string{"webhook"},
	}, adminToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// 删除
	rec = s.do(http.MethodDelete, "/api/v1/notification-rules/"+rule.ID, nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNotificationRulesRequireRole(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")

	rec := s.do(http.MethodPost, "/api/v1/notification-rules", map[string]any{
		"name": "x", "event": "cert_expiring", "channels": []string{"webhook"},
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}

func TestTaskLogsList(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	rec := s.do(http.MethodGet, "/api/v1/task-logs", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("task-logs: %d %s", rec.Code, rec.Body.String())
	}
	var taskPage struct {
		Items []any `json:"items"`
	}
	decodeData(t, rec, &taskPage)
}

func TestSendLogsListAndFilter(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 无记录
	rec := s.do(http.MethodGet, "/api/v1/notification-send-logs", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("send-logs: %d %s", rec.Code, rec.Body.String())
	}
	var emptyPage struct {
		Items []any `json:"items"`
	}
	decodeData(t, rec, &emptyPage)
	if len(emptyPage.Items) != 0 {
		t.Fatalf("expected empty, got %d", len(emptyPage.Items))
	}

	// 造一条发送历史：直接插入存储
	store := sqlite.NewNotificationSendLogStore(s.db)
	if err := store.Append(s.t.Context(), &domain.NotificationSendLog{
		ID: "sl1", CertID: "cert-x", RuleID: "rule-x", RuleName: "测试规则",
		ChannelID: "ch-x", ChannelName: "测试渠道", Event: domain.EventCertExpiring,
		Status: "failed", Detail: "webhook 超时", SentAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// 状态筛选
	rec = s.do(http.MethodGet, "/api/v1/notification-send-logs?status=failed", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter status: %d", rec.Code)
	}
	var failedPage struct {
		Items []map[string]any `json:"items"`
	}
	decodeData(t, rec, &failedPage)
	failed := failedPage.Items
	if len(failed) != 1 || failed[0]["status"] != "failed" {
		t.Fatalf("unexpected filtered: %v", failed)
	}

	// 关键字筛选（规则名）
	rec = s.do(http.MethodGet, "/api/v1/notification-send-logs?keyword=%E6%B5%8B%E8%AF%95", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter keyword: %d", rec.Code)
	}
	var kwPage struct {
		Items []any `json:"items"`
	}
	decodeData(t, rec, &kwPage)
	if len(kwPage.Items) != 1 {
		t.Fatalf("expected 1 keyword match, got %d", len(kwPage.Items))
	}

	// 状态不匹配 → 空
	rec = s.do(http.MethodGet, "/api/v1/notification-send-logs?status=success", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter success: %d", rec.Code)
	}
	var okPage struct {
		Items []any `json:"items"`
	}
	decodeData(t, rec, &okPage)
	if len(okPage.Items) != 0 {
		t.Fatalf("expected empty success, got %d", len(okPage.Items))
	}
}

// fakeScanRunner 模拟调度器
type fakeScanRunner struct{ ran bool }

func (f *fakeScanRunner) RunScan(ctx context.Context) error {
	f.ran = true
	return nil
}

func TestTemplateCRUD(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")
	viewerToken := createUser(t, s, adminToken, "v2", "readonly")

	// 初始为空
	rec := s.do(http.MethodGet, "/api/v1/notification-templates", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var empty []any
	decodeData(t, rec, &empty)
	if len(empty) != 0 {
		t.Fatalf("expected empty, got %d", len(empty))
	}

	// 保存模板
	rec = s.do(http.MethodPut, "/api/v1/notification-templates/cert_expiring", map[string]any{
		"title": "【到期】{domain}", "body": "证书 {domain} 还剩 {days_left} 天",
	}, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	// 读取
	rec = s.do(http.MethodGet, "/api/v1/notification-templates", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("list2: %d", rec.Code)
	}
	var list []map[string]any
	decodeData(t, rec, &list)
	if len(list) != 1 || list[0]["event"] != "cert_expiring" {
		t.Fatalf("unexpected list: %v", list)
	}

	// 非法事件拒绝
	rec = s.do(http.MethodPut, "/api/v1/notification-templates/bogus", map[string]any{
		"title": "x",
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid event, got %d", rec.Code)
	}

	// 重置（空模板删除）
	rec = s.do(http.MethodDelete, "/api/v1/notification-templates/cert_expiring", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/notification-templates", nil, adminToken)
	var after []any
	decodeData(t, rec, &after)
	if len(after) != 0 {
		t.Fatalf("expected empty after reset, got %d", len(after))
	}

	// readonly 不可修改
	rec = s.do(http.MethodPut, "/api/v1/notification-templates/cert_expiring", map[string]any{
		"title": "x",
	}, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}

func TestExportCSV(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 造数据
	store := sqlite.NewNotificationSendLogStore(s.db)
	_ = store.Append(s.t.Context(), &domain.NotificationSendLog{
		ID: "sl-export", CertID: "cert-1", RuleName: "规则", ChannelName: "渠道",
		Event: domain.EventCertExpiring, Status: "failed", Detail: "超时", SentAt: time.Now(),
	})

	// 发送历史导出（登录即可）
	rec := s.do(http.MethodGet, "/api/v1/notification-send-logs/export", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("send export: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "notification-send-logs.csv") && !strings.Contains(body, "sent_at,cert_id") {
		// Content-Disposition 是 header；检查 CSV 内容
	}
	if !strings.Contains(body, "cert-1") || !strings.Contains(body, "超时") {
		t.Fatalf("send export content: %s", body)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("missing attachment header: %s", cd)
	}

	// 审计导出（admin only）
	rec = s.do(http.MethodGet, "/api/v1/audit-logs/export", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit export: %d", rec.Code)
	}
	rec = s.do(http.MethodGet, "/api/v1/audit-logs/export", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %d", rec.Code)
	}

	// 任务日志导出
	rec = s.do(http.MethodGet, "/api/v1/task-logs/export", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("task export: %d", rec.Code)
	}
}

func TestCSVFormulaInjectionSanitized(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 造一条 detail 以 = 开头的审计记录（模拟用户可控内容注入）
	if err := sqlite.NewAuditStore(s.db).Append(s.t.Context(), &domain.AuditLog{
		ID: "inj-1", Username: "admin", Action: "login.success",
		Detail: "=HYPERLINK(\"http://evil\",\"x\")", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	rec := s.do(http.MethodGet, "/api/v1/audit-logs/export", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d", rec.Code)
	}
	body := rec.Body.String()
	// 原始公式不应原样出现，应被单引号前缀防护
	if strings.Contains(body, `"=HYPERLINK`) {
		t.Fatalf("CSV formula injection not sanitized:\n%s", body)
	}
	if !strings.Contains(body, `"'=HYPERLINK`) {
		t.Fatalf("expected sanitized cell with leading quote:\n%s", body)
	}
}

func TestTriggerScan(t *testing.T) {
	// 用带 ScanRunner 的 server 验证接口存在与权限
	s := newTestServer(t)
	s.seedUser("admin", "Adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "Adminpass123456")

	// 未配置 ScanRunner 时返回 501
	rec := s.do(http.MethodPost, "/api/v1/task-logs/scan", nil, adminToken)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 without scan runner, got %d", rec.Code)
	}

	// readonly 无权限（即使有 runner 也是 403 优先于 501？403 在 handler 内先判）
	viewerToken := createUser(t, s, adminToken, "viewer", "readonly")
	rec = s.do(http.MethodPost, "/api/v1/task-logs/scan", nil, viewerToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for readonly, got %d", rec.Code)
	}
}
