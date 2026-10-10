package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

// DeployAdapterRegistry 部署通道注册表（infrastructure/deploy.Registry 实现）
type DeployAdapterRegistry interface {
	Get(t string) (deploy.Adapter, bool)
}

// AgentTaskCleaner 删除目标时级联清理 agent 任务的清理器（可空）
type AgentTaskCleaner interface {
	DeleteByTarget(ctx context.Context, targetID string) error
}

type DeployService struct {
	targets     domain.DeployTargetStore
	records     domain.DeployRecordStore
	certs       domain.CertificateStore
	audit       domain.AuditStore
	adapters    DeployAdapterRegistry
	cryptor     Cryptor
	clock       Clock
	bus         *EventBus
	taskCleaner AgentTaskCleaner
	// deployLocks 按 certID 互斥：hasRunningDeploy 是 check-then-act，
	// 手动部署与续期事件并发进入会对同一证书双份部署
	deployLocks sync.Map
}

func NewDeployService(
	targets domain.DeployTargetStore,
	records domain.DeployRecordStore,
	certs domain.CertificateStore,
	audit domain.AuditStore,
	adapters DeployAdapterRegistry,
	cryptor Cryptor,
	clock Clock,
	bus *EventBus,
) *DeployService {
	s := &DeployService{
		targets: targets, records: records, certs: certs, audit: audit,
		adapters: adapters, cryptor: cryptor, clock: clock, bus: bus,
	}	// 自动部署：签发/续期成功后推送至绑定目标
	if bus != nil {
		bus.Subscribe(EventCertificateIssued, s.onCertificateEvent)
		bus.Subscribe(EventCertificateRenewed, s.onCertificateEvent)
	}
	return s
}

// SetAgentTaskCleaner 注入 agent 任务清理器（删除目标时级联清理 agent 任务）
func (s *DeployService) SetAgentTaskCleaner(c AgentTaskCleaner) { s.taskCleaner = c }

func (s *DeployService) onCertificateEvent(e Event) {
	cert, ok := e.Payload.(*domain.Certificate)
	if !ok || len(cert.DeployTargetIDs) == 0 {
		return
	}
	// 异步执行：签发/续期请求在 Publish 调用栈内同步等部署会把 API 响应拖到
	// 全部目标部署完成（单目标可达数分钟），故放到后台 goroutine
	go func(cert *domain.Certificate) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = s.DeployToTargets(ctx, cert.ID, cert.DeployTargetIDs)
	}(cert)
}

type DeployTargetInput struct {
	Name      string
	Type      domain.DeployTargetType
	Config    map[string]any // 目标配置（凭据由 service 加密存储）
	Enabled   bool
}

func (s *DeployService) CreateTarget(ctx context.Context, input DeployTargetInput) (*domain.DeployTarget, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: 目标名称不能为空", domain.ErrInvalidArgument)
	}
	if !input.Type.Valid() {
		return nil, fmt.Errorf("%w: 无效目标类型 %s", domain.ErrInvalidArgument, input.Type)
	}
	if _, ok := s.adapters.Get(string(input.Type)); !ok {
		return nil, fmt.Errorf("%w: 未注册的部署类型 %s", domain.ErrInvalidArgument, input.Type)
	}
	plain, err := json.Marshal(input.Config)
	if err != nil {
		return nil, err
	}
	enc, err := s.cryptor.EncryptString(string(plain))
	if err != nil {
		return nil, err
	}
	target := &domain.DeployTarget{
		ID:        newID(),
		Name:      input.Name,
		Type:      input.Type,
		ConfigEnc: enc,
		Enabled:   input.Enabled,
		CreatedAt: s.clock.Now(),
		UpdatedAt: s.clock.Now(),
	}
	if err := s.targets.Create(ctx, target); err != nil {
		return nil, err
	}
	s.auditAction(ctx, "deploy.target.create", target.Name)
	return target, nil
}

func (s *DeployService) ListTargets(ctx context.Context) ([]*domain.DeployTarget, error) {
	return s.targets.List(ctx)
}

// GetTarget 查询单个部署目标；config 解密后返回（供查看/编辑回显）
func (s *DeployService) GetTarget(ctx context.Context, id string) (*domain.DeployTarget, map[string]any, error) {
	target, err := s.targets.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := s.targetConfig(target)
	if err != nil {
		return nil, nil, err
	}
	return target, cfg, nil
}

