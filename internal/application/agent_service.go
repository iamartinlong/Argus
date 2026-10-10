package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
)

type AgentService struct {
	agents    domain.AgentStore
	tasks     domain.AgentTaskStore
	targets   domain.DeployTargetStore
	certs     domain.CertificateStore
	records   domain.DeployRecordStore
	audit     domain.AuditStore
	cryptor   Cryptor
	clock     Clock
	binaryDir string
	bus       *EventBus
	// heartbeatMu 串行化心跳读-改-写，避免并发请求互相覆盖（升级指令残留等）
	heartbeatMu sync.Mutex
}

func NewAgentService(
	agents domain.AgentStore,
	tasks domain.AgentTaskStore,
	targets domain.DeployTargetStore,
	certs domain.CertificateStore,
	records domain.DeployRecordStore,
	audit domain.AuditStore,
	cryptor Cryptor,
	clock Clock,
	bus *EventBus,
) *AgentService {
	return &AgentService{
		agents: agents, tasks: tasks, targets: targets, certs: certs, records: records,
		audit: audit, cryptor: cryptor, clock: clock, bus: bus,
	}
}

// SetBinaryDir 配置 Agent 二进制存储目录（中心端上传的新版本）
func (s *AgentService) SetBinaryDir(dir string) { s.binaryDir = dir }

// ---------------- Agent 自升级 ----------------

