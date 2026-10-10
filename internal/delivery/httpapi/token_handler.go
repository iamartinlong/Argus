package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

type tokenResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

func toTokenResponse(t *domain.ApiToken) tokenResponse {
	out := tokenResponse{
		ID:        t.ID,
		Name:      t.Name,
		Scopes:    t.Scopes,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
	}
	if t.ExpiresAt != nil {
		ts := t.ExpiresAt.Format(time.RFC3339)
		out.ExpiresAt = &ts
	}
	if t.LastUsedAt != nil {
		ts := t.LastUsedAt.Format(time.RFC3339)
		out.LastUsedAt = &ts
	}
	return out
}

type createTokenRequest struct {
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	TTLDays  int      `json:"ttl_days"` // 0 = 永不过期
}

type createTokenResponse struct {
	tokenResponse
	Token string `json:"token"` // 明文仅返回一次
}

type TokenHandler struct {
	tokens tokenServiceIface
}

// tokenServiceIface 便于测试
type tokenServiceIface interface {
	CreateToken(ctx context.Context, user *domain.User, name string, scopes []string, ttlDays int) (*domain.ApiToken, string, error)
	ListTokens(ctx context.Context, userID string) ([]*domain.ApiToken, error)
	RevokeToken(ctx context.Context, actor *domain.User, id string) error
}

func newTokenHandler(tokens *application.ApiTokenService) *TokenHandler {
	return &TokenHandler{tokens: tokens}
}

func (h *TokenHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
		r.Post("/", h.create)
		r.Delete("/{id}", h.revoke)
	})
	return r
}

func (h *TokenHandler) list(w http.ResponseWriter, r *http.Request) {
	p := userFrom(r.Context())
	tokens, err := h.tokens.ListTokens(r.Context(), p.User.ID)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	out := make([]tokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toTokenResponse(t))
	}
	writeOK(w, r, out)
}

func (h *TokenHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if !readJSON(w, r, &req) {
		return
	}
	p := userFrom(r.Context())
	token, raw, err := h.tokens.CreateToken(r.Context(), p.User, req.Name, req.Scopes, req.TTLDays)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, createTokenResponse{
		tokenResponse: toTokenResponse(token),
		Token:         raw,
	})
}

func (h *TokenHandler) revoke(w http.ResponseWriter, r *http.Request) {
	p := userFrom(r.Context())
	if err := h.tokens.RevokeToken(r.Context(), p.User, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	writeOK(w, r, map[string]bool{"ok": true})
}
