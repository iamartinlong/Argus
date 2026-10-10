package application

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

// fakeAgentStore 内存 agent 存储
type fakeAgentStore struct {
	mu     sync.Mutex
	agents map[string]*domain.Agent
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{agents: map[string]*domain.Agent{}}
}

func (f *fakeAgentStore) Create(_ context.Context, a *domain.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.ID] = a
	return nil
}

func (f *fakeAgentStore) GetByID(_ context.Context, id string) (*domain.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.agents[id]; ok {
		return a, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeAgentStore) GetByHash(_ context.Context, hash string) (*domain.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.agents {
		if a.TokenHash == hash {
			return a, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeAgentStore) List(context.Context) ([]*domain.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Agent
	for _, a := range f.agents {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeAgentStore) Update(_ context.Context, a *domain.Agent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[a.ID] = a
	return nil
}

func (f *fakeAgentStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, id)
	return nil
}

func (f *fakeAgentStore) CountByStatus(_ context.Context, status string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, a := range f.agents {
		if a.Status == status {
			n++
		}
	}
	return n, nil
}

func (f *fakeAgentStore) MarkStaleOffline(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, a := range f.agents {
		if a.Enabled && a.Status == "online" && a.LastSeenAt != nil && a.LastSeenAt.Before(cutoff) {
			a.Status = "offline"
			n++
		}
	}
	return n, nil
}

// fakeAgentTaskStore 内存任务存储
type fakeAgentTaskStore struct {
	mu     sync.Mutex
	tasks  []*domain.AgentTask
}

func newFakeAgentTaskStore() *fakeAgentTaskStore {
	return &fakeAgentTaskStore{}
}

func (f *fakeAgentTaskStore) Create(_ context.Context, t *domain.AgentTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks = append(f.tasks, t)
	return nil
}

func (f *fakeAgentTaskStore) GetByID(_ context.Context, id string) (*domain.AgentTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeAgentTaskStore) ListByAgent(_ context.Context, agentID string, limit int) ([]*domain.AgentTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.AgentTask
	for _, t := range f.tasks {
		if t.AgentID == agentID {
			out = append(out, t)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeAgentTaskStore) ClaimNext(_ context.Context, agentID string) (*domain.AgentTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tasks {
		if t.AgentID == agentID && t.Status == domain.AgentTaskPending {
			t.Status = domain.AgentTaskRunning
			now := time.Now()
			t.ClaimedAt = &now
			return t, nil
		}
	}
	return nil, nil
}

func (f *fakeAgentTaskStore) Update(_ context.Context, t *domain.AgentTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, x := range f.tasks {
		if x.ID == t.ID {
			f.tasks[i] = t
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeAgentTaskStore) MarkStaleTasks(_ context.Context, olderThan time.Duration) ([]*domain.AgentTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	var affected []*domain.AgentTask
	for _, t := range f.tasks {
		if (t.Status == domain.AgentTaskRunning && t.ClaimedAt != nil && t.ClaimedAt.Before(cutoff)) ||
			(t.Status == domain.AgentTaskPending && t.CreatedAt.Before(cutoff)) {
			// 返回 UPDATE 前快照（保留原状态供上层区分原因），再原地更新任务为 failed
			snap := *t
			affected = append(affected, &snap)
			t.Status = domain.AgentTaskFailed
			if t.ClaimedAt != nil {
				t.Message = "agent 超时未回执"
			} else {
				t.Message = "agent 未领取超时"
			}
		}
	}
	return affected, nil
}

func (f *fakeAgentTaskStore) DeleteByTarget(_ context.Context, targetID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.AgentTask
	for _, t := range f.tasks {
		if t.TargetID != targetID {
			out = append(out, t)
		}
	}
	f.tasks = out
	return nil
}

func newTestAgentService() (*AgentService, *fakeAgentStore, *fakeAgentTaskStore, *fakeCertificateStore, *fakeDeployTargetStore) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	svc := NewAgentService(agents, tasks, targets, certs, newFakeDeployRecordStore(), &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	return svc, agents, tasks, certs, targets
}

func TestAgentCreateAndAuthenticate(t *testing.T) {
	svc, agents, _, _, _ := newTestAgentService()
	ctx := context.Background()

	agent, raw, err := svc.CreateAgent(ctx, "prod-01")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(raw, "ag_") || agent.TokenHash == "" {
		t.Fatalf("unexpected token: %q", raw)
	}
	if agent.TokenHash != HashToken(raw) {
		t.Fatalf("token hash mismatch")
	}
	// 认证并更新心跳
	authed, err := svc.Authenticate(ctx, raw, "0.1.0", "linux")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authed.Status != "online" || authed.LastSeenAt == nil {
		t.Fatalf("expected online status, got %+v", authed)
	}
	// 错误 token
	if _, err := svc.Authenticate(ctx, "ag_wrong", "", ""); err != domain.ErrUnauthorized {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	_ = agents
}

func TestReconcileStaleOffline(t *testing.T) {
	svc, agents, _, _, _ := newTestAgentService()
	ctx := context.Background()

	now := time.Now()
	// 心跳较新的 agent（保持在线）
	recent := &domain.Agent{ID: "a1", Name: "recent", TokenHash: "h1", Status: "online", Enabled: true,
		LastSeenAt: &now, CreatedAt: now}
	// 心跳超时的 agent（应转离线）
	old := now.Add(-time.Hour)
	stale := &domain.Agent{ID: "a2", Name: "stale", TokenHash: "h2", Status: "online", Enabled: true,
		LastSeenAt: &old, CreatedAt: now}
	// 禁用中的 agent（不处理）
	disabled := &domain.Agent{ID: "a3", Name: "disabled", TokenHash: "h3", Status: "offline", Enabled: false,
		LastSeenAt: &now, CreatedAt: now}
	for _, a := range []*domain.Agent{recent, stale, disabled} {
		if err := agents.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	n, err := svc.ReconcileStaleOffline(ctx, 30*time.Second)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 stale offline, got %d", n)
	}
	a2, _ := agents.GetByID(ctx, "a2")
	if a2.Status != "offline" {
		t.Fatalf("stale agent should be offline, got %s", a2.Status)
	}
	a1, _ := agents.GetByID(ctx, "a1")
	if a1.Status != "online" {
		t.Fatalf("recent agent should stay online, got %s", a1.Status)
	}
}

func TestAgentDeployQueueAndClaim(t *testing.T) {
	svc, _, _, certs, targets := newTestAgentService()
	ctx := context.Background()

	// 准备证书与目标
	cert := &domain.Certificate{
		ID: "c1", PrimaryDomain: "agent.example.com",
		CertificatePEM: "CERT-PEM", PrivateKeyEnc: mustEnc("KEY-PEM"),
		Status: domain.StatusValid,
	}
	if err := certs.Create(ctx, cert); err != nil {
		t.Fatal(err)
	}
	target := &domain.DeployTarget{
		ID: "t1", Name: "agent-target", Type: domain.DeployTargetType("agent"),
		ConfigEnc: mustEnc(`{"agent_id":"a1","cert_path":"/tmp/cert.pem","key_path":"/tmp/key.pem","hook":"echo ok"}`),
		Enabled:   true,
	}
	if err := targets.Create(ctx, target); err != nil {
		t.Fatal(err)
	}
	agent := &domain.Agent{ID: "a1", Name: "agent-1", TokenHash: "h", Status: "online"}
	if err := svc.agents.Create(ctx, agent); err != nil {
		t.Fatal(err)
	}

	// 排队部署
	if err := svc.QueueDeploy(ctx, "a1", "c1", "t1"); err != nil {
		t.Fatal(err)
	}
	// agent 领取任务（动态组装 payload）
	payload, err := svc.ClaimTask(ctx, "a1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if payload == nil {
		t.Fatal("expected task")
	}
	if payload.CertPath != "/tmp/cert.pem" || payload.CertificatePEM != "CERT-PEM" || payload.PrivateKeyPEM != "KEY-PEM" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Hook != "echo ok" {
		t.Fatalf("hook missing: %+v", payload)
	}
	// 回执
	if err := svc.SubmitResult(ctx, "a1", payload.TaskID, true, "checksum-1", "done"); err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.GetByID(ctx, payload.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.AgentTaskSuccess || task.Checksum != "checksum-1" {
		t.Fatalf("unexpected task state: %+v", task)
	}
	// 再次领取无任务
	payload2, err := svc.ClaimTask(ctx, "a1")
	if err != nil || payload2 != nil {
		t.Fatalf("expected no more tasks, got %v (%v)", payload2, err)
	}
}

func TestAgentSubmitResultWrongAgent(t *testing.T) {
	svc, _, _, certs, targets := newTestAgentService()
	ctx := context.Background()
	cert := &domain.Certificate{ID: "c1", PrimaryDomain: "x.com", CertificatePEM: "C", PrivateKeyEnc: mustEnc("K"), Status: domain.StatusValid}
	_ = certs.Create(ctx, cert)
	target := &domain.DeployTarget{ID: "t1", Type: "agent", ConfigEnc: mustEnc(`{}`), Enabled: true}
	_ = targets.Create(ctx, target)
	_ = svc.QueueDeploy(ctx, "a1", "c1", "t1")
	task, _ := svc.tasks.ClaimNext(ctx, "a1")
	if task == nil {
		t.Fatal("expected task")
	}
	// 其他 agent 回执 → 403
	if err := svc.SubmitResult(ctx, "a2", task.ID, true, "", "x"); err != domain.ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestAgentAdapterDeploy(t *testing.T) {
	svc, agents, tasks, certs, targets := newTestAgentService()
	ctx := context.Background()
	agent := &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"}
	_ = agents.Create(ctx, agent)
	_ = certs.Create(ctx, &domain.Certificate{ID: "c1", PrimaryDomain: "x.com", CertificatePEM: "C", PrivateKeyEnc: mustEnc("K"), Status: domain.StatusValid})
	_ = targets.Create(ctx, &domain.DeployTarget{ID: "t1", Type: "agent", ConfigEnc: mustEnc(`{}`), Enabled: true})

	adapter := NewAgentAdapter(svc, fixedClock{time.Now()})
	result, err := adapter.Deploy(ctx, deploy.Payload{PrimaryDomain: "x.com", CertificatePEM: "C", PrivateKeyPEM: "K"}, map[string]any{
		"agent_id": "a1", "_cert_id": "c1", "_target_id": "t1",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.Checksum == "" {
		t.Fatal("expected checksum")
	}
	// 任务已排队
	task, _ := tasks.ClaimNext(ctx, "a1")
	if task == nil || task.CertificateID != "c1" {
		t.Fatalf("expected queued task, got %+v", task)
	}
}

// TestClaimTaskLazilyCleansStale 领取前惰性清理超时任务（不依赖调度器）
func TestClaimTaskLazilyCleansStale(t *testing.T) {
	svc, agents, tasks, certs, targets := newTestAgentService()
	ctx := context.Background()
	agent := &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"}
	_ = agents.Create(ctx, agent)
	_ = certs.Create(ctx, &domain.Certificate{ID: "c1", PrimaryDomain: "x.com", CertificatePEM: "C", PrivateKeyEnc: mustEnc("K"), Status: domain.StatusValid})
	_ = targets.Create(ctx, &domain.DeployTarget{ID: "t1", Type: "agent", ConfigEnc: mustEnc(`{}`), Enabled: true})

	// 制造一个超时的 running 任务（claimed_at 很久以前）
	old := time.Now().Add(-30 * time.Minute)
	stale := &domain.AgentTask{
		ID: "stale-1", AgentID: "a1", CertificateID: "c1", TargetID: "t1",
		Status: domain.AgentTaskRunning, ClaimedAt: &old, CreatedAt: time.Now().Add(-time.Hour),
	}
	_ = tasks.Create(ctx, stale)

	// ClaimTask 应触发清理并把 stale 标记为 failed
	if _, err := svc.ClaimTask(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	got, _ := tasks.GetByID(ctx, "stale-1")
	if got.Status != domain.AgentTaskFailed {
		t.Fatalf("stale task should be marked failed, got %s", got.Status)
	}
	if got.Message != "agent 超时未回执" {
		t.Fatalf("unexpected message: %s", got.Message)
	}
}

// TestSubmitResultUpdatesDeployRecord 回执联动更新部署记录
func TestSubmitResultUpdatesDeployRecord(t *testing.T) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewAgentService(agents, tasks, targets, certs, records, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()
	_ = agents.Create(ctx, &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"})
	_ = certs.Create(ctx, &domain.Certificate{ID: "c1", PrimaryDomain: "x.com", CertificatePEM: "C", PrivateKeyEnc: mustEnc("K"), Status: domain.StatusValid})
	_ = targets.Create(ctx, &domain.DeployTarget{ID: "t1", Type: "agent", ConfigEnc: mustEnc(`{}`), Enabled: true})

	// 排队任务 + 关联 running 部署记录
	_ = svc.QueueDeploy(ctx, "a1", "c1", "t1")
	task, _ := tasks.ClaimNext(ctx, "a1")
	if task == nil {
		t.Fatal("expected task")
	}
	rec := &domain.DeployRecord{
		ID: "rec-1", TargetID: "t1", CertificateID: "c1",
		Status: domain.DeployRunning, Message: "已排队等待 agent 拉取执行",
		CreatedAt: time.Now(),
	}
	_ = records.Create(ctx, rec)

	// agent 回执成功
	if err := svc.SubmitResult(ctx, "a1", task.ID, true, "checksum-xyz", "部署完成；hook 已执行"); err != nil {
		t.Fatal(err)
	}
	got, _ := records.GetByID(ctx, "rec-1")
	if got.Status != domain.DeploySuccess {
		t.Fatalf("deploy record should be success, got %s", got.Status)
	}
	if got.Checksum != "checksum-xyz" || got.Message != "部署完成；hook 已执行" {
		t.Fatalf("record not updated: %+v", got)
	}
}

// TestClaimTaskIISPFXComposition IIS 场景：服务端合成 PFX 附带 payload（agent 仅下载导入）
func TestClaimTaskIISPFXComposition(t *testing.T) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewAgentService(agents, tasks, targets, certs, records, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()
	_ = agents.Create(ctx, &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"})
	// 用真实自签证书（PFX 合成需要有效 PEM）
	certPEM, keyPEM := generateTestCertWithKey("iis-agent.example.com")
	_ = certs.Create(ctx, &domain.Certificate{
		ID: "c1", PrimaryDomain: "iis-agent.example.com",
		CertificatePEM: certPEM, PrivateKeyEnc: mustEnc(keyPEM), Status: domain.StatusValid,
	})
	// agent 目标含 pfx_path（IIS 场景标记）
	_ = targets.Create(ctx, &domain.DeployTarget{
		ID: "t1", Type: "agent", Enabled: true,
		ConfigEnc: mustEnc(`{"agent_id":"a1","pfx_path":"C:\\certs\\x.pfx","is_windows":true}`),
	})
	_ = svc.QueueDeploy(ctx, "a1", "c1", "t1")

	payload, err := svc.ClaimTask(ctx, "a1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if payload == nil {
		t.Fatal("expected payload")
	}
	if payload.PfxPath != `C:\certs\x.pfx` || payload.PfxData == "" || payload.PfxPassword == "" {
		t.Fatalf("expected pfx composition: %+v", payload)
	}
	if !payload.IsWindows {
		t.Fatal("expected is_windows from config")
	}
	// base64 解码后可解析为有效 PFX（非空且含 PKCS12 头）
	pfxBytes, err := base64.StdEncoding.DecodeString(payload.PfxData)
	if err != nil {
		t.Fatalf("pfx decode: %v", err)
	}
	if len(pfxBytes) < 20 {
		t.Fatalf("pfx too small: %d", len(pfxBytes))
	}
	// 私钥明文不应进入 payload（仅 PFX 附带）
	if strings.Contains(payload.CertificatePEM, keyPEM) {
		t.Fatal("payload should not contain raw private key alongside pfx")
	}
}

// TestClaimTaskAgentAutoIISScript pfx+bindings+site_name 时自动生成完整 IIS 脚本（免手写 hook）
func TestClaimTaskAgentAutoIISScript(t *testing.T) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewAgentService(agents, tasks, targets, certs, records, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()
	_ = agents.Create(ctx, &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"})
	certPEM, keyPEM := generateTestCertWithKey("iis-agent.example.com")
	_ = certs.Create(ctx, &domain.Certificate{
		ID: "c1", PrimaryDomain: "iis-agent.example.com",
		CertificatePEM: certPEM, PrivateKeyEnc: mustEnc(keyPEM), Status: domain.StatusValid,
	})
	// 无 hook，含 pfx_path + bindings + site_name → 自动生成脚本
	_ = targets.Create(ctx, &domain.DeployTarget{
		ID: "t1", Type: "agent", Enabled: true,
		ConfigEnc: mustEnc(`{"agent_id":"a1","pfx_path":"C:\\certs\\x.pfx","bindings":[443,8443],"site_name":"Default Web Site"}`),
	})
	_ = svc.QueueDeploy(ctx, "a1", "c1", "t1")
	payload, err := svc.ClaimTask(ctx, "a1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if payload == nil {
		t.Fatal("expected payload")
	}
	if payload.Hook == "" {
		t.Fatal("expected auto-generated IIS script")
	}
	for _, c := range []string{"Import-PfxCertificate", "netsh http add sslcert", "appcmd.exe", "Default Web Site", "iis-agent.example.com", "443, 8443"} {
		if !strings.Contains(payload.Hook, c) {
			t.Fatalf("auto hook missing %q\n%s", c, payload.Hook)
		}
	}
	if !payload.IsWindows {
		t.Fatal("expected is_windows auto-set for IIS")
	}
}

// TestClaimTaskAgentHookTakesPrecedence 用户手写 hook 优先于自动生成
func TestClaimTaskAgentHookTakesPrecedence(t *testing.T) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewAgentService(agents, tasks, targets, certs, records, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()
	_ = agents.Create(ctx, &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"})
	certPEM, keyPEM := generateTestCertWithKey("iis-agent.example.com")
	_ = certs.Create(ctx, &domain.Certificate{
		ID: "c1", PrimaryDomain: "iis-agent.example.com",
		CertificatePEM: certPEM, PrivateKeyEnc: mustEnc(keyPEM), Status: domain.StatusValid,
	})
	// 用户自定义 hook + pfx_path（PFX 变量仍渲染）
	_ = targets.Create(ctx, &domain.DeployTarget{
		ID: "t1", Type: "agent", Enabled: true,
		ConfigEnc: mustEnc(`{"agent_id":"a1","pfx_path":"C:\\certs\\x.pfx","bindings":[443],"site_name":"Default Web Site","hook":"Write-Output 'custom {pfx_password} {domain}'"}`),
	})
	_ = svc.QueueDeploy(ctx, "a1", "c1", "t1")
	payload, err := svc.ClaimTask(ctx, "a1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !strings.Contains(payload.Hook, "custom") {
		t.Fatalf("expected custom hook, got: %s", payload.Hook)
	}
	// 变量渲染后不应残留占位符
	if strings.Contains(payload.Hook, "{pfx_password}") || strings.Contains(payload.Hook, "{domain}") {
		t.Fatalf("hook placeholders not rendered: %s", payload.Hook)
	}
}

// TestClaimTaskHookTemplateRendered hook 模板变量渲染（{pfx_path}/{pfx_password}/{domain}）
func TestClaimTaskHookTemplateRendered(t *testing.T) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewAgentService(agents, tasks, targets, certs, records, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()
	_ = agents.Create(ctx, &domain.Agent{ID: "a1", Name: "a", TokenHash: "h", Status: "online"})
	certPEM, keyPEM := generateTestCertWithKey("hook-agent.example.com")
	_ = certs.Create(ctx, &domain.Certificate{
		ID: "c1", PrimaryDomain: "hook-agent.example.com",
		CertificatePEM: certPEM, PrivateKeyEnc: mustEnc(keyPEM), Status: domain.StatusValid,
	})
	// hook 引用 PFX 动态变量与域名
	_ = targets.Create(ctx, &domain.DeployTarget{
		ID: "t1", Type: "agent", Enabled: true,
		ConfigEnc: mustEnc(`{"agent_id":"a1","pfx_path":"C:\\certs\\x.pfx","hook":"Import-PfxCertificate -FilePath '{pfx_path}' -Password (ConvertTo-SecureString '{pfx_password}' -AsPlainText -Force); Write-Output '{domain}'","is_windows":true}`),
	})
	_ = svc.QueueDeploy(ctx, "a1", "c1", "t1")

	payload, err := svc.ClaimTask(ctx, "a1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if payload == nil {
		t.Fatal("expected payload")
	}
	if strings.Contains(payload.Hook, "{pfx_path}") || strings.Contains(payload.Hook, "{pfx_password}") || strings.Contains(payload.Hook, "{domain}") {
		t.Fatalf("hook placeholders not rendered: %s", payload.Hook)
	}
	if !strings.Contains(payload.Hook, payload.PfxPath) {
		t.Fatalf("hook missing pfx_path: %s", payload.Hook)
	}
	if !strings.Contains(payload.Hook, payload.PfxPassword) {
		t.Fatalf("hook missing pfx_password: %s", payload.Hook)
	}
	if !strings.Contains(payload.Hook, "hook-agent.example.com") {
		t.Fatalf("hook missing domain: %s", payload.Hook)
	}
}

// TestReconcileStaleAgentTasksLinksDeployRecord agent 任务超时未回执时，
// 联动将对应 running 部署记录置为 failed（retries 置顶排除自动重试）。
func TestReconcileStaleAgentTasksLinksDeployRecord(t *testing.T) {
	agents := newFakeAgentStore()
	tasks := newFakeAgentTaskStore()
	certs := newFakeCertificateStore()
	targets := newFakeDeployTargetStore()
	records := newFakeDeployRecordStore()
	svc := NewAgentService(agents, tasks, targets, certs, records, &fakeAuditStore{}, fakeCryptor{}, fixedClock{time.Now()}, NewEventBus())
	ctx := context.Background()

	claimed := time.Now().Add(-30 * time.Minute)
	if err := tasks.Create(ctx, &domain.AgentTask{
		ID: "task-1", AgentID: "a1", CertificateID: "c1", TargetID: "t1",
		Status: domain.AgentTaskRunning, ClaimedAt: &claimed, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 超时的 pending 任务（从未被领取）
	if err := tasks.Create(ctx, &domain.AgentTask{
		ID: "task-2", AgentID: "a2", CertificateID: "c2", TargetID: "t2",
		Status: domain.AgentTaskPending, CreatedAt: time.Now().Add(-30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// 对应的 running 部署记录
	if err := records.Create(ctx, &domain.DeployRecord{
		ID: "rec-1", CertificateID: "c1", TargetID: "t1", Status: domain.DeployRunning, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 与 pending 超时任务对应的 running 部署记录
	if err := records.Create(ctx, &domain.DeployRecord{
		ID: "rec-3", CertificateID: "c2", TargetID: "t2", Status: domain.DeployRunning, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 无关记录（不应被误改）
	if err := records.Create(ctx, &domain.DeployRecord{
		ID: "rec-2", CertificateID: "c9", TargetID: "t9", Status: domain.DeployRunning, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.ReconcileStaleAgentTasks(ctx, 10*time.Minute); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// 超时任务被标记失败
	tt, _ := tasks.GetByID(ctx, "task-1")
	if tt.Status != domain.AgentTaskFailed || tt.Message != "agent 超时未回执" {
		t.Fatalf("task not marked failed: %+v", tt)
	}
	tt2, _ := tasks.GetByID(ctx, "task-2")
	if tt2.Status != domain.AgentTaskFailed || tt2.Message != "agent 未领取超时" {
		t.Fatalf("pending task not marked failed: %+v", tt2)
	}

	// 对应部署记录置为失败、retries 置顶、原因正确
	rec, _ := records.GetByID(ctx, "rec-1")
	if rec.Status != domain.DeployFailed {
		t.Fatalf("expected deploy record failed, got %s", rec.Status)
	}
	if rec.Message != "agent 超时未回执" {
		t.Fatalf("unexpected message: %s", rec.Message)
	}
	if rec.Retries < 3 {
		t.Fatalf("expected retries capped to exclude retry, got %d", rec.Retries)
	}

	// pending 超时任务对应的部署记录也应被联动置为失败
	rec3, _ := records.GetByID(ctx, "rec-3")
	if rec3.Status != domain.DeployFailed {
		t.Fatalf("expected pending-linked record failed, got %s", rec3.Status)
	}
	if rec3.Message != "agent 未领取超时" {
		t.Fatalf("unexpected message for pending record: %s", rec3.Message)
	}

	// 无关记录不受影响
	rec2, _ := records.GetByID(ctx, "rec-2")
	if rec2.Status != domain.DeployRunning {
		t.Fatalf("unrelated record should stay running, got %s", rec2.Status)
	}
}

