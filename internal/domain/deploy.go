package domain

import "time"

type DeployTargetType string

const (
	DeployLocal   DeployTargetType = "local"   // 本机路径 + hook
	DeploySSH     DeployTargetType = "ssh"     // Agentless SSH 推送
	DeployAgent   DeployTargetType = "agent"   // 出站 Agent 通道
	DeployK8s     DeployTargetType = "k8s"     // Kubernetes Secret
	DeployWebhook DeployTargetType = "webhook" // 通用 Webhook
)

// DeployScene 部署场景（目标环境）：决定部署产物格式与动作模板，
// 与连接通道（Type）正交。通用/Nginx/Apache/Tomcat 写 PEM+Key 或 PFX；
// IIS 场景仅 Windows 目标可用（平台属性，非通道限制）。
type DeployScene string

const (
	SceneGeneric  DeployScene = "generic"  // 通用：写文件 + 自定义 hook
	SceneNginx    DeployScene = "nginx"    // Nginx：PEM/Key + reload 模板
	SceneApache   DeployScene = "apache"   // Apache：PEM/Key + reload 模板
	SceneTomcat   DeployScene = "tomcat"   // Tomcat：PFX 推送 + 可选 hook
	SceneIIS      DeployScene = "iis"      // Windows IIS：PFX + 导入 + https 绑定
)

func (s DeployScene) Valid() bool {
	switch s {
	case SceneGeneric, SceneNginx, SceneApache, SceneTomcat, SceneIIS:
		return true
	}
	return false
}

func (t DeployTargetType) Valid() bool {
	switch t {
	case DeployLocal, DeploySSH, DeployAgent, DeployK8s, DeployWebhook:
		return true
	}
	return false
}

type DeployTarget struct {
	ID         string
	Name       string
	Type       DeployTargetType
	ConfigEnc  string // 目标配置 JSON 的 AES-GCM 密文（凭据等敏感信息）
	Enabled    bool
	LastStatus string // success | error
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type DeployRecordStatus string

const (
	DeployRunning DeployRecordStatus = "running"
	DeploySuccess DeployRecordStatus = "success"
	DeployFailed  DeployRecordStatus = "failed"
)

type DeployRecord struct {
	ID           string
	TargetID     string
	CertificateID string
	Status       DeployRecordStatus
	Checksum     string // 部署内容 SHA256
	Message      string
	Retries      int
	DeployedAt   *time.Time
	CreatedAt    time.Time
}