// UpdateTarget 更新目标名称/启用状态/配置（config 重新加密存储）
func (s *DeployService) UpdateTarget(ctx context.Context, id string, input DeployTargetInput) (*domain.DeployTarget, error) {
	target, err := s.targets.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		target.Name = input.Name
	}
	if input.Type.Valid() {
		if _, ok := s.adapters.Get(string(input.Type)); !ok {
			return nil, fmt.Errorf("%w: 未注册的部署类型 %s", domain.ErrInvalidArgument, input.Type)
		}
		target.Type = input.Type
	}
	target.Enabled = input.Enabled
	if input.Config != nil {
		merged := make(map[string]any, len(input.Config))
		for k, v := range input.Config {
			merged[k] = v
		}
		if plainOld, err := s.cryptor.DecryptString(target.ConfigEnc); err == nil && plainOld != "" {
			var oldCfg map[string]any
			_ = json.Unmarshal([]byte(plainOld), &oldCfg)
			for k, v := range oldCfg {
				if cur, ok := merged[k]; !ok || cur == "" {
					merged[k] = v
				} else if s, ok := cur.(string); ok && s == "" {
					merged[k] = v
				}
			}
		}
		plain, err := json.Marshal(merged)
		if err != nil {
			return nil, err
		}
		enc, err := s.cryptor.EncryptString(string(plain))
		if err != nil {
			return nil, err
		}
		target.ConfigEnc = enc
	}
	target.UpdatedAt = s.clock.Now()
	if err := s.targets.Update(ctx, target); err != nil {
		return nil, err
	}
	s.auditAction(ctx, "deploy.target.update", target.Name)
	return target, nil
}

func (s *DeployService) DeleteTarget(ctx context.Context, id string) error {
	target, err := s.targets.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.targets.Delete(ctx, id); err != nil {
		return err
	}
	// 清理引用该目标的部署记录与 agent 任务（避免孤儿记录）
	_ = s.records.DeleteByTarget(ctx, id)
	if s.taskCleaner != nil {
		_ = s.taskCleaner.DeleteByTarget(ctx, id)
	}
	// 清理引用该目标的证书绑定
	certs, err := s.certs.List(ctx)
	if err == nil {
		for _, cert := range certs {
			ids := make([]string, 0, len(cert.DeployTargetIDs))
			changed := false
			for _, tid := range cert.DeployTargetIDs {
				if tid == id {
					changed = true
					continue
				}
				ids = append(ids, tid)
			}
			if changed {
				cert.DeployTargetIDs = ids
				_ = s.certs.Update(ctx, cert)
			}
		}
	}
	s.auditAction(ctx, "deploy.target.delete", target.Name)
	return nil
}

// TestTarget 目标连接测试
func (s *DeployService) TestTarget(ctx context.Context, id string) error {
	target, err := s.targets.GetByID(ctx, id)
	if err != nil {
		return err
	}
	adapter, ok := s.adapters.Get(string(target.Type))
	if !ok {
		return fmt.Errorf("未注册的部署类型 %s", target.Type)
	}
	cfg, err := s.targetConfig(target)
	if err != nil {
		return err
	}
	if err := adapter.Test(ctx, cfg); err != nil {
		target.LastStatus = "error"
		target.LastError = err.Error()
		target.UpdatedAt = s.clock.Now()
		_ = s.targets.Update(ctx, target)
		return err
	}
	target.LastStatus = "success"
	target.LastError = ""
	target.UpdatedAt = s.clock.Now()
	_ = s.targets.Update(ctx, target)
	return nil
}

// DeployToTargets 部署证书到指定目标（手动/事件触发共用）
func (s *DeployService) DeployToTargets(ctx context.Context, certID string, targetIDs []string) error {
	// 同一证书串行化：先抢锁再查 running，避免并发双份部署
	muAny, _ := s.deployLocks.LoadOrStore(certID, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// 已有部署中的任务时拒绝再次提交，避免重复部署与重复记录
	running, err := s.hasRunningDeploy(ctx, certID)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("%w: 该证书已有部署正在进行中，请等待完成后再部署", domain.ErrConflict)
	}
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return err
	}
	if cert.CertificatePEM == "" {
		return fmt.Errorf("%w: 证书内容为空，无法部署", domain.ErrInvalidArgument)
	}
	priv, err := s.cryptor.DecryptString(cert.PrivateKeyEnc)
	if err != nil {
		return fmt.Errorf("解密私钥失败: %w", err)
	}
	payload := deploy.Payload{
		Domains:        cert.Domains,
		PrimaryDomain:  cert.PrimaryDomain,
		CertificatePEM: cert.CertificatePEM,
		PrivateKeyPEM:  priv,
	}
	for _, targetID := range targetIDs {
		s.deployOne(ctx, cert, targetID, payload, 0)
	}
	return nil
}

