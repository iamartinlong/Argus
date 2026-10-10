package domain

import "time"

// NotificationEvent 通知触发事件
type NotificationEvent string

const (
	EventCertExpiring  NotificationEvent = "cert_expiring"  // 到期前 N 天
	EventRenewSuccess  NotificationEvent = "renew_success"
	EventRenewFailed   NotificationEvent = "renew_failed"
	EventIssueSuccess  NotificationEvent = "issue_success"
	EventIssueFailed   NotificationEvent = "issue_failed"
	EventDeploySuccess NotificationEvent = "deploy_success"
	EventDeployFailed  NotificationEvent = "deploy_failed"
)

func (e NotificationEvent) Valid() bool {
	switch e {
	case EventCertExpiring, EventRenewSuccess, EventRenewFailed,
		EventIssueSuccess, EventIssueFailed, EventDeploySuccess, EventDeployFailed:
		return true
	}
	return false
}

type NotificationChannel string

const (
	ChannelSMTP      NotificationChannel = "smtp"
	ChannelWebhook   NotificationChannel = "webhook"
	ChannelWeCom     NotificationChannel = "wecom"     // 企业微信机器人
	ChannelDingTalk  NotificationChannel = "dingtalk"  // 钉钉机器人
	ChannelFeishu    NotificationChannel = "feishu"    // 飞书机器人
)

func (c NotificationChannel) Valid() bool {
	switch c {
	case ChannelSMTP, ChannelWebhook, ChannelWeCom, ChannelDingTalk, ChannelFeishu:
		return true
	}
	return false
}

type NotificationRule struct {
	ID         string
	Name       string
	Event      NotificationEvent
	Channels   []NotificationChannel // 兼容旧字段：类型名（回填迁移后可弃用）
	ChannelIDs []string              // 引用 notification_channels（界面配置渠道）
	DaysBefore []int // 到期提醒提前天数（仅 cert_expiring 使用）
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NotificationChannelRecord 界面配置的通知渠道（参数加密存储）
type NotificationChannelRecord struct {
	ID        string
	Name      string
	Type      NotificationChannel
	ConfigEnc string // 渠道参数 JSON 的 AES-GCM 密文
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NotificationLog 防重复提醒记录
type NotificationLog struct {
	ID         string
	CertID     string
	RuleID     string
	DaysBefore int
	SentAt     time.Time
}

// NotificationSendLog 通知发送历史（每次发送尝试：成功/失败均记录）
type NotificationSendLog struct {
	ID          string
	CertID      string
	RuleID      string
	RuleName    string
	ChannelID   string
	ChannelName string
	Event       NotificationEvent
	Status      string // success | failed
	Detail      string
	SentAt      time.Time
}

// NotificationTemplate 通知消息模板（全局设置，每事件一条）
type NotificationTemplate struct {
	Event     NotificationEvent
	Title     string
	Body      string
	UpdatedAt time.Time
}

type TaskType string

const (
	TaskScanCertificates TaskType = "scan_certificates"
	TaskAutoRenew        TaskType = "auto_renew"
	TaskExpiryNotify     TaskType = "expiry_notify"
	TaskNotifyRetry      TaskType = "notify_retry"
)

type TaskStatus string

const (
	TaskRunning TaskStatus = "running"
	TaskSuccess TaskStatus = "success"
	TaskFailed  TaskStatus = "failed"
)

type TaskLog struct {
	ID         string
	Type       TaskType
	Status     TaskStatus
	Detail     string
	Summary    string // JSON 序列化 ScanSummary，兼容旧 detail 文本
	StartedAt  time.Time
	FinishedAt *time.Time
}

// ScanSummary 扫描任务结构化摘要（task_logs.summary 聚合视图）
type ScanSummary struct {
	Scanned         int          `json:"scanned"`
	Expiring        []ExpiringInfo `json:"expiring,omitempty"`
	RenewTriggered  int          `json:"renew_triggered"`
	RenewSuccess    []string     `json:"renew_success,omitempty"`
	RenewFailed     []RenewFail  `json:"renew_failed,omitempty"`
	RenewSkipped    []SkippedInfo `json:"renew_skipped,omitempty"`
	NotifySent      int          `json:"notify_sent"`
	DeployRetried   int          `json:"deploy_retried"`
	DeployRetriedOk int          `json:"deploy_retried_ok"`
	AgentCleaned    int          `json:"agent_cleaned"`
	DurationMs      int64        `json:"duration_ms"`
}

type ExpiringInfo struct {
	Domain   string `json:"domain"`
	DaysLeft int    `json:"days_left"`
	Status   string `json:"status"`
	Provider string `json:"provider"`
}

type RenewFail struct {
	Domain string `json:"domain"`
	Error  string `json:"error"`
}

type SkippedInfo struct {
	Domain string `json:"domain"`
	Reason string `json:"reason"`
}

// NotificationMessage 统一通知内容（infrastructure/notify 与 application 共用）
type NotificationMessage struct {
	Title   string
	Body    string
	Details map[string]string
}

// MessageSender 通知渠道（infrastructure/notify 实现）
type MessageSender interface {
	Channel() string
	Send(msg NotificationMessage) error
}

// NotifierBuilder 从渠道配置构建发送器（界面配置渠道动态创建）
type NotifierBuilder interface {
	Build(channelType NotificationChannel, config map[string]any) (MessageSender, error)
}
