package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// fakeCertOps 内存证书操作
type fakeCertOps struct {
	mu          sync.Mutex
	certs       []*domain.Certificate
	renewed     []string
	renewErr    error
	renewCalls  int
	statusFlips []string // 记录状态落库（certID=status）
}

func (f *fakeCertOps) UpdateStatus(_ context.Context, certID string, status domain.CertificateStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusFlips = append(f.statusFlips, certID+"="+string(status))
	for _, c := range f.certs {
		if c.ID == certID {
			c.Status = status
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeCertOps) List(context.Context) ([]*domain.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Certificate, len(f.certs))
	copy(out, f.certs)
	return out, nil
}

func (f *fakeCertOps) Renew(_ context.Context, certID string, _ map[string]string, _ string) (*domain.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewCalls++
	f.renewed = append(f.renewed, certID)
	if f.renewErr != nil {
		return nil, f.renewErr
	}
	for _, c := range f.certs {
		if c.ID == certID {
			c.Status = domain.StatusValid
			c.NotAfter = time.Now().Add(90 * 24 * time.Hour)
			return c, nil
		}
	}
	return nil, domain.ErrNotFound
}

// fakeTaskRunner 内存任务日志
type fakeTaskRunner struct {
	mu   sync.Mutex
	logs []domain.TaskType
}

func (f *fakeTaskRunner) RunTask(_ context.Context, typ domain.TaskType, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	f.logs = append(f.logs, typ)
	f.mu.Unlock()
	return fn(context.Background())
}

func (f *fakeTaskRunner) RunTaskWithSummary(_ context.Context, typ domain.TaskType, fn func(ctx context.Context) (string, error)) error {
	f.mu.Lock()
	f.logs = append(f.logs, typ)
	f.mu.Unlock()
	_, err := fn(context.Background())
	return err
}

func newTestScheduler(certs []*domain.Certificate) (*SchedulerService, *fakeCertOps, *fakeNotifier, *fakeRuleStore) {
	ops := &fakeCertOps{certs: certs}
	notifier := &fakeNotifier{}
	rules := newFakeRuleStore()
	channelSvc := NewChannelService(newFakeChannelStore(), newFakeRuleStore(), &fakeAuditStore{},
		&fakeNotifierBuilder{notifier: notifier}, fakeCryptor{}, fixedClock{time.Now()})
	seed, err := channelSvc.Create(context.Background(), ChannelInput{
		Name: "webhook", Type: domain.ChannelWebhook,
		Config: map[string]any{"url": "http://example.com/hook"}, Enabled: boolPtr(true),
	})
	if err != nil {
		panic(err)
	}
	testWebhookChannelID = seed.ID
	svc := NewNotificationService(rules, newFakeNotifyLogStore(), newFakeSendLogStore(), nil, &fakeAuditStore{}, channelSvc, fixedClock{time.Now()})
	svc.sleepBetween = func(time.Duration) {}
	sched := NewSchedulerService(ops, svc, &fakeTaskRunner{}, fixedClock{time.Now()}, 30, []int{7})
	return sched, ops, notifier, rules
}

func TestScanAutoRenewsExpiringCert(t *testing.T) {
	expiring := &domain.Certificate{
		ID: "cert-expiring", PrimaryDomain: "expiring.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(10 * 24 * time.Hour),
	}
	sched, ops, notifier, rules := newTestScheduler([]*domain.Certificate{expiring})
	seedRule(t, rules, domain.EventRenewSuccess, nil)

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ops.renewed) != 1 || ops.renewed[0] != "cert-expiring" {
		t.Fatalf("expected expiring cert renewed, got %v", ops.renewed)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected renew success notification, got %d", len(notifier.sent))
	}
}

func TestScanSkipsLongLivedCert(t *testing.T) {
	valid := &domain.Certificate{
		ID: "cert-valid", PrimaryDomain: "valid.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(200 * 24 * time.Hour),
	}
	sched, ops, _, _ := newTestScheduler([]*domain.Certificate{valid})

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ops.renewed) != 0 {
		t.Fatalf("long-lived cert should not be renewed, got %v", ops.renewed)
	}
}

