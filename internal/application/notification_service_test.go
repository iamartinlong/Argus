package application

import (
	"strings"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeNotifier 内存通知渠道
type fakeNotifier struct {
	mu     sync.Mutex
	sent   []domain.NotificationMessage
	fail   bool
	calls  int
}

func (f *fakeNotifier) Channel() string { return "webhook" }

func (f *fakeNotifier) Send(msg domain.NotificationMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return errTestFail
	}
	f.sent = append(f.sent, msg)
	return nil
}

var errTestFail = errTestFailType{}

type errTestFailType struct{}

func (errTestFailType) Error() string { return "test fail" }

type fakeNotifierRegistry struct {
	mu        sync.Mutex
	notifiers map[string]domain.MessageSender
}

func newFakeNotifierRegistry(notifiers ...domain.MessageSender) *fakeNotifierRegistry {
	r := &fakeNotifierRegistry{notifiers: map[string]domain.MessageSender{}}
	for _, n := range notifiers {
		r.notifiers[n.Channel()] = n
	}
	return r
}

func (r *fakeNotifierRegistry) Get(name string) (domain.MessageSender, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.notifiers[name]
	return n, ok
}

// fakeRuleStore 内存规则存储
type fakeRuleStore struct {
	mu    sync.Mutex
	rules map[string]*domain.NotificationRule
	order []string
}

func newFakeRuleStore() *fakeRuleStore {
	return &fakeRuleStore{rules: map[string]*domain.NotificationRule{}}
}

func (f *fakeRuleStore) Create(_ context.Context, r *domain.NotificationRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules[r.ID] = r
	f.order = append(f.order, r.ID)
	return nil
}

func (f *fakeRuleStore) GetByID(_ context.Context, id string) (*domain.NotificationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rules[id]; ok {
		return r, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeRuleStore) List(context.Context) ([]*domain.NotificationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.NotificationRule
	for _, id := range f.order {
		out = append(out, f.rules[id])
	}
	return out, nil
}

func (f *fakeRuleStore) ListByEvent(_ context.Context, event domain.NotificationEvent) ([]*domain.NotificationRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.NotificationRule
	for _, id := range f.order {
		if f.rules[id].Event == event {
			out = append(out, f.rules[id])
		}
	}
	return out, nil
}

func (f *fakeRuleStore) Update(_ context.Context, r *domain.NotificationRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules[r.ID] = r
	return nil
}

func (f *fakeRuleStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rules, id)
	return nil
}

// fakeNotifyLogStore 内存通知日志
type fakeNotifyLogStore struct {
	mu    sync.Mutex
	logs  map[string]bool
}

func newFakeNotifyLogStore() *fakeNotifyLogStore {
	return &fakeNotifyLogStore{logs: map[string]bool{}}
}

func (f *fakeNotifyLogStore) Append(_ context.Context, log *domain.NotificationLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs[log.CertID+"|"+log.RuleID+"|"+itoa(log.DaysBefore)] = true
	return nil
}

