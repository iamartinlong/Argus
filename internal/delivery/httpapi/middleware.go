package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/iamartinlong/Argus/internal/domain"
)

type ctxKey string

const ctxUserKey ctxKey = "user"

type principal struct {
	User     *domain.User
	TokenType string // "jwt" | "apitoken"
}

func userFrom(ctx context.Context) *principal {
	p, _ := ctx.Value(ctxUserKey).(*principal)
	return p
}

func withUser(ctx context.Context, p *principal) context.Context {
	return context.WithValue(ctx, ctxUserKey, p)
}

func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			tokenStr := strings.TrimPrefix(auth, "Bearer ")
			user, err := h.jwt.VerifyAccessToken(tokenStr)
			if err != nil {
				writeError(w, r, http.StatusUnauthorized, "会话无效或已过期")
				return
			}
			// 从存储确认用户仍存在且启用
			fresh, err := h.users.GetByID(r.Context(), user.ID)
			if err != nil || fresh.Status != domain.UserActive {
				writeError(w, r, http.StatusUnauthorized, "账号不可用")
				return
			}
			// 强制改密拦截：初始密码用户仅允许改密接口
			if fresh.MustChangePassword && !isPasswordChangePath(r.URL.Path) {
				writeError(w, r, http.StatusForbidden, "首次登录需先修改初始密码")
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), &principal{User: fresh, TokenType: "jwt"})))
			return
		}
		// API Token 认证（程序化客户端）
		if apiToken := r.Header.Get("X-API-Token"); apiToken != "" && h.tokens != nil {
			token, err := h.tokens.Authenticate(r.Context(), apiToken)
			if err != nil {
				writeError(w, r, http.StatusUnauthorized, "API Token 无效或已过期")
				return
			}
			user, err := h.users.GetByID(r.Context(), token.UserID)
			if err != nil || user.Status != domain.UserActive {
				writeError(w, r, http.StatusUnauthorized, "账号不可用")
				return
			}
			if user.MustChangePassword {
				writeError(w, r, http.StatusForbidden, "首次登录需先修改初始密码")
				return
			}
			// scope 校验
			if required, ok := requiredScope(r); ok && !token.HasScope(required) {
				writeError(w, r, http.StatusForbidden, "Token 无权执行该操作（需要 scope: "+required+"）")
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), &principal{User: user, TokenType: "apitoken"})))
			return
		}
		writeError(w, r, http.StatusUnauthorized, "未认证")
	})
}

// requiredScope 根据请求路径与方法返回所需 scope
func requiredScope(r *http.Request) (string, bool) {
	path := r.URL.Path
	write := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	readScope := func(resource string) string { return resource + ":read" }
	writeScope := func(resource string) string { return resource + ":write" }
	switch {
	case strings.HasPrefix(path, "/api/v1/certificates"):
		if write {
			return writeScope("certificates"), true
		}
		return readScope("certificates"), true
	case strings.HasPrefix(path, "/api/v1/acme-accounts"):
		if write {
			return writeScope("accounts"), true
		}
		return readScope("accounts"), true
	case strings.HasPrefix(path, "/api/v1/dns-accounts"):
		if write {
			return writeScope("certificates"), true
		}
		return readScope("certificates"), true
	case strings.HasPrefix(path, "/api/v1/deploy-targets"):
		if write {
			return writeScope("deploy"), true
		}
		return readScope("deploy"), true
	case strings.HasPrefix(path, "/api/v1/notification-rules"):
		if write {
			return writeScope("notify"), true
		}
		return readScope("notify"), true
	case strings.HasPrefix(path, "/api/v1/notification-channels"),
		strings.HasPrefix(path, "/api/v1/notification-templates"),
		strings.HasPrefix(path, "/api/v1/notification-send-logs"):
		if write {
			return writeScope("notify"), true
		}
		return readScope("notify"), true
	case strings.HasPrefix(path, "/api/v1/agents"):
		if write {
			return writeScope("admin"), true
		}
		return readScope("admin"), true
	case strings.HasPrefix(path, "/api/v1/api-tokens"):
		if write {
			return writeScope("admin"), true
		}
		return readScope("admin"), true
	case strings.HasPrefix(path, "/api/v1/settings/contacts"):
		// 申请人信息属系统管理资源，API Token 需 admin scope（与 users/agents 一致）
		if write {
			return writeScope("admin"), true
		}
		return readScope("admin"), true
	case strings.HasPrefix(path, "/api/v1/users"), strings.HasPrefix(path, "/api/v1/audit-logs"):
		if write {
			return writeScope("admin"), true
		}
		return readScope("admin"), true
	case strings.HasPrefix(path, "/api/v1/task-logs"):
		// 手动触发扫描是写操作（触发续期与部署重试），需证书写权限
		if write {
			return writeScope("certificates"), true
		}
		return domain.ScopeRead, true
	case strings.HasPrefix(path, "/api/v1/stats"):
		return domain.ScopeRead, true
	default:
		// 未知路径：安全默认拒绝（要求管理员权限），避免新 API 未纳入规则时被 token 绕过
		if write {
			return writeScope("admin"), true
		}
		return readScope("admin"), true
	}
}

func isPasswordChangePath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/users/me/password")
}

func requireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	allowed := make(map[domain.Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := userFrom(r.Context())
			if p == nil {
				writeError(w, r, http.StatusUnauthorized, "未认证")
				return
			}
			if !allowed[p.User.Role] {
				writeError(w, r, http.StatusForbidden, "无权限执行该操作")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func mapError(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrLocked):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func clientIP(r *http.Request) string {
	// 不信任 X-Forwarded-For / X-Real-IP 等请求头（客户端可伪造，可绕过登录限流并污染审计日志），
	// 一律取 TCP 对端地址。反代部署下会统一记录为代理 IP——限流从紧（安全方向），
	// 如需真实 IP 应配置可信代理网段后自行解析 XFF。
	host := r.RemoteAddr
	if idx := strings.LastIndexByte(host, ':'); idx > 0 {
		return host[:idx]
	}
	return host
}