// hasRunningDeploy 该证书是否已有部署中的记录
func (s *DeployService) hasRunningDeploy(ctx context.Context, certID string) (bool, error) {
	records, err := s.records.ListByCertificate(ctx, certID)
	if err != nil {
		return false, err
	}
	for _, rec := range records {
		if rec.Status == domain.DeployRunning {
			return true, nil
		}
	}
	return false, nil
}

func (s *DeployService) deployOne(ctx context.Context, cert *domain.Certificate, targetID string, payload deploy.Payload, priorRetries int) {
	target, err := s.targets.GetByID(ctx, targetID)
	if err != nil {
		// 目标已被删除（如续期后的自动部署指向已删目标）：仍落失败记录并通知，
		// 否则用户以为部署成功
		s.recordFailure(ctx, cert, targetID, targetID, "部署目标不存在（可能已被删除）")
		return
	}
	if !target.Enabled {
		s.record(ctx, target, cert, fmt.Errorf("部署目标已停用"))
		return
	}
	adapter, ok := s.adapters.Get(string(target.Type))
	if !ok {
		s.record(ctx, target, cert, fmt.Errorf("未注册的部署类型 %s", target.Type))
		return
	}
	cfg, err := s.targetConfig(target)
	if err != nil {
		s.record(ctx, target, cert, err)
		return
	}
	// 注入证书与目标 ID（agent 等异步适配器需要）
	cfg["_cert_id"] = cert.ID
	cfg["_target_id"] = targetID
	record := &domain.DeployRecord{
		ID:            newID(),
		TargetID:      targetID,
		CertificateID: cert.ID,
		Status:        domain.DeployRunning,
		Retries:       priorRetries + 1,
		CreatedAt:     s.clock.Now(),
	}
	_ = s.records.Create(ctx, record)
	s.executeDeploy(ctx, cert, target, adapter, cfg, payload, record)
}

// executeDeploy 执行部署并更新记录/目标/审计（deployOne 与 RetryFailed 共用）
func (s *DeployService) executeDeploy(ctx context.Context, cert *domain.Certificate, target *domain.DeployTarget,
	adapter deploy.Adapter, cfg map[string]any, payload deploy.Payload, record *domain.DeployRecord) {
	result, err := adapter.Deploy(ctx, payload, cfg)
	if err != nil {
		_ = s.records.UpdateResult(ctx, record.ID, domain.DeployFailed, "", err.Error(), nil)
		s.auditAction(ctx, "deploy.failed", cert.PrimaryDomain+" -> "+target.Name)
		s.publishDeployEvent(EventDeployFailed, cert, target.Name, err.Error())
		return
	}
	now := s.clock.Now()
	// 异步适配器（agent 通道）：保持 running，等待 agent 回执后联动更新
	if adapter.IsAsync() {
		_ = s.records.UpdateResult(ctx, record.ID, domain.DeployRunning, "", result.Message, nil)
		return
	}
	_ = s.records.UpdateResult(ctx, record.ID, domain.DeploySuccess, result.Checksum, result.Message, &now)
	target.LastStatus = "success"
	target.LastError = ""
	target.UpdatedAt = now
	_ = s.targets.Update(ctx, target)
	s.auditAction(ctx, "deploy.success", cert.PrimaryDomain+" -> "+target.Name)
	s.publishDeployEvent(EventDeploySuccess, cert, target.Name, "")
}

// publishDeployEvent 发布部署事件（通知订阅）
func (s *DeployService) publishDeployEvent(name string, cert *domain.Certificate, targetName, errMsg string) {
	if s.bus != nil {
		s.bus.Publish(name, &DeployEvent{Cert: cert, TargetName: targetName, Error: errMsg})
	}
}