func (f *fakeNotifyLogStore) DeleteOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeNotifyLogStore) HasSent(_ context.Context, certID, ruleID string, daysBefore int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs[certID+"|"+ruleID+"|"+itoa(daysBefore)], nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// testWebhookChannelID 最近一次 newTestNotificationService 创建的种子渠道 ID
var testWebhookChannelID string

func newTestNotificationService() (*NotificationService, *fakeNotifier, *fakeRuleStore, *fakeNotifyLogStore) {
	notifier := &fakeNotifier{}
	channels := newFakeChannelStore()
	channelSvc := NewChannelService(channels, newFakeRuleStore(), &fakeAuditStore{},
		&fakeNotifierBuilder{notifier: notifier}, fakeCryptor{}, fixedClock{time.Now()})
	rules := newFakeRuleStore()
	logs := newFakeNotifyLogStore()
	svc := NewNotificationService(rules, logs, newFakeSendLogStore(), nil, &fakeAuditStore{}, channelSvc, fixedClock{time.Now()})
	svc.sleepBetween = func(time.Duration) {} // 测试不等待
	// 种子渠道：规则引用
	seed, err := channelSvc.Create(context.Background(), ChannelInput{
		Name: "webhook", Type: domain.ChannelWebhook,
		Config: map[string]any{"url": "http://example.com/hook"}, Enabled: boolPtr(true),
	})
	if err != nil {
		panic(err)
	}
	testWebhookChannelID = seed.ID
	return svc, notifier, rules, logs
}

func seedRule(t *testing.T, rules *fakeRuleStore, event domain.NotificationEvent, days []int) *domain.NotificationRule {
	t.Helper()
	rule := &domain.NotificationRule{
		ID:         "rule-1",
		Name:       "test",
		Event:      event,
		ChannelIDs: []string{testWebhookChannelID},
		DaysBefore: days,
		Enabled:    true,
		CreatedAt:  time.Now(),
	}
	if err := rules.Create(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	return rule
}

func testCert(cn string, notAfter time.Time) *domain.Certificate {
	return &domain.Certificate{
		ID:            "cert-1",
		PrimaryDomain: cn,
		Status:        domain.StatusValid,
		NotAfter:      notAfter,
	}
}

func TestRuleUpdate(t *testing.T) {
	svc, _, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventCertExpiring, []int{30, 7})

	// 更新：改事件/渠道/提前天数/停用
	updated, err := svc.UpdateRule(context.Background(), "rule-1", NotificationRuleInput{
		Name:       "改名",
		Event:      domain.EventRenewFailed,
		ChannelIDs: []string{testWebhookChannelID},
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "改名" || updated.Event != domain.EventRenewFailed ||
		len(updated.ChannelIDs) != 1 || updated.Enabled {
		t.Fatalf("unexpected updated rule: %+v", updated)
	}
	got, _ := rules.GetByID(context.Background(), "rule-1")
	if got.Name != "改名" || got.Event != domain.EventRenewFailed || got.Enabled {
		t.Fatalf("store not updated: %+v", got)
	}

	// 校验：无效事件 / 空渠道 / 渠道不存在 / 空名称
	for _, bad := range []NotificationRuleInput{
		{Name: "x", Event: "bogus", ChannelIDs: []string{testWebhookChannelID}},
		{Name: "x", Event: domain.EventRenewFailed},
		{Name: "x", Event: domain.EventRenewFailed, ChannelIDs: []string{"nope"}},
		{Name: "", Event: domain.EventRenewFailed, ChannelIDs: []string{testWebhookChannelID}},
	} {
		if _, err := svc.UpdateRule(context.Background(), "rule-1", bad); err == nil {
			t.Fatalf("expected error for %+v", bad)
		}
	}
	// 不存在 → 404
	if _, err := svc.UpdateRule(context.Background(), "no-such", NotificationRuleInput{
		Name: "x", Event: domain.EventRenewFailed, ChannelIDs: []string{testWebhookChannelID},
	}); err != domain.ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestNotifyExpiry(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventCertExpiring, []int{7})

	cert := testCert("expire.example.com", time.Now().Add(7*24*time.Hour))
	svc.NotifyExpiry(context.Background(), cert, 7)

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(notifier.sent))
	}
	if notifier.sent[0].Details["days_left"] != "7" {
		t.Fatalf("unexpected details: %v", notifier.sent[0].Details)
	}
}

func TestNotifyExpiryNoDuplicate(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventCertExpiring, []int{7})

	cert := testCert("dedupe.example.com", time.Now().Add(7*24*time.Hour))
	svc.NotifyExpiry(context.Background(), cert, 7)
	svc.NotifyExpiry(context.Background(), cert, 7) // 第二次不应再发

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected 1 message (dedupe), got %d", len(notifier.sent))
	}
}

func TestNotifyExpiryDayMismatch(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventCertExpiring, []int{7})

	cert := testCert("mismatch.example.com", time.Now().Add(3*24*time.Hour))
	svc.NotifyExpiry(context.Background(), cert, 3) // 规则只配了 7 天

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 0 {
		t.Fatalf("expected no message, got %d", len(notifier.sent))
	}
}

func TestSendHistoryRecordsSuccessAndFailure(t *testing.T) {	svc, _, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventCertExpiring, []int{7})
	cert := testCert("history.example.com", time.Now().Add(7*24*time.Hour))
	svc.NotifyExpiry(context.Background(), cert, 7)

	logs, err := svc.ListSendLogs(context.Background(), domain.NotificationSendLogQuery{}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 send log, got %d", len(logs))
	}
	l := logs[0]
	if l.Status != "success" || l.Event != domain.EventCertExpiring {
		t.Fatalf("unexpected send log: %+v", l)
	}
	if l.CertID != cert.ID || l.ChannelName == "" || l.RuleName != "test" {
		t.Fatalf("unexpected send log fields: %+v", l)
	}

	// 渠道停用 → 发送失败记录（detail 含原因）；新证书（不同 ID）不受防重影响
	disabled := boolPtr(false)
	if _, err := svc.channels.Update(context.Background(), testWebhookChannelID, ChannelInput{Enabled: disabled}); err != nil {
		t.Fatal(err)
	}
	cert2 := &domain.Certificate{
		ID: "cert-2", PrimaryDomain: "history2.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(7 * 24 * time.Hour),
	}
	svc.NotifyExpiry(context.Background(), cert2, 7)
	ch, _ := svc.channels.Get(context.Background(), testWebhookChannelID)
	t.Logf("channel enabled=%v", ch.Enabled)
	logs, _ = svc.ListSendLogs(context.Background(), domain.NotificationSendLogQuery{}, 50, 0)
	if len(logs) != 2 {
		t.Fatalf("expected 2 send logs, got %d", len(logs))
	}
	failed := logs[len(logs)-1]
	if failed.Status != "failed" || failed.Detail == "" {
		t.Fatalf("expected failed log with detail: %+v", failed)
	}
}

