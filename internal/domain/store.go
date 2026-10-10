package domain

import (
	"context"
	"time"
)

// Store 接口由 application 层消费者定义，infrastructure 实现。
type UserStore interface {
	GetByID(ctx context.Context, id string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
}

type SessionStore interface {
	Create(ctx context.Context, session *RefreshSession) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*RefreshSession, error)
	Revoke(ctx context.Context, tokenHash string) error
	RevokeAllByUser(ctx context.Context, userID string) error
}

type AuditStore interface {
	Append(ctx context.Context, log *AuditLog) error
	List(ctx context.Context, filter AuditQuery, limit, offset int) ([]*AuditLog, error)
	// Count 带筛选条件的总数（分页元信息）
	Count(ctx context.Context, filter AuditQuery) (int64, error)
	// DeleteOlderThan 删除早于指定时间的日志（保留策略）
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)
}

type CertificateStore interface {
	Create(ctx context.Context, cert *Certificate) error
	GetByID(ctx context.Context, id string) (*Certificate, error)
	List(ctx context.Context) ([]*Certificate, error)
	Update(ctx context.Context, cert *Certificate) error
	// UpdateStatus 仅更新状态列（扫描持久化过渡态，避免整行覆盖并发写）
	UpdateStatus(ctx context.Context, id string, status CertificateStatus) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
	CountExpiring(ctx context.Context, within time.Duration) (int64, error)
	CountExpired(ctx context.Context) (int64, error)
	CountByAccount(ctx context.Context, accountID string) (int64, error)
}

type OrderStore interface {
	Create(ctx context.Context, order *Order) error
	GetByID(ctx context.Context, id string) (*Order, error)
	ListByCertificate(ctx context.Context, certID string) ([]*Order, error)
	Update(ctx context.Context, order *Order) error
	// RenewStats 统计指定时间段后的续期成功/总数
	RenewStats(ctx context.Context, since time.Time) (success, total int64, err error)
	// RenewalTrend 按月的续期统计（含空月补零）
	RenewalTrend(ctx context.Context, since time.Time) ([]*RenewalTrendPoint, error)
}

// RenewalTrendPoint 单月续期统计
type RenewalTrendPoint struct {
	Month   time.Time // 当月 1 日
	Success int
	Total   int
}

type ProviderAccountStore interface {
	Create(ctx context.Context, account *ProviderAccount) error
	GetByID(ctx context.Context, id string) (*ProviderAccount, error)
	List(ctx context.Context) ([]*ProviderAccount, error)
	Update(ctx context.Context, account *ProviderAccount) error
	Delete(ctx context.Context, id string) error
}

type DnsProviderAccountStore interface {
	Create(ctx context.Context, account *DnsProviderAccount) error
	GetByID(ctx context.Context, id string) (*DnsProviderAccount, error)
	List(ctx context.Context) ([]*DnsProviderAccount, error)
	Update(ctx context.Context, account *DnsProviderAccount) error
	Delete(ctx context.Context, id string) error
}

type ContactStore interface {
	Create(ctx context.Context, contact *ContactPerson) error
	GetByID(ctx context.Context, id string) (*ContactPerson, error)
	List(ctx context.Context) ([]*ContactPerson, error)
	Update(ctx context.Context, contact *ContactPerson) error
	Delete(ctx context.Context, id string) error
}

type NotificationRuleStore interface {
	Create(ctx context.Context, rule *NotificationRule) error
	GetByID(ctx context.Context, id string) (*NotificationRule, error)
	List(ctx context.Context) ([]*NotificationRule, error)
	ListByEvent(ctx context.Context, event NotificationEvent) ([]*NotificationRule, error)
	Update(ctx context.Context, rule *NotificationRule) error
	Delete(ctx context.Context, id string) error
}

type NotificationChannelStore interface {
	Create(ctx context.Context, channel *NotificationChannelRecord) error
	GetByID(ctx context.Context, id string) (*NotificationChannelRecord, error)
	List(ctx context.Context) ([]*NotificationChannelRecord, error)
	Update(ctx context.Context, channel *NotificationChannelRecord) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
}

type NotificationLogStore interface {
	Append(ctx context.Context, log *NotificationLog) error
	// HasSent 检查某证书在某提前天数下是否已提醒
	HasSent(ctx context.Context, certID string, ruleID string, daysBefore int) (bool, error)
	// DeleteOlderThan 删除早于指定时间的防重记录（保留策略）
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)
}

// NotificationSendLogStore 通知发送历史
type NotificationSendLogStore interface {
	Append(ctx context.Context, log *NotificationSendLog) error
	// List 按条件倒序分页（filter 字段为空表示不筛选）
	List(ctx context.Context, filter NotificationSendLogQuery, limit, offset int) ([]*NotificationSendLog, error)
	// Count 带筛选条件的总数（分页元信息）
	Count(ctx context.Context, filter NotificationSendLogQuery) (int64, error)
	// DeleteOlderThan 删除早于指定时间的记录（保留策略），返回删除条数
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)
	// CountByStatus 按发送结果计数（status: success | failed）
	CountByStatus(ctx context.Context, status string) (int64, error)
}

