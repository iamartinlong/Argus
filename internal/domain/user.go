package domain

import (
	"errors"
	"time"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleReadonly Role = "readonly"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleReadonly:
		return true
	default:
		return false
	}
}

var (
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrLocked          = errors.New("account locked")
	ErrMustChangePass  = errors.New("password change required")
)

type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

type User struct {
	ID                 string
	Username           string
	PasswordHash       string
	Role               Role
	Status             UserStatus
	MustChangePassword bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type RefreshSession struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

type AuditAction string

const (
	AuditLoginSuccess  AuditAction = "login.success"
	AuditLoginFailure  AuditAction = "login.failure"
	AuditLogout        AuditAction = "logout"
	AuditRefresh       AuditAction = "refresh"
	AuditUserCreate    AuditAction = "user.create"
	AuditUserUpdate    AuditAction = "user.update"
	AuditUserDelete    AuditAction = "user.delete"
	AuditPassword      AuditAction = "password.change"
	AuditPasswordReset AuditAction = "password.reset"

	AuditCertIssue     AuditAction = "certificate.issue"
	AuditCertRenew     AuditAction = "certificate.renew"
	AuditCertRevoke    AuditAction = "certificate.revoke"
	AuditCertImport    AuditAction = "certificate.import"
	AuditCertDelete    AuditAction = "certificate.delete"
	AuditCertKeyAccess AuditAction = "certificate.key.access"
	AuditCertTargets   AuditAction = "certificate.targets.update"
	AuditCertNotify    AuditAction = "certificate.notify.update"

	AuditAccountCreate AuditAction = "account.create"
	AuditAccountUpdate AuditAction = "account.update"
	AuditAccountDelete AuditAction = "account.delete"
	AuditAccountTest   AuditAction = "account.test"

	AuditContactCreate AuditAction = "contact.create"
	AuditContactUpdate AuditAction = "contact.update"
	AuditContactDelete AuditAction = "contact.delete"
)

type AuditLog struct {
	ID        string      `json:"id"`
	UserID    string      `json:"user_id"`
	Username  string      `json:"username"`
	Action    AuditAction `json:"action"`
	Resource  string      `json:"resource"`
	Detail    string      `json:"detail"`
	IP        string      `json:"ip"`
	UserAgent string      `json:"user_agent"`
	CreatedAt time.Time   `json:"created_at"`
}

// AuditQuery 审计日志筛选条件
type AuditQuery struct {
	Username string // 精确匹配
	Action   string // 前缀匹配（如 login.）
	Keyword  string // 资源/详情模糊搜索
}