func TestNotifyEventRenewSuccess(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventRenewSuccess, nil)

	cert := testCert("renewed.example.com", time.Now().Add(90*24*time.Hour))
	cert.Status = domain.StatusValid
	svc.NotifyEvent(context.Background(), domain.EventRenewSuccess, cert, nil)

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(notifier.sent))
	}
}

func TestNotifyRetryOnFailure(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	notifier.fail = true
	seedRule(t, rules, domain.EventRenewFailed, nil)

	cert := testCert("fail.example.com", time.Now().Add(10*24*time.Hour))
	svc.NotifyEvent(context.Background(), domain.EventRenewFailed, cert, nil)

	// 发送失败但不应 panic；重试次数等于调用次数
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if notifier.calls == 0 {
		t.Fatal("expected send attempts")
	}
}

// fakeTemplateStore 内存模板
type fakeTemplateStore struct {
	templates map[domain.NotificationEvent]*domain.NotificationTemplate
}

func (f *fakeTemplateStore) Upsert(_ context.Context, t *domain.NotificationTemplate) error {
	f.templates[t.Event] = t
	return nil
}

func (f *fakeTemplateStore) Get(_ context.Context, event domain.NotificationEvent) (*domain.NotificationTemplate, error) {
	return f.templates[event], nil
}