func (s *DeployService) record(ctx context.Context, target *domain.DeployTarget, cert *domain.Certificate, err error) {
	s.recordFailure(ctx, cert, target.ID, target.Name, err.Error())
}

// recordFailure 落失败部署记录并发布失败事件（target 已删除时仅有 ID）
func (s *DeployService) recordFailure(ctx context.Context, cert *domain.Certificate, targetID, targetName, msg string) {
	rec := &domain.DeployRecord{
		ID:            newID(),
		TargetID:      targetID,
		CertificateID: cert.ID,
		Status:        domain.DeployFailed,
		Message:       msg,
		CreatedAt:     s.clock.Now(),
	}
	_ = s.records.Create(ctx, rec)
	s.publishDeployEvent(EventDeployFailed, cert, targetName, msg)
}

// RetryFailed 重试失败的部署记录（供调度器调用）。
// 原子抢占原记录（retries+1 → running），复用同一记录，避免每次重试生成新记录导致无限增长
func (s *DeployService) RetryFailed(ctx context.Context) error {
	records, err := s.records.ListFailedPending(ctx)
	if err != nil {
		return err
	}
	for _, rec := range records {
		// 抢占：仅成功将 failed→running 且 retries<3 的记录可重试（并发实例互斥）
		if err := s.records.MarkRetrying(ctx, rec.ID); err != nil {
			continue
		}
		// MarkRetrying 已递增 retries，重读最新记录避免用快照旧值覆盖（retries 上限失效 → 无限重试）
		fresh, err := s.records.GetByID(ctx, rec.ID)
		if err != nil {
			continue
		}
		rec = fresh
		cert, err := s.certs.GetByID(ctx, rec.CertificateID)
		if err != nil || cert.CertificatePEM == "" {
			rec.Status = domain.DeployFailed
			rec.Message = "证书不存在或内容缺失"
			_ = s.records.Update(ctx, rec)
			continue
		}
		priv, err := s.cryptor.DecryptString(cert.PrivateKeyEnc)
		if err != nil {
			rec.Status = domain.DeployFailed
			rec.Message = "私钥解密失败"
			_ = s.records.Update(ctx, rec)
			continue
		}
		target, err := s.targets.GetByID(ctx, rec.TargetID)
		if err != nil || !target.Enabled {
			rec.Status = domain.DeployFailed
			rec.Message = "部署目标不存在或已停用"
			_ = s.records.Update(ctx, rec)
			continue
		}
		adapter, ok := s.adapters.Get(string(target.Type))
		if !ok {
			rec.Status = domain.DeployFailed
			rec.Message = "部署类型未注册"
			_ = s.records.Update(ctx, rec)
			continue
		}
		cfg, err := s.targetConfig(target)
		if err != nil {
			rec.Status = domain.DeployFailed
			rec.Message = err.Error()
			_ = s.records.Update(ctx, rec)
			continue
		}
		cfg["_cert_id"] = cert.ID
		cfg["_target_id"] = rec.TargetID
		s.executeDeploy(ctx, cert, target, adapter, cfg, deploy.Payload{
			Domains:        cert.Domains,
			PrimaryDomain:  cert.PrimaryDomain,
			CertificatePEM: cert.CertificatePEM,
			PrivateKeyPEM:  priv,
		}, rec)
	}
	return nil
}

// Records 证书部署记录
func (s *DeployService) Records(ctx context.Context, certID string) ([]*domain.DeployRecord, error) {
	return s.records.ListByCertificate(ctx, certID)
}

// ClearRecords 证书删除时清理其部署记录
func (s *DeployService) ClearRecords(ctx context.Context, certID string) error {
	return s.records.DeleteByCertificate(ctx, certID)
}

func (s *DeployService) targetConfig(target *domain.DeployTarget) (map[string]any, error) {
	plain, err := s.cryptor.DecryptString(target.ConfigEnc)
	if err != nil {
		return nil, fmt.Errorf("解密目标配置失败: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = map[string]any{} // 空配置目标（如 local）
	}
	return cfg, nil
}

func (s *DeployService) auditAction(ctx context.Context, action string, resource string) {
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    domain.AuditAction(action),
		Resource:  resource,
		CreatedAt: s.clock.Now(),
	})
}