// NotificationTemplateStore 通知模板设置
type NotificationTemplateStore interface {
	// Upsert 保存模板（按 event 覆盖）
	Upsert(ctx context.Context, t *NotificationTemplate) error
	// Get 按事件读取；未配置返回 nil, nil
	Get(ctx context.Context, event NotificationEvent) (*NotificationTemplate, error)
	// List 返回全部已配置模板
	List(ctx context.Context) ([]*NotificationTemplate, error)
	// Delete 删除模板配置（回退默认文案）
	Delete(ctx context.Context, event NotificationEvent) error
}

// NotificationSendLogQuery 发送历史筛选条件
type NotificationSendLogQuery struct {
	ChannelID string
	Status    string // success | failed
	Keyword   string // 规则名/渠道名/证书ID模糊搜索
}

type TaskLogStore interface {
	Create(ctx context.Context, log *TaskLog) error
	Update(ctx context.Context, log *TaskLog) error
	List(ctx context.Context, limit, offset int) ([]*TaskLog, error)
	Count(ctx context.Context) (int64, error)
	// DeleteOlderThan 删除早于指定时间的任务日志（保留策略）
	DeleteOlderThan(ctx context.Context, before time.Time) (int64, error)
}

type DeployTargetStore interface {
	Create(ctx context.Context, target *DeployTarget) error
	GetByID(ctx context.Context, id string) (*DeployTarget, error)
	List(ctx context.Context) ([]*DeployTarget, error)
	Update(ctx context.Context, target *DeployTarget) error
	Delete(ctx context.Context, id string) error
}

type DeployRecordStore interface {
	Create(ctx context.Context, record *DeployRecord) error
	GetByID(ctx context.Context, id string) (*DeployRecord, error)
	ListByCertificate(ctx context.Context, certID string) ([]*DeployRecord, error)
	Update(ctx context.Context, record *DeployRecord) error
	// UpdateResult 仅更新执行结果字段（不覆盖 retries，避免重试计数被旧值回写）
	UpdateResult(ctx context.Context, id string, status DeployRecordStatus, checksum, message string, deployedAt *time.Time) error
	ListFailedPending(ctx context.Context) ([]*DeployRecord, error)
	DeleteByCertificate(ctx context.Context, certID string) error
	DeleteByTarget(ctx context.Context, targetID string) error
	// MarkRetrying 原子抢占失败记录用于重试：failed→running 且 retries+1（仅 retries<3 生效）
	MarkRetrying(ctx context.Context, id string) error
	// MarkTimedOut 将 agent 超时/未领取对应的 running 部署记录置为 failed（retries 置顶排除自动重试）
	MarkTimedOut(ctx context.Context, certID, targetID, message string) (int64, error)
	// CountByStatus 按状态计数（status: success | failed | pending | running）
	CountByStatus(ctx context.Context, status string) (int64, error)
}

type ApiTokenStore interface {
	Create(ctx context.Context, token *ApiToken) error
	GetByID(ctx context.Context, id string) (*ApiToken, error)
	GetByHash(ctx context.Context, tokenHash string) (*ApiToken, error)
	ListByUser(ctx context.Context, userID string) ([]*ApiToken, error)
	Delete(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string) error
}

type AgentStore interface {
	Create(ctx context.Context, agent *Agent) error
	GetByID(ctx context.Context, id string) (*Agent, error)
	GetByHash(ctx context.Context, tokenHash string) (*Agent, error)
	List(ctx context.Context) ([]*Agent, error)
	Update(ctx context.Context, agent *Agent) error
	Delete(ctx context.Context, id string) error
	// CountByStatus 按状态计数（status: online | offline | disabled）
	CountByStatus(ctx context.Context, status string) (int64, error)
	// MarkStaleOffline 将心跳超时的在线 Agent 置为 offline（返回受影响数量）
	MarkStaleOffline(ctx context.Context, olderThan time.Time) (int64, error)
}

type AgentTaskStore interface {
	Create(ctx context.Context, task *AgentTask) error
	GetByID(ctx context.Context, id string) (*AgentTask, error)
	// ListByAgent 某 Agent 的任务记录（按时间倒序）
	ListByAgent(ctx context.Context, agentID string, limit int) ([]*AgentTask, error)
	// ClaimNext 领取一个 pending 任务（原子更新为 running）
	ClaimNext(ctx context.Context, agentID string) (*AgentTask, error)
	Update(ctx context.Context, task *AgentTask) error
	// MarkStaleTasks 将超时未回执的 running 任务或从未领取的 pending 任务标记为 failed，返回受影响任务（供联动部署记录）
	MarkStaleTasks(ctx context.Context, olderThan time.Duration) ([]*AgentTask, error)
	// DeleteByTarget 删除指向某部署目标的所有任务（目标删除时级联清理）
	DeleteByTarget(ctx context.Context, targetID string) error
}