func (f *fakeTemplateStore) List(context.Context) ([]*domain.NotificationTemplate, error) {
	var out []*domain.NotificationTemplate
	for _, t := range f.templates {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeTemplateStore) Delete(_ context.Context, event domain.NotificationEvent) error {
	delete(f.templates, event)
	return nil
}

func TestTemplateRendering(t *testing.T) {
	// 自定义模板 + 变量替换
	store := &fakeTemplateStore{templates: map[domain.NotificationEvent]*domain.NotificationTemplate{}}
	if err := store.Upsert(context.Background(), &domain.NotificationTemplate{
		Event: domain.EventCertExpiring,
		Title: "【到期】{domain}",
		Body:  "证书 {domain} 还剩 {days_left} 天（{expires}）",
	}); err != nil {
		t.Fatal(err)
	}
	svc := &NotificationService{templates: store, clock: fixedClock{time.Now()}}
	details := map[string]string{"domain": "a.example.com", "days_left": "7", "expires": "2026-12-01 00:00"}
	title, body := svc.render(context.Background(), domain.EventCertExpiring, testCert("a.example.com", time.Now()), details)
	if title != "【到期】a.example.com" {
		t.Fatalf("title: %q", title)
	}
	if body != "证书 a.example.com 还剩 7 天（2026-12-01 00:00）" {
		t.Fatalf("body: %q", body)
	}

	// 未配置 → 默认文案
	svc2 := &NotificationService{templates: store, clock: fixedClock{time.Now()}}
	title2, _ := svc2.render(context.Background(), domain.EventRenewSuccess, testCert("b.example.com", time.Now()), details)
	if title2 != "证书续期成功：b.example.com" {
		t.Fatalf("default title: %q", title2)
	}
}

func TestSaveTemplateEmptyDeletes(t *testing.T) {
	store := &fakeTemplateStore{templates: map[domain.NotificationEvent]*domain.NotificationTemplate{}}
	svc := NewNotificationService(newFakeRuleStore(), newFakeNotifyLogStore(), newFakeSendLogStore(), store,
		&fakeAuditStore{}, &ChannelService{}, fixedClock{time.Now()})
	ctx := context.Background()
	if err := svc.SaveTemplate(ctx, &domain.NotificationTemplate{Event: domain.EventCertExpiring, Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(store.templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(store.templates))
	}
	// 空模板 = 删除
	if err := svc.SaveTemplate(ctx, &domain.NotificationTemplate{Event: domain.EventCertExpiring}); err != nil {
		t.Fatal(err)
	}
	if len(store.templates) != 0 {
		t.Fatalf("expected template deleted, got %d", len(store.templates))
	}
	// 非法事件拒绝
	if err := svc.SaveTemplate(ctx, &domain.NotificationTemplate{Event: "bogus", Title: "x"}); err == nil {
		t.Fatal("expected invalid event rejected")
	}
	// 标题含换行拒绝（防 SMTP 头注入）
	if err := svc.SaveTemplate(ctx, &domain.NotificationTemplate{
		Event: domain.EventCertExpiring, Title: "x\r\nBcc: evil@example.com",
	}); err == nil {
		t.Fatal("expected newline in title rejected")
	}
	// 超长拒绝
	long := strings.Repeat("a", 1001)
	if err := svc.SaveTemplate(ctx, &domain.NotificationTemplate{
		Event: domain.EventCertExpiring, Title: long,
	}); err == nil {
		t.Fatal("expected overlong title rejected")
	}
}

func TestDisabledRuleSkipped(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	rule := seedRule(t, rules, domain.EventCertExpiring, []int{7})
	rule.Enabled = false

	cert := testCert("disabled.example.com", time.Now().Add(7*24*time.Hour))
	svc.NotifyExpiry(context.Background(), cert, 7)

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 0 {
		t.Fatalf("disabled rule should not send, got %d", len(notifier.sent))
	}
}

func TestRuleCRUD(t *testing.T) {
	svc, _, _, _ := newTestNotificationService()
	ctx := context.Background()

	rule, err := svc.CreateRule(ctx, NotificationRuleInput{
		Name: "到期提醒", Event: domain.EventCertExpiring,
		Channels: []string{"webhook"}, DaysBefore: []int{30, 7}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rule.Event != domain.EventCertExpiring || len(rule.DaysBefore) != 2 {
		t.Fatalf("unexpected rule: %+v", rule)
	}
	// 无效渠道
	if _, err := svc.CreateRule(ctx, NotificationRuleInput{
		Name: "bad", Event: domain.EventCertExpiring, Channels: []string{"sms"},
	}); err == nil {
		t.Fatal("expected error for invalid channel")
	}
	// 空渠道
	if _, err := svc.CreateRule(ctx, NotificationRuleInput{
		Name: "empty", Event: domain.EventCertExpiring, Channels: nil,
	}); err == nil {
		t.Fatal("expected error for empty channels")
	}
	if err := svc.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// TestEventSubscriptionFilter 证书级通知事件勾选：未勾选的事件不发送
func TestEventSubscriptionFilter(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventIssueSuccess, nil)

	// 证书未订阅 issue_success → 不发送
	cert := testCert("filter.example.com", time.Now().Add(90*24*time.Hour))
	cert.NotifyEvents = []domain.NotificationEvent{domain.EventDeploySuccess}
	svc.NotifyEvent(context.Background(), domain.EventIssueSuccess, cert, nil)
	notifier.mu.Lock()
	if len(notifier.sent) != 0 {
		t.Fatalf("unsubscribed event should not send, got %d", len(notifier.sent))
	}
	notifier.mu.Unlock()

	// 订阅后 → 发送
	cert2 := testCert("filter2.example.com", time.Now().Add(90*24*time.Hour))
	cert2.NotifyEvents = []domain.NotificationEvent{domain.EventIssueSuccess}
	svc.NotifyEvent(context.Background(), domain.EventIssueSuccess, cert2, nil)
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(notifier.sent))
	}
}

// TestNotifyExpirySubscriptionFilter 到期提醒同样受证书级勾选过滤
func TestNotifyExpirySubscriptionFilter(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	seedRule(t, rules, domain.EventCertExpiring, []int{7})

	cert := testCert("expire-filter.example.com", time.Now().Add(7*24*time.Hour))
	cert.NotifyEvents = []domain.NotificationEvent{domain.EventIssueSuccess} // 未勾选到期提醒
	svc.NotifyExpiry(context.Background(), cert, 7)

	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 0 {
		t.Fatalf("unsubscribed expiry should not send, got %d", len(notifier.sent))
	}
}

// TestDefaultTemplateNewEvents 新事件默认文案
func TestDefaultTemplateNewEvents(t *testing.T) {
	details := map[string]string{"target": "web-1", "error": "timeout", "expires": "2026-12-01"}
	cert := testCert("a.example.com", time.Now())
	cert.NotAfter = time.Now().Add(90 * 24 * time.Hour)
	cert.LastError = "timeout"

	cases := []struct {
		event       domain.NotificationEvent
		titlePrefix string
	}{
		{domain.EventIssueSuccess, "证书签发成功"},
		{domain.EventIssueFailed, "证书签发失败"},
		{domain.EventDeploySuccess, "证书部署成功"},
		{domain.EventDeployFailed, "证书部署失败"},
	}
	for _, c := range cases {
		title, _ := defaultTemplate(c.event, cert, details)
		if !strings.HasPrefix(title, c.titlePrefix) {
			t.Fatalf("event %s: title %q, want prefix %q", c.event, title, c.titlePrefix)
		}
	}
	// 部署正文直接取 details 中的 target/error 值（默认文案不含未渲染占位符）
	_, body := defaultTemplate(domain.EventDeploySuccess, cert, details)
	if !strings.Contains(body, "web-1") {
		t.Fatalf("deploy success body should contain target value: %q", body)
	}
	_, bodyF := defaultTemplate(domain.EventDeployFailed, cert, details)
	if !strings.Contains(bodyF, "timeout") || !strings.Contains(bodyF, "web-1") {
		t.Fatalf("deploy failed body should contain target+error values: %q", bodyF)
	}
	if strings.Contains(bodyF, "{target}") || strings.Contains(bodyF, "{error}") {
		t.Fatalf("deploy failed body should not contain unreplaced placeholders: %q", bodyF)
	}
}

// TestNotifyEventBus 事件总线订阅：签发成功/失败、部署成功/失败触发通知
func TestNotifyEventBus(t *testing.T) {
	svc, notifier, rules, _ := newTestNotificationService()
	bus := NewEventBus()
	svc.AttachEventBus(bus)

	cert := testCert("bus.example.com", time.Now().Add(90*24*time.Hour))
	cert.NotifyEvents = []domain.NotificationEvent{
		domain.EventIssueSuccess, domain.EventIssueFailed,
		domain.EventDeploySuccess, domain.EventDeployFailed,
	}
	cert.LastError = "boom"

	seedRuleID(t, rules, "r-issue-success", domain.EventIssueSuccess, nil)
	bus.Publish(EventCertificateIssued, cert)
	waitSent(t, notifier, 1, "issue success")

	seedRuleID(t, rules, "r-issue-failed", domain.EventIssueFailed, nil)
	bus.Publish(EventCertificateIssueFail, cert)
	waitSent(t, notifier, 2, "issue failed")

	seedRuleID(t, rules, "r-deploy-success", domain.EventDeploySuccess, nil)
	bus.Publish(EventDeploySuccess, &DeployEvent{Cert: cert, TargetName: "web-1"})
	waitSent(t, notifier, 3, "deploy success")
	notifier.mu.Lock()
	if notifier.sent[2].Details["target"] != "web-1" {
		t.Fatalf("deploy success details: %v", notifier.sent[2].Details)
	}
	notifier.mu.Unlock()

	seedRuleID(t, rules, "r-deploy-failed", domain.EventDeployFailed, nil)
	bus.Publish(EventDeployFailed, &DeployEvent{Cert: cert, TargetName: "web-1", Error: "boom"})
	waitSent(t, notifier, 4, "deploy failed")
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if notifier.sent[3].Details["error"] != "boom" {
		t.Fatalf("deploy failed details: %v", notifier.sent[3].Details)
	}
}

// waitSent 等待异步通知发送完成（事件订阅改为后台 goroutine 发送）
func waitSent(t *testing.T, notifier *fakeNotifier, want int, label string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		notifier.mu.Lock()
		n := len(notifier.sent)
		notifier.mu.Unlock()
		if n == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	t.Fatalf("%s: expected %d notifications, got %d", label, want, len(notifier.sent))
}

// seedRuleID 以指定 ID 创建规则（供同测试多次 seed 使用）
func seedRuleID(t *testing.T, rules *fakeRuleStore, id string, event domain.NotificationEvent, days []int) {
	t.Helper()
	rule := &domain.NotificationRule{
		ID:         id,
		Name:       "test-" + id,
		Event:      event,
		ChannelIDs: []string{testWebhookChannelID},
		DaysBefore: days,
		Enabled:    true,
		CreatedAt:  time.Now(),
	}
	if err := rules.Create(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
}

// TestNewRuleEventsValid 4 种新事件可通过规则创建
func TestNewRuleEventsValid(t *testing.T) {
	svc, _, _, _ := newTestNotificationService()
	ctx := context.Background()
	for _, e := range []domain.NotificationEvent{
		domain.EventIssueSuccess, domain.EventIssueFailed,
		domain.EventDeploySuccess, domain.EventDeployFailed,
	} {
		if _, err := svc.CreateRule(ctx, NotificationRuleInput{
			Name: string(e), Event: e, Channels: []string{"webhook"},
		}); err != nil {
			t.Fatalf("create rule for %s: %v", e, err)
		}
	}
}
