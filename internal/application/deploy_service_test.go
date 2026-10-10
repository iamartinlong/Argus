package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

// fakeDeployAdapter 内存部署通道
type fakeDeployAdapter struct {
	name      string
	mu        sync.Mutex
	deploys   int
	fail      bool
	configs   []map[string]any
	payloads  []deploy.Payload
}

func (f *fakeDeployAdapter) Type() string { return f.name }

func (f *fakeDeployAdapter) IsAsync() bool { return false }

func (f *fakeDeployAdapter) Deploy(_ context.Context, payload deploy.Payload, config map[string]any) (*deploy.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deploys++
	f.payloads = append(f.payloads, payload)
	f.configs = append(f.configs, config)
	if f.fail {
		return nil, errors.New("adapter failure")
	}
	return &deploy.Result{Checksum: "abc123", Message: "deployed"}, nil
}

func (f *fakeDeployAdapter) Test(context.Context, map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("connection failed")
	}
	return nil
}

type fakeDeployRegistry struct {
	adapters map[string]deploy.Adapter
}

func newFakeDeployRegistry(adapters ...deploy.Adapter) *fakeDeployRegistry {
	r := &fakeDeployRegistry{adapters: map[string]deploy.Adapter{}}
	for _, a := range adapters {
		r.adapters[a.Type()] = a
	}
	return r
}

func (r *fakeDeployRegistry) Get(t string) (deploy.Adapter, bool) {
	a, ok := r.adapters[t]
	return a, ok
}

// fakeDeployTargetStore 内存目标存储
type fakeDeployTargetStore struct {
	mu      sync.Mutex
	targets map[string]*domain.DeployTarget
}

func newFakeDeployTargetStore() *fakeDeployTargetStore {
	return &fakeDeployTargetStore{targets: map[string]*domain.DeployTarget{}}
}

func (f *fakeDeployTargetStore) Create(_ context.Context, t *domain.DeployTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets[t.ID] = t
	return nil
}

func (f *fakeDeployTargetStore) GetByID(_ context.Context, id string) (*domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.targets[id]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeDeployTargetStore) List(context.Context) ([]*domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.DeployTarget
	for _, t := range f.targets {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeDeployTargetStore) Update(_ context.Context, t *domain.DeployTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets[t.ID] = t
	return nil
}

func (f *fakeDeployTargetStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.targets, id)
	return nil
}

// fakeDeployRecordStore 内存部署记录
type fakeDeployRecordStore struct {
	mu      sync.Mutex
	records []*domain.DeployRecord
}

func newFakeDeployRecordStore() *fakeDeployRecordStore {
	return &fakeDeployRecordStore{}
}

func (f *fakeDeployRecordStore) Create(_ context.Context, r *domain.DeployRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, r)
	return nil
}

