package domain

import "time"

// ApiToken 程序化客户端凭据（M5）
type ApiToken struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string // sha256(raw token)
	Scopes     []string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// Scope 常量
const (
	ScopeAll   = "all" // 继承用户角色全部权限
	ScopeRead  = "read"
	ScopeCertRead  = "certificates:read"
	ScopeCertWrite = "certificates:write"
	ScopeAccountRead  = "accounts:read"
	ScopeAccountWrite = "accounts:write"
	ScopeDeployRead   = "deploy:read"
	ScopeDeployWrite  = "deploy:write"
	ScopeNotifyRead   = "notify:read"
	ScopeNotifyWrite  = "notify:write"
	ScopeAdminRead    = "admin:read"
	ScopeAdminWrite   = "admin:write"
)

func ValidScope(s string) bool {
	switch s {
	case ScopeAll, ScopeRead,
		ScopeCertRead, ScopeCertWrite,
		ScopeAccountRead, ScopeAccountWrite,
		ScopeDeployRead, ScopeDeployWrite,
		ScopeNotifyRead, ScopeNotifyWrite,
		ScopeAdminRead, ScopeAdminWrite:
		return true
	}
	return false
}

func (t *ApiToken) HasScope(required string) bool {
	if t == nil {
		return false
	}
	for _, s := range t.Scopes {
		if s == ScopeAll || s == required {
			return true
		}
		// read scope 覆盖全部只读
		if s == ScopeRead && isReadScope(required) {
			return true
		}
	}
	return false
}

func isReadScope(s string) bool {
	return len(s) > 5 && s[len(s)-5:] == ":read"
}