// UpgradeInfo 下发升级指令（agent 查询）
type UpgradeInfo struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// validVersion 校验版本字符串（防路径遍历：仅允许字母数字 . _ -）
func validVersion(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	for _, c := range version {
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') &&
			!(c >= '0' && c <= '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// UploadBinary 上传新版本二进制（admin），返回 SHA256；哈希落盘（防上传后文件篡改）
func (s *AgentService) UploadBinary(ctx context.Context, version string, data []byte) (string, error) {
	if !validVersion(version) {
		return "", fmt.Errorf("%w: 非法版本号", domain.ErrInvalidArgument)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("%w: 文件内容不能为空", domain.ErrInvalidArgument)
	}
	if s.binaryDir == "" {
		return "", fmt.Errorf("Agent 二进制目录未配置")
	}
	if err := os.MkdirAll(s.binaryDir, 0o750); err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	sha := hex.EncodeToString(hash[:])
	if err := os.WriteFile(filepath.Join(s.binaryDir, version), data, 0o750); err != nil {
		return "", err
	}
	// 哈希落盘：后续下载校验与上传时一致（文件被篡改即校验失败）。
	// 写失败必须整体失败：否则 agent 升级时无哈希会跳过完整性校验（fail-open）
	if err := os.WriteFile(filepath.Join(s.binaryDir, version+".sha256"), []byte(sha), 0o640); err != nil {
		return "", fmt.Errorf("写入哈希文件失败: %w", err)
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    "agent.binary.upload",
		Resource:  version,
		Detail:    "sha256=" + sha[:16],
		CreatedAt: s.clock.Now(),
	})
	return sha, nil
}

// ListBinaries 已上传的 Agent 版本列表
func (s *AgentService) ListBinaries(ctx context.Context) ([]map[string]any, error) {
	if s.binaryDir == "" {
		return []map[string]any{}, nil
	}
	entries, err := os.ReadDir(s.binaryDir)
	if err != nil {
		return []map[string]any{}, nil
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) > 7 && e.Name()[len(e.Name())-7:] == ".sha256" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		sha := ""
		if b, err := os.ReadFile(filepath.Join(s.binaryDir, e.Name()+".sha256")); err == nil {
			sha = string(bytes.TrimSpace(b))
		}
		out = append(out, map[string]any{
			"version": e.Name(), "size": info.Size(), "sha256": sha, "created_at": info.ModTime().Format(time.RFC3339),
		})
	}
	return out, nil
}

// BinaryData 读取指定版本的二进制（校验版本格式，防路径遍历）
func (s *AgentService) BinaryData(ctx context.Context, version string) ([]byte, error) {
	if !validVersion(version) {
		return nil, fmt.Errorf("%w: 非法版本号", domain.ErrInvalidArgument)
	}
	if s.binaryDir == "" {
		return nil, fmt.Errorf("Agent 二进制目录未配置")
	}
	return os.ReadFile(filepath.Join(s.binaryDir, version))
}

// RequestUpgrade 向指定 agent 下发升级指令（admin）— 幂等：同版本重复下发不刷新
func (s *AgentService) RequestUpgrade(ctx context.Context, agentID, version string) error {
	if !validVersion(version) {
		return fmt.Errorf("%w: 非法版本号", domain.ErrInvalidArgument)
	}
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return err
	}
	if agent.Version == version {
		return nil
	}
	if agent.UpgradeVersion == version {
		return nil
	}
	if _, err := os.Stat(filepath.Join(s.binaryDir, version)); err != nil {
		return fmt.Errorf("%w: 版本 %s 未上传", domain.ErrInvalidArgument, version)
	}
	sha := s.storedSHA256(version)
	if sha == "" {
		return fmt.Errorf("%w: 版本 %s 缺少哈希记录，拒绝下发升级（防完整性校验绕过）", domain.ErrInvalidArgument, version)
	}
	agent.UpgradeVersion = version
	// 固定上传时的哈希（防上传后文件被篡改）
	agent.UpgradeSHA256 = sha
	now := s.clock.Now()
	agent.UpgradeRequestedAt = &now
	agent.UpdatedAt = now
	if err := s.agents.Update(ctx, agent); err != nil {
		return err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    "agent.upgrade.request",
		Resource:  agent.Name,
		Detail:    "version=" + version,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

// storedSHA256 读取上传时落盘的哈希
func (s *AgentService) storedSHA256(version string) string {
	data, err := os.ReadFile(filepath.Join(s.binaryDir, version+".sha256"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// QueryUpgrade agent 查询是否有升级指令（附带 SHA256）
func (s *AgentService) QueryUpgrade(ctx context.Context, agentID string) (*UpgradeInfo, error) {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.UpgradeVersion == "" {
		return nil, nil
	}
	info := &UpgradeInfo{Version: agent.UpgradeVersion}
	if agent.UpgradeSHA256 != "" {
		info.SHA256 = agent.UpgradeSHA256
	} else {
		// 兼容旧指令：回退到落盘哈希；仍缺失则拒绝下发（fail-closed）
		info.SHA256 = s.storedSHA256(agent.UpgradeVersion)
	}
	if info.SHA256 == "" {
		return nil, fmt.Errorf("升级指令缺少 SHA256，拒绝下发")
	}
	return info, nil
}

// ResolveUpgrade agent 上报新版本后清理升级指令（版本一致视为升级完成）
func (s *AgentService) ResolveUpgrade(ctx context.Context, agentID string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return err
	}
	if agent.UpgradeVersion != "" && agent.UpgradeVersion == agent.Version {
		agent.UpgradeVersion = ""
		agent.UpgradeSHA256 = ""
		agent.UpgradeRequestedAt = nil
		agent.UpdatedAt = s.clock.Now()
		_ = s.agents.Update(ctx, agent)
	}
	return nil
}

// CreateAgent 管理员创建 agent 记录，返回明文 token（仅一次）
func (s *AgentService) CreateAgent(ctx context.Context, name string) (*domain.Agent, string, error) {
	if name == "" {
		return nil, "", fmt.Errorf("%w: Agent 名称不能为空", domain.ErrInvalidArgument)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	tokenStr := "ag_" + hex.EncodeToString(raw)
	agent := &domain.Agent{
		ID:        newID(),
		Name:      name,
		TokenHash: HashToken(tokenStr),
		Status:    "offline",
		Enabled:   true,
		CreatedAt: s.clock.Now(),
		UpdatedAt: s.clock.Now(),
	}
	if err := s.agents.Create(ctx, agent); err != nil {
		return nil, "", err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    "agent.create",
		Resource:  name,
		CreatedAt: s.clock.Now(),
	})
	return agent, tokenStr, nil
}

func (s *AgentService) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	return s.agents.List(ctx)
}

func (s *AgentService) DeleteAgent(ctx context.Context, id string) error {
	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.agents.Delete(ctx, id); err != nil {
		return err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    "agent.delete",
		Resource:  agent.Name,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

// SetEnabled 启用/禁用 Agent（软白名单：禁用后 Token 立即失效、置离线）
func (s *AgentService) SetEnabled(ctx context.Context, agentID string, enabled bool) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return err
	}
	if agent.Enabled == enabled {
		return nil
	}
	agent.Enabled = enabled
	agent.UpdatedAt = s.clock.Now()
	if !enabled {
		// 禁用：立即置离线（心跳不再更新）
		agent.Status = "offline"
	}
	if err := s.agents.Update(ctx, agent); err != nil {
		return err
	}
	action := "agent.enable"
	if !enabled {
		action = "agent.disable"
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    domain.AuditAction(action),
		Resource:  agent.Name,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

// ReconcileStaleOffline 将心跳超时（timeout 内无心跳）的在线 Agent 置为 offline
func (s *AgentService) ReconcileStaleOffline(ctx context.Context, timeout time.Duration) (int64, error) {
	cutoff := s.clock.Now().Add(-timeout)
	return s.agents.MarkStaleOffline(ctx, cutoff)
}

// Authenticate 校验 agent token，更新心跳（在线状态）
func (s *AgentService) Authenticate(ctx context.Context, rawToken, version, osName string) (*domain.Agent, error) {
	if rawToken == "" {
		return nil, domain.ErrUnauthorized
	}
	agent, err := s.agents.GetByHash(ctx, HashToken(rawToken))
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	// 软白名单：被禁用的 Agent 拒绝全部请求
	if !agent.Enabled {
		return nil, domain.ErrUnauthorized
	}
	// 心跳是读-改-写：并发请求（register + 轮询）会互相覆盖（例如刚清空的升级指令
	// 被携带旧快照的请求写回），故串行化后再读一次最新状态
	s.heartbeatMu.Lock()
	defer s.heartbeatMu.Unlock()
	agent, err = s.agents.GetByHash(ctx, HashToken(rawToken))
	if err != nil || !agent.Enabled {
		return nil, domain.ErrUnauthorized
	}
	now := s.clock.Now()
	agent.LastSeenAt = &now
	agent.Status = "online"
	// 注册/心跳携带版本与系统信息；轮询等场景为空时保留原值
	if version != "" {
		agent.Version = version
	}
	if osName != "" {
		agent.OS = osName
	}
	agent.UpdatedAt = now
	// 升级完成感知：上报版本与升级目标一致则清理升级指令
	if agent.UpgradeVersion != "" && agent.UpgradeVersion == version {
		agent.UpgradeVersion = ""
		agent.UpgradeSHA256 = ""
		agent.UpgradeRequestedAt = nil
	}
	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// ReconcileStaleAgentTasks 清理超时未回执的 running 任务与从未领取的 pending 任务，并联动将对应部署记录置为失败
//（agent 超时/离线/误绑时部署记录不再卡在「进行中」）。由 agent 轮询（ClaimTask）与调度器调用。
func (s *AgentService) ReconcileStaleAgentTasks(ctx context.Context, olderThan time.Duration) error {
	stale, err := s.tasks.MarkStaleTasks(ctx, olderThan)
	if err != nil {
		return err
	}
	if s.records == nil {
		return nil
	}
	for _, task := range stale {
		if task.CertificateID == "" || task.TargetID == "" {
			continue
		}
		// affected 为 UPDATE 前快照：task.Status 仍是 pending/running，据此区分失败原因
		msg := "agent 超时未回执"
		if task.Status == domain.AgentTaskPending {
			msg = "agent 未领取超时"
		}
		_, _ = s.records.MarkTimedOut(ctx, task.CertificateID, task.TargetID, msg)
	}
	return nil
}

// ClaimTask agent 领取下一个任务（返回动态载荷）
// 领取前惰性清理超时未回执的 running 任务（不依赖调度器开关）
func (s *AgentService) ClaimTask(ctx context.Context, agentID string) (*domain.AgentTaskPayload, error) {
	_ = s.ReconcileStaleAgentTasks(ctx, 10*time.Minute)
	task, err := s.tasks.ClaimNext(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, nil
	}
	cert, err := s.certs.GetByID(ctx, task.CertificateID)
	if err != nil {
		return nil, err
	}
	target, err := s.targets.GetByID(ctx, task.TargetID)
	if err != nil {
		return nil, err
	}
	cfg, err := decryptTargetConfig(target, s.cryptor)
	if err != nil {
		return nil, err
	}
	priv, err := s.cryptor.DecryptString(cert.PrivateKeyEnc)
	if err != nil {
		return nil, err
	}
	payload := &domain.AgentTaskPayload{
		TaskID:         task.ID,
		CertificatePEM: cert.CertificatePEM,
		PrivateKeyPEM:  priv,
	}
	if v, ok := cfg["cert_path"].(string); ok {
		payload.CertPath = v
	}
	if v, ok := cfg["key_path"].(string); ok {
		payload.KeyPath = v
	}
	if v, ok := cfg["hook"].(string); ok {
		payload.Hook = v
	}
	if v, ok := cfg["is_windows"].(bool); ok {
		payload.IsWindows = v
	}
	// Nginx SNI：路径支持 {domain} 占位
	if scene, _ := cfg["scene"].(string); scene == "nginx" {
		if payload.CertPath != "" {
			payload.CertPath = strings.ReplaceAll(payload.CertPath, "{domain}", cert.PrimaryDomain)
		}
		if payload.KeyPath != "" {
			payload.KeyPath = strings.ReplaceAll(payload.KeyPath, "{domain}", cert.PrimaryDomain)
		}
		if payload.Hook == "" {
			payload.Hook = "nginx -t && nginx -s reload"
		}
	}
	// IIS 场景：服务端合成 PFX 附带（agent 仅下载写入），不落盘中心端
	pfxPath := ""
	if v, ok := cfg["pfx_path"].(string); ok {
		pfxPath = v
	}
	if pfxPath != "" {
		pfxPassword, err := randomPassword(16)
		if err != nil {
			return nil, fmt.Errorf("生成 PFX 密码: %w", err)
		}
		pfxData, err := deploy.BuildPFX(cert.CertificatePEM, priv, pfxPassword)
		if err != nil {
			return nil, fmt.Errorf("合成 PFX: %w", err)
		}
		payload.PfxPath = pfxPath
		payload.PfxData = base64.StdEncoding.EncodeToString(pfxData)
		payload.PfxPassword = pfxPassword
		// IIS 场景自动生成完整部署脚本（appcmd 站点绑定 + SNI），用户未手写 hook 时使用
		if payload.Hook == "" {
			bindings := parseIntSlice(cfg["bindings"])
			siteName, _ := cfg["site_name"].(string)
			hostname, _ := cfg["hostname"].(string)
			if hostname == "" {
				hostname = cert.PrimaryDomain
			}
			if len(bindings) > 0 {
				payload.Hook = deploy.RenderIISAgentScriptV2(pfxPath, pfxPassword, bindings, siteName, hostname)
				payload.IsWindows = true
			}
		}
	}
	// hook 模板渲染：支持 {pfx_path} {pfx_password} {cert_path} {key_path} {domain} {iis_bindings} 变量
	//（IIS 场景 hook 可引用服务端生成的动态 PFX 密码）
	if payload.Hook != "" {
		payload.Hook = renderAgentHook(payload.Hook, payload, cert.PrimaryDomain)
	}
	return payload, nil
}

// parseIntSlice 解析 config 中的端口数组（支持 []any / []float64 / []int）
func parseIntSlice(v any) []int {
	switch arr := v.(type) {
	case []any:
		out := make([]int, 0, len(arr))
		for _, item := range arr {
			switch n := item.(type) {
			case float64:
				out = append(out, int(n))
			case int:
				out = append(out, n)
			}
		}
		return out
	case []float64:
		out := make([]int, 0, len(arr))
		for _, n := range arr {
			out = append(out, int(n))
		}
		return out
	case []int:
		return arr
	}
	return nil
}

// renderAgentHook 替换 hook 模板变量
func renderAgentHook(tpl string, payload *domain.AgentTaskPayload, primaryDomain string) string {
	r := strings.NewReplacer(
		deploy.VarCertPath, payload.CertPath,
		deploy.VarKeyPath, payload.KeyPath,
		deploy.VarDomain, primaryDomain,
		"{pfx_path}", payload.PfxPath,
		"{pfx_password}", payload.PfxPassword,
	)
	return r.Replace(tpl)
}

// SubmitResult agent 回传任务结果（同步更新源部署记录）
func (s *AgentService) SubmitResult(ctx context.Context, agentID, taskID string, ok bool, checksum, message string) error {
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task.AgentID != agentID {
		return domain.ErrForbidden
	}
	now := s.clock.Now()
	task.Checksum = checksum
	task.Message = message
	task.FinishedAt = &now
	if ok {
		task.Status = domain.AgentTaskSuccess
	} else {
		task.Status = domain.AgentTaskFailed
	}
	if err := s.tasks.Update(ctx, task); err != nil {
		return err
	}
	// 同步更新源部署记录（agent 适配器排队时创建）
	if s.records != nil && task.TargetID != "" {
		records, err := s.records.ListByCertificate(ctx, task.CertificateID)
		if err == nil {
			for _, rec := range records {
				if rec.TargetID == task.TargetID && rec.Status == domain.DeployRunning {
					status := domain.DeploySuccess
					if !ok {
						status = domain.DeployFailed
					}
					// UpdateResult 不覆盖 retries，避免回写旧快照导致重试计数失效
					_ = s.records.UpdateResult(ctx, rec.ID, status, checksum, message, &now)
					s.publishDeployResult(ctx, status, task.CertificateID, task.TargetID, ok, message)
					break
				}
			}
		}
	}
	return nil
}

// publishDeployResult agent 异步部署终态发布事件（通知订阅）
func (s *AgentService) publishDeployResult(ctx context.Context, status domain.DeployRecordStatus, certID, targetID string, ok bool, message string) {
	if s.bus == nil {
		return
	}
	cert, err := s.certs.GetByID(ctx, certID)
	if err != nil {
		return
	}
	targetName := targetID
	if s.targets != nil {
		if t, terr := s.targets.GetByID(ctx, targetID); terr == nil {
			targetName = t.Name
		}
	}
	name := EventDeploySuccess
	if !ok {
		name = EventDeployFailed
	}
	s.bus.Publish(name, &DeployEvent{Cert: cert, TargetName: targetName, Error: message})
}

// QueueDeploy 为 agent 目标排队部署任务（供 agent 部署适配器调用）
func (s *AgentService) QueueDeploy(ctx context.Context, agentID, certID, targetID string) error {
	certDomain := ""
	if s.certs != nil {
		if cert, err := s.certs.GetByID(ctx, certID); err == nil {
			certDomain = cert.PrimaryDomain
			if certDomain == "" && len(cert.Domains) > 0 {
				certDomain = cert.Domains[0]
			}
		}
	}
	task := &domain.AgentTask{
		ID:                newID(),
		AgentID:           agentID,
		CertificateID:     certID,
		CertificateDomain: certDomain,
		TargetID:          targetID,
		Status:            domain.AgentTaskPending,
		CreatedAt:         s.clock.Now(),
	}
	return s.tasks.Create(ctx, task)
}

// AgentAdapter 实现 deploy.Adapter：部署=排队任务，由 agent 出站拉取执行
type AgentAdapter struct {
	agents *AgentService
	clock  Clock
}

func NewAgentAdapter(agents *AgentService, clock Clock) *AgentAdapter {
	return &AgentAdapter{agents: agents, clock: clock}
}

func (a *AgentAdapter) Type() string { return "agent" }

func (a *AgentAdapter) IsAsync() bool { return true }

func (a *AgentAdapter) Deploy(ctx context.Context, payload deploy.Payload, config map[string]any) (*deploy.Result, error) {
	agentID, ok := config["agent_id"].(string)
	if !ok || agentID == "" {
		return nil, fmt.Errorf("agent 目标缺少 agent_id")
	}
	// DeployService 注入证书与目标 ID
	certID, _ := config["_cert_id"].(string)
	targetID, _ := config["_target_id"].(string)
	if certID == "" {
		return nil, fmt.Errorf("agent 部署缺少证书 ID")
	}
	if _, err := a.agents.GetAgent(ctx, agentID); err != nil {
		return nil, fmt.Errorf("agent 不存在: %w", err)
	}
	if err := a.agents.QueueDeploy(ctx, agentID, certID, targetID); err != nil {
		return nil, err
	}
	return &deploy.Result{Checksum: deploy.ChecksumOf(payload), Message: "已排队等待 agent 拉取执行"}, nil
}

func (a *AgentAdapter) Test(ctx context.Context, config map[string]any) error {
	agentID, ok := config["agent_id"].(string)
	if !ok || agentID == "" {
		return fmt.Errorf("agent 目标缺少 agent_id")
	}
	agent, err := a.agents.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("agent 不存在: %w", err)
	}
	if agent.Status != "online" {
		return fmt.Errorf("agent %s 当前离线", agent.Name)
	}
	return nil
}

// GetAgent 供 adapter 与外部查询
func (s *AgentService) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	return s.agents.GetByID(ctx, id)
}

// RenameAgent 重命名 Agent
func (s *AgentService) RenameAgent(ctx context.Context, id, name string) (*domain.Agent, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: Agent 名称不能为空", domain.ErrInvalidArgument)
	}
	agent, err := s.agents.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	agent.Name = name
	agent.UpdatedAt = s.clock.Now()
	if err := s.agents.Update(ctx, agent); err != nil {
		return nil, err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Action:    "agent.update",
		Resource:  agent.Name,
		CreatedAt: s.clock.Now(),
	})
	return agent, nil
}

// TaskHistory 某 Agent 的任务记录（倒序，最近 50 条）
func (s *AgentService) TaskHistory(ctx context.Context, agentID string) ([]*domain.AgentTask, error) {
	if _, err := s.agents.GetByID(ctx, agentID); err != nil {
		return nil, err
	}
	return s.tasks.ListByAgent(ctx, agentID, 50)
}

func decryptTargetConfig(target *domain.DeployTarget, cryptor Cryptor) (map[string]any, error) {
	plain, err := cryptor.DecryptString(target.ConfigEnc)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// randomPassword 生成随机密码（PFX 等场景）。
// 用拒绝采样消除取模偏置，并传播随机源错误（失败时不能退化成可预测密码）。
func randomPassword(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// 256 - 256%len(chars)：此上界内的字节取模分布均匀
	const limit = 256 - 256%len(chars)
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, chars[int(b)%len(chars)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