func TestScanRenewFailureNotifies(t *testing.T) {
	expiring := &domain.Certificate{
		ID: "cert-fail", PrimaryDomain: "fail.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(5 * 24 * time.Hour),
	}
	sched, ops, notifier, rules := newTestScheduler([]*domain.Certificate{expiring})
	ops.renewErr = errors.New("dns propagation timeout")
	seedRule(t, rules, domain.EventRenewFailed, nil)

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected renew failure notification, got %d", len(notifier.sent))
	}
}

// TestScanPersistsExpiredStatus 过渡状态（valid→expired）应落库，保证统计口径
func TestScanPersistsExpiredStatus(t *testing.T) {
	expired := &domain.Certificate{
		ID: "cert-expired", PrimaryDomain: "expired.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(-24 * time.Hour),
	}
	sched, ops, _, _ := newTestScheduler([]*domain.Certificate{expired})

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()
	if len(ops.statusFlips) != 1 || ops.statusFlips[0] != "cert-expired=expired" {
		t.Fatalf("expected expired status persisted, got %v", ops.statusFlips)
	}
}

// TestScanSingleFlight 已有扫描在执行时，并发触发立即返回 ErrScanInProgress
func TestScanSingleFlight(t *testing.T) {
	valid := &domain.Certificate{
		ID: "cert-busy", PrimaryDomain: "busy.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(200 * 24 * time.Hour),
	}
	sched, _, _, _ := newTestScheduler([]*domain.Certificate{valid})

	sched.scanMu.Lock() // 模拟一轮扫描进行中
	_, err := sched.RunScan(context.Background())
	sched.scanMu.Unlock()
	if !errors.Is(err, ErrScanInProgress) {
		t.Fatalf("expected ErrScanInProgress, got %v", err)
	}
	// 释放后应能正常执行
	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatalf("scan after release: %v", err)
	}
}

func TestScanNotifiesExpiry(t *testing.T) {
	soon := &domain.Certificate{
		ID: "cert-soon", PrimaryDomain: "soon.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(7 * 24 * time.Hour),
	}
	sched, ops, notifier, rules := newTestScheduler([]*domain.Certificate{soon})
	// 续期失败（模拟渠道不可用），应触发 renew_failed + 到期提醒两条通知
	ops.renewErr = errors.New("acme down")
	seedRule(t, rules, domain.EventCertExpiring, []int{7})
	seedRule(t, rules, domain.EventRenewFailed, nil)

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	// renew 失败通知 1 条 + 到期提醒 1 条
	if len(notifier.sent) != 2 {
		t.Fatalf("expected 2 notifications (renew_failed + expiry), got %d: %+v", len(notifier.sent), notifier.sent)
	}
}

// TestScanRemindsLateEntryWithTighterWindow 服务停机跨过提醒日后，下一轮用"最紧"的
// 适用提醒补发（10 天剩余 → 命中 14 天档，而不是漏发）
func TestScanRemindsLateEntryWithTighterWindow(t *testing.T) {
	late := &domain.Certificate{
		ID: "cert-late", PrimaryDomain: "late.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(10 * 24 * time.Hour),
	}
	sched, _, notifier, rules := newTestScheduler([]*domain.Certificate{late})
	// 关闭自动续期（renewDays=5 < 剩余 10 天），只走到期提醒分支
	sched.renewDays = 5
	sched.expiryDays = []int{30, 14, 7, 1}
	seedRule(t, rules, domain.EventCertExpiring, []int{14})

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("expected 1 expiry reminder, got %d: %+v", len(notifier.sent), notifier.sent)
	}
	if got := notifier.sent[0].Details["days_left"]; got != "14" {
		t.Fatalf("expected reminder for 14-day tier, got days_left=%q", got)
	}
}