func (f *fakeDeployRecordStore) GetByID(_ context.Context, id string) (*domain.DeployRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeDeployRecordStore) ListByCertificate(_ context.Context, certID string) ([]*domain.DeployRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.DeployRecord
	for _, r := range f.records {
		if r.CertificateID == certID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeDeployRecordStore) Update(_ context.Context, r *domain.DeployRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, x := range f.records {
		if x.ID == r.ID {
			f.records[i] = r
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeDeployRecordStore) UpdateResult(_ context.Context, id string, status domain.DeployRecordStatus, checksum, message string, deployedAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id {
			r.Status = status
			r.Checksum = checksum
			r.Message = message
			r.DeployedAt = deployedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeDeployRecordStore) ListFailedPending(context.Context) ([]*domain.DeployRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.DeployRecord
	for _, r := range f.records {
		if r.Status == domain.DeployFailed {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeDeployRecordStore) MarkRetrying(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id && r.Status == domain.DeployFailed && r.Retries < 3 {
			r.Status = domain.DeployRunning
			r.Retries++
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeDeployRecordStore) MarkTimedOut(_ context.Context, certID, targetID, message string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, r := range f.records {
		if r.CertificateID == certID && r.TargetID == targetID && r.Status == domain.DeployRunning {
			r.Status = domain.DeployFailed
			r.Message = message
			r.Retries = 3
			now := time.Now()
			r.DeployedAt = &now
			n++
		}
	}
	return n, nil
}

func (f *fakeDeployRecordStore) CountByStatus(context.Context, string) (int64, error) { return 0, nil }

func (f *fakeDeployRecordStore) DeleteByCertificate(_ context.Context, certID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.DeployRecord
	for _, r := range f.records {
		if r.CertificateID != certID {
			out = append(out, r)
		}
	}
	f.records = out
	return nil
}

func (f *fakeDeployRecordStore) DeleteByTarget(_ context.Context, targetID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.DeployRecord
	for _, r := range f.records {
		if r.TargetID != targetID {
			out = append(out, r)
		}
	}
	f.records = out
	return nil
}

func newTestDeployService() (*DeployService, *fakeDeployAdapter, *fakeCertificateStore, *fakeDeployTargetStore) {
	adapter := &fakeDeployAdapter{name: "local"}
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	svc := NewDeployService(targets, newFakeDeployRecordStore(), certs, &fakeAuditStore{},
		newFakeDeployRegistry(adapter), fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	return svc, adapter, certs, targets
}

func seedTarget(t *testing.T, targets *fakeDeployTargetStore, id, typ string) *domain.DeployTarget {
	t.Helper()
	target := &domain.DeployTarget{
		ID: id, Name: id, Type: domain.DeployTargetType(typ),
		ConfigEnc: "e30=", // base64("{}")
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	if err := targets.Create(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestCreateTargetValidation(t *testing.T) {
	svc, _, _, _ := newTestDeployService()
	ctx := context.Background()

	// 非法类型
	if _, err := svc.CreateTarget(ctx, DeployTargetInput{Name: "x", Type: "ftp", Config: map[string]any{}}); err == nil {
		t.Fatal("expected error for invalid type")
	}
	// 空名称
	if _, err := svc.CreateTarget(ctx, DeployTargetInput{Name: "", Type: domain.DeployLocal, Config: map[string]any{}}); err == nil {
		t.Fatal("expected error for empty name")
	}
	// 有效创建
	target, err := svc.CreateTarget(ctx, DeployTargetInput{
		Name: "local-1", Type: domain.DeployLocal,
		Config: map[string]any{"cert_path": "/etc/nginx/certs/x.pem"}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if target.ConfigEnc == "" {
		t.Fatal("expected encrypted config")
	}
}

func TestDeleteTargetCascadeCleansRecordsAndAgentTasks(t *testing.T) {
	svc, _, certs, targets := newTestDeployService()
	ctx := context.Background()

	seedTarget(t, targets, "t1", string(domain.DeployAgent))
	recs := &fakeDeployRecordStore{}
	svc.records = recs
	recs.Create(ctx, &domain.DeployRecord{ID: "r1", TargetID: "t1", CertificateID: "c1", Status: domain.DeployRunning})
	recs.Create(ctx, &domain.DeployRecord{ID: "r2", TargetID: "other", CertificateID: "c1", Status: domain.DeploySuccess})

	taskCleaner := &fakeAgentTaskStore{}
	svc.taskCleaner = taskCleaner
	taskCleaner.tasks = append(taskCleaner.tasks, &domain.AgentTask{ID: "k1", TargetID: "t1"})
	taskCleaner.tasks = append(taskCleaner.tasks, &domain.AgentTask{ID: "k2", TargetID: "other"})

	certs.Create(ctx, &domain.Certificate{ID: "c1", DeployTargetIDs: []string{"t1", "other"}})

	if err := svc.DeleteTarget(ctx, "t1"); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	if len(recs.records) != 1 || recs.records[0].ID != "r2" {
		t.Fatalf("records not cascade-cleaned: %+v", recs.records)
	}
	if len(taskCleaner.tasks) != 1 || taskCleaner.tasks[0].ID != "k2" {
		t.Fatalf("agent tasks not cascade-cleaned: %+v", taskCleaner.tasks)
	}
	// 证书绑定清理
	cert, _ := certs.GetByID(ctx, "c1")
	if len(cert.DeployTargetIDs) != 1 || cert.DeployTargetIDs[0] != "other" {
		t.Fatalf("cert binding not cleaned: %+v", cert.DeployTargetIDs)
	}
}

func TestUpdateTargetUpdatesEnabledAndConfig(t *testing.T) {
	svc, _, _, targets := newTestDeployService()
	ctx := context.Background()

	seedTarget(t, targets, "t1", "local")
	// 初始 enabled=true
	if _, err := svc.UpdateTarget(ctx, "t1", DeployTargetInput{Name: "", Type: "", Config: nil, Enabled: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	// 更新为禁用 + 改配置
	if _, err := svc.UpdateTarget(ctx, "t1", DeployTargetInput{
		Name:    "renamed",
		Type:    domain.DeployLocal,
		Config:  map[string]any{"cert_path": "/new/x.pem"},
		Enabled: false,
	}); err != nil {
		t.Fatalf("update disable: %v", err)
	}
	target, cfg, err := svc.GetTarget(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if target.Name != "renamed" {
		t.Fatalf("name: %q", target.Name)
	}
	if target.Enabled {
		t.Fatal("expected disabled")
	}
	if cfg["cert_path"] != "/new/x.pem" {
		t.Fatalf("config: %+v", cfg)
	}
	// 再次启用（回归：不因缺省被强制禁用）
	if _, err := svc.UpdateTarget(ctx, "t1", DeployTargetInput{Name: "", Type: "", Config: nil, Enabled: true}); err != nil {
		t.Fatalf("update enable: %v", err)
	}
	target, _, _ = svc.GetTarget(ctx, "t1")
	if !target.Enabled {
		t.Fatal("expected enabled after re-enable")
	}
}

func TestDeployToTargetsSuccess(t *testing.T) {
	svc, adapter, certs, targets := newTestDeployService()
	ctx := context.Background()
	seedTarget(t, targets, "t1", "local")

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "deploy.example.com", Domains: []string{"deploy.example.com"},
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeployToTargets(ctx, "c1", []string{"t1"}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.deploys != 1 {
		t.Fatalf("expected 1 deploy, got %d", adapter.deploys)
	}
	if adapter.payloads[0].PrimaryDomain != "deploy.example.com" {
		t.Fatalf("unexpected payload: %+v", adapter.payloads[0])
	}
}

func TestDeployFailsAndRecords(t *testing.T) {
	svc, adapter, certs, targets := newTestDeployService()
	ctx := context.Background()
	adapter.fail = true
	seedTarget(t, targets, "t1", "local")

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "fail.example.com",
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeployToTargets(ctx, "c1", []string{"t1"}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	// 失败不返回错误（记录失败状态），通过 RetryFailed 可重试
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.deploys != 1 {
		t.Fatalf("expected 1 deploy attempt, got %d", adapter.deploys)
	}
}

// TestDeployRejectsWhileRunning 已有部署中的任务时，拒绝再次提交部署
func TestDeployRejectsWhileRunning(t *testing.T) {
	adapter := &fakeDeployAdapter{name: "local"}
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewDeployService(targets, records, certs, &fakeAuditStore{},
		newFakeDeployRegistry(adapter), fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "busy.example.com", Domains: []string{"busy.example.com"},
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}
	// 已有部署中的记录
	if err := records.Create(ctx, &domain.DeployRecord{
		ID: "rec-1", CertificateID: "c1", TargetID: "t1",
		Status: domain.DeployRunning, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	err := svc.DeployToTargets(ctx, "c1", []string{"t1"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict while running, got %v", err)
	}
	if adapter.deploys != 0 {
		t.Fatalf("expected no deploy attempt, got %d", adapter.deploys)
	}
}

func TestEventTriggersAutoDeploy(t *testing.T) {
	bus := NewEventBus()
	adapter := &fakeDeployAdapter{name: "local"}
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	NewDeployService(targets, records, certs, &fakeAuditStore{},
		newFakeDeployRegistry(adapter), fakeCryptor{}, fixedClock{time.Now()}, bus)
	ctx := context.Background()
	seedTarget(t, targets, "t1", "local")

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "auto.example.com",
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		DeployTargetIDs: []string{"t1"},
		Status:          domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}

	// 发布 renewed 事件（模拟自动续期完成）→ 应触发自动部署
	bus.Publish(EventCertificateRenewed, cert)
	time.Sleep(100 * time.Millisecond)

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.deploys != 1 {
		t.Fatalf("expected auto deploy on event, got %d", adapter.deploys)
	}
}

// TestDeployPublishesEvents 部署成功/失败发布 deploy 事件
func TestDeployPublishesEvents(t *testing.T) {
	bus := NewEventBus()
	adapter := &fakeDeployAdapter{name: "local"}
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewDeployService(targets, records, certs, &fakeAuditStore{},
		newFakeDeployRegistry(adapter), fakeCryptor{}, fixedClock{time.Now()}, bus)
	ctx := context.Background()
	seedTarget(t, targets, "t1", "local")

	var success, failed []*DeployEvent
	bus.Subscribe(EventDeploySuccess, func(e Event) { success = append(success, e.Payload.(*DeployEvent)) })
	bus.Subscribe(EventDeployFailed, func(e Event) { failed = append(failed, e.Payload.(*DeployEvent)) })

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "events.example.com",
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}

	// 成功
	if err := svc.DeployToTargets(ctx, "c1", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	if len(success) != 1 || success[0].TargetName != "t1" {
		t.Fatalf("expected deploy.success with target, got %+v", success)
	}

	// 失败（新证书，避免记录 running 冲突）
	adapter.fail = true
	cert2 := &domain.Certificate{
		ID: "c2", PrimaryDomain: "events2.example.com",
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert2); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeployToTargets(ctx, "c2", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Error == "" {
		t.Fatalf("expected deploy.failed with error, got %+v", failed)
	}
}

func TestRetryFailedDeploys(t *testing.T) {
	svc, adapter, certs, targets := newTestDeployService()
	ctx := context.Background()
	seedTarget(t, targets, "t1", "local")

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "retry.example.com",
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}

	// 首次失败
	adapter.fail = true
	if err := svc.DeployToTargets(ctx, "c1", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	first := adapter.deploys
	adapter.mu.Unlock()

	// 恢复后重试
	adapter.fail = false
	if err := svc.RetryFailed(ctx); err != nil {
		t.Fatal(err)
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.deploys <= first {
		t.Fatalf("expected retry deploy, got %d after %d", adapter.deploys, first)
	}
}

func mustEnc(s string) string {
	enc, _ := fakeCryptor{}.EncryptString(s)
	return enc
}

func TestCloudAccountCredentialsEncrypted(t *testing.T) {
	accounts := newFakeProviderAccountStore()
	svc := NewProviderAccountService(accounts, newFakeCertificateStore(), &fakeAuditStore{},
		newFakeRegistry(newFakeProvider("acme")), fakeCryptor{}, fixedClock{time.Now()})
	ctx := context.Background()

	account, err := svc.Create(ctx, CreateAccountInput{
		Provider: "acme",
		Name:     "cloud",
		Credentials: map[string]string{
			"access_key_id": "AK-test", "access_key_secret": "SK-secret",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 凭据必须加密存储（fakeCryptor = base64，密文不等于明文）
	if account.CredentialsEnc == "" {
		t.Fatal("credentials should be encrypted at rest")
	}
	plain, err := fakeCryptor{}.DecryptString(account.CredentialsEnc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "AK-test") || !strings.Contains(plain, "SK-secret") {
		t.Fatalf("unexpected decrypted credentials: %s", plain)
	}
}

// TestDeployToOneMissingTargetRecordsFailure 目标已删除/停用时必须落失败记录并通知，
// 不能静默返回（否则续期后的自动部署会被误认为成功）
func TestDeployToOneMissingTargetRecordsFailure(t *testing.T) {
	bus := NewEventBus()
	adapter := &fakeDeployAdapter{name: "local"}
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewDeployService(targets, records, certs, &fakeAuditStore{},
		newFakeDeployRegistry(adapter), fakeCryptor{}, fixedClock{time.Now()}, bus)
	ctx := context.Background()

	var failures []*DeployEvent
	bus.Subscribe(EventDeployFailed, func(e Event) { failures = append(failures, e.Payload.(*DeployEvent)) })

	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "ghost.example.com",
		CertificatePEM: "CERT", PrivateKeyEnc: mustEnc("KEY"), Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}

	// 目标不存在
	if err := svc.DeployToTargets(ctx, "c1", []string{"ghost-target"}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	got, _ := records.ListByCertificate(ctx, "c1")
	if len(got) != 1 || got[0].Status != domain.DeployFailed || got[0].TargetID != "ghost-target" {
		t.Fatalf("expected failed record for missing target, got %+v", got)
	}
	if len(failures) != 1 {
		t.Fatalf("expected deploy.failed event, got %d", len(failures))
	}

	// 目标被停用
	seedTarget(t, targets, "t-off", "local")
	if _, err := svc.UpdateTarget(ctx, "t-off", DeployTargetInput{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeployToTargets(ctx, "c1", []string{"t-off"}); err != nil {
		t.Fatalf("deploy disabled: %v", err)
	}
	got, _ = records.ListByCertificate(ctx, "c1")
	if len(got) != 2 || got[1].Status != domain.DeployFailed {
		t.Fatalf("expected failed record for disabled target, got %+v", got)
	}
}
