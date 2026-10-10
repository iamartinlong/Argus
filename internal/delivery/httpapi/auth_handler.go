package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type loginResponse struct {
	AccessToken        string       `json:"access_token"`
	TokenType          string       `json:"token_type"`
	ExpiresIn          int64        `json:"expires_in"`
	MustChangePassword bool         `json:"must_change_password"`
	User               userResponse `json:"user"`
}

const refreshCookieName = "argus_refresh"

type AuthHandler struct {
	auth         *application.AuthService
	accessTTL    time.Duration
	refreshTTL   time.Duration
	cookieSecure bool
}

func newAuthHandler(auth *application.AuthService, accessTTL, refreshTTL time.Duration, cookieSecure bool) *AuthHandler {
	return &AuthHandler{
		auth:         auth,
		accessTTL:    accessTTL,
		refreshTTL:   refreshTTL,
		cookieSecure: cookieSecure,
	}
}

func (h *AuthHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	return r
}

func toUserResponse(u *domain.User) userResponse {
	return userResponse{
		ID:        u.ID,
		Username:  u.Username,
		Role:      string(u.Role),
		Status:    string(u.Status),
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !readJSON(w, r, &req) {
		return
	}
	result, err := h.auth.Login(r.Context(), req.Username, req.Password, clientIP(r), r.UserAgent())
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	h.setRefreshCookie(w, result.RawToken)
	writeOK(w, r, loginResponse{
		AccessToken:        result.AccessToken,
		TokenType:          "Bearer",
		ExpiresIn:          int64(h.accessTTL.Seconds()),
		MustChangePassword: result.MustChangePassword,
		User:               toUserResponse(result.User),
	})
}

func (h *AuthHandler) refresh(w http.ResponseWriter, r *http.Request) {
	// CSRF 加固：刷新是唯一 cookie 认证端点，校验同源（阻止跨站表单触发会话轮换）
	if !sameOrigin(r) {
		writeError(w, r, http.StatusForbidden, "跨站请求被拒绝")
		return
	}
	raw, err := h.refreshTokenFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "未登录")
		return
	}
	result, err := h.auth.Refresh(r.Context(), raw, clientIP(r), r.UserAgent())
	if err != nil {
		h.clearRefreshCookie(w)
		writeError(w, r, mapError(err), "会话已过期，请重新登录")
		return
	}
	h.setRefreshCookie(w, result.RawToken)
	writeOK(w, r, loginResponse{
		AccessToken:        result.AccessToken,
		TokenType:          "Bearer",
		ExpiresIn:          int64(h.accessTTL.Seconds()),
		MustChangePassword: result.MustChangePassword,
		User:               toUserResponse(result.User),
	})
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	raw, _ := h.refreshTokenFromRequest(r)
	if raw != "" {
		_ = h.auth.Logout(r.Context(), raw)
	}
	h.clearRefreshCookie(w)
	writeOK(w, r, map[string]bool{"ok": true})
}

// sameOrigin 校验请求同源（Origin 头与 Host 匹配；或 Sec-Fetch-Site 为 same-origin）
func sameOrigin(r *http.Request) bool {
	if sf := r.Header.Get("Sec-Fetch-Site"); sf != "" {
		return sf == "same-origin" || sf == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		// 无 Origin（同源导航/表单）默认放行
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// refreshTokenFromRequest: 优先 cookie，兼容 Authorization 头携带
func (h *AuthHandler) refreshTokenFromRequest(r *http.Request) (string, error) {	if c, err := r.Cookie(refreshCookieName); err == nil && c.Value != "" {
		raw, err := base64.RawURLEncoding.DecodeString(c.Value)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:], nil
	}
	return "", domain.ErrUnauthorized
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, rawToken string) {
	raw := base64.RawURLEncoding.EncodeToString([]byte(rawToken))
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    raw,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.refreshTTL.Seconds()),
	})
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
