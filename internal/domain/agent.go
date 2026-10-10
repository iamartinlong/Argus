package domain

import "time"

// Agent 业务机代理（出站连接中心端，解决 NAT/入站限制场景）
type Agent struct {
	ID         string
	Name       string
	TokenHash  string // agent 认证 token 的 sha256
	Status     string // online | offline
	LastSeenAt *time.Time
	Version    string
	OS         string
	Enabled    bool // 启用/禁用（软白名单：禁用后 Token 立即失效）
	// 升级指令（admin 下发）
	UpgradeVersion     string     `json:"-"`
	UpgradeSHA256      string     `json:"-"`
	UpgradeRequestedAt *time.Time `json:"-"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// AgentUpgradeStatus 升级执行状态（agent 上报）
type AgentUpgradeStatus string

const (
	UpgradeIdle       AgentUpgradeStatus = "idle"
	UpgradeDownloaded AgentUpgradeStatus = "downloaded"
	UpgradeApplied    AgentUpgradeStatus = "applied"
	UpgradeFailed     AgentUpgradeStatus = "failed"
)

type AgentTaskStatus string

const (
	AgentTaskPending AgentTaskStatus = "pending"
	AgentTaskRunning AgentTaskStatus = "running"
	AgentTaskSuccess AgentTaskStatus = "success"
	AgentTaskFailed  AgentTaskStatus = "failed"
)

// AgentTask 下发给 agent 的部署任务
type AgentTask struct {
	ID              string
	AgentID         string
	CertificateID   string
	CertificateDomain string // 快照：创建时的 primary_domain
	TargetID        string // 来源部署目标（deploy_targets.id）
	// 部署参数（由中心端在拉取时动态组装，不落库）
	Status     AgentTaskStatus
	Checksum   string
	Message    string
	ClaimedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
}

// AgentTaskPayload 拉取任务时的动态载荷（含解密后的证书内容）
// IIS 场景：服务端合成 PFX 并通过 pfx_data（base64）附带，agent 仅下载写入
type AgentTaskPayload struct {
	TaskID         string `json:"task_id"`
	CertPath       string `json:"cert_path"`
	KeyPath        string `json:"key_path"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	Hook           string `json:"hook"`
	IsWindows      bool   `json:"is_windows"`
	PfxPath        string `json:"pfx_path,omitempty"`
	PfxData        string `json:"pfx_data,omitempty"` // base64，服务端 BuildPFX 合成
	PfxPassword    string `json:"pfx_password,omitempty"`
}