// TestScanCSRCertSilentlySkipped CSR 证书（无私钥）续期失败不产生通知噪音
func TestScanCSRCertSilentlySkipped(t *testing.T) {
	csrCert := &domain.Certificate{
		ID: "cert-csr", PrimaryDomain: "csr.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(5 * 24 * time.Hour),
	}
	sched, ops, notifier, rules := newTestScheduler([]*domain.Certificate{csrCert})
	ops.renewErr = ErrCSRCertNotRenewable
	seedRule(t, rules, domain.EventRenewFailed, nil)

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 0 {
		t.Fatalf("expected no renew_failed notification for CSR cert, got %d", len(notifier.sent))
	}
}

// TestScanImportedCertSilentlySkipped 导入的证书（无渠道）不参与自动续期，避免失败通知噪音
func TestScanImportedCertSilentlySkipped(t *testing.T) {
	imported := &domain.Certificate{
		ID: "cert-imported", PrimaryDomain: "imp.example.com", Provider: "import",
		Status: domain.StatusValid, NotAfter: time.Now().Add(5 * 24 * time.Hour),
	}
	sched, ops, notifier, rules := newTestScheduler([]*domain.Certificate{imported})
	ops.renewErr = ErrCSRCertNotRenewable
	seedRule(t, rules, domain.EventRenewFailed, nil)

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	notified := len(notifier.sent)
	notifier.mu.Unlock()
	if notified != 0 {
		t.Fatalf("expected no renew_failed notification for imported cert, got %d", notified)
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()
	if ops.renewCalls != 0 {
		t.Fatalf("expected no renew attempt for imported cert, got %d", ops.renewCalls)
	}
}

func TestExpiryNotifyWorksForFailedStatus(t *testing.T) {
	// 审计修复：续期失败（status=failed）但证书未到期，到期提醒仍应触发
	soon := &domain.Certificate{
		ID: "cert-failed", PrimaryDomain: "failed.example.com",
		Status: domain.StatusFailed, NotAfter: time.Now().Add(7 * 24 * time.Hour),
	}
	// failed 状态不会触发自动续期分支，直接走提醒
	sched, _, notifier, rules := newTestScheduler([]*domain.Certificate{soon})
	seedRule(t, rules, domain.EventCertExpiring, []int{7})

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.sent) != 1 {
		t.Fatalf("failed-status cert should still get expiry notice, got %d", len(notifier.sent))
	}
}

func TestRenewTaskLoggedIndividually(t *testing.T) {
	expiring := &domain.Certificate{
		ID: "cert-log", PrimaryDomain: "log.example.com",
		Status: domain.StatusValid, NotAfter: time.Now().Add(10 * 24 * time.Hour),
	}
	ops := &fakeCertOps{certs: []*domain.Certificate{expiring}}
	tasks := &fakeTaskRunner{}
	notifier := &fakeNotifier{}
	rules := newFakeRuleStore()
	channelSvc := NewChannelService(newFakeChannelStore(), newFakeRuleStore(), &fakeAuditStore{},
		&fakeNotifierBuilder{notifier: notifier}, fakeCryptor{}, fixedClock{time.Now()})
	seed, err := channelSvc.Create(context.Background(), ChannelInput{
		Name: "webhook", Type: domain.ChannelWebhook,
		Config: map[string]any{"url": "http://example.com/hook"}, Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	testWebhookChannelID = seed.ID
	svc := NewNotificationService(rules, newFakeNotifyLogStore(), newFakeSendLogStore(), nil, &fakeAuditStore{}, channelSvc, fixedClock{time.Now()})
	svc.sleepBetween = func(time.Duration) {}
	sched := NewSchedulerService(ops, svc, tasks, fixedClock{time.Now()}, 30, []int{7})

	if _, err := sched.RunScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	var hasAutoRenew bool
	for _, typ := range tasks.logs {
		if typ == domain.TaskAutoRenew {
			hasAutoRenew = true
		}
	}
	if !hasAutoRenew {
		t.Fatalf("expected auto_renew task logged, got %v", tasks.logs)
	}
}
