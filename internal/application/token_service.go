package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

type ApiTokenService struct {
	tokens domain.ApiTokenStore
	audit  domain.AuditStore
	clock  Clock
}

func NewApiTokenService(tokens domain.ApiTokenStore, audit domain.AuditStore, clock Clock) *ApiTokenService {
	return &ApiTokenService{tokens: tokens, audit: audit, clock: clock}
}

// CreateToken 创建 API Token；返回明文（仅此一次）
func (s *ApiTokenService) CreateToken(ctx context.Context, user *domain.User, name string, scopes []string, ttlDays int) (*domain.ApiToken, string, error) {
	if name == "" {
		return nil, "", fmt.Errorf("%w: Token 名称不能为空", domain.ErrInvalidArgument)
	}
	if ttlDays < 0 {
		return nil, "", fmt.Errorf("%w: 有效期天数不能为负数", domain.ErrInvalidArgument)
	}
	if len(scopes) == 0 {
		scopes = []string{domain.ScopeAll}
	}
	for _, sc := range scopes {
		if !domain.ValidScope(sc) {
			return nil, "", fmt.Errorf("%w: 无效 scope %s", domain.ErrInvalidArgument, sc)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	tokenStr := "st_" + hex.EncodeToString(raw)
	token := &domain.ApiToken{
		ID:        newID(),
		UserID:    user.ID,
		Name:      name,
		TokenHash: HashToken(tokenStr),
		Scopes:    scopes,
		CreatedAt: s.clock.Now(),
	}
	if ttlDays > 0 {
		exp := s.clock.Now().Add(time.Duration(ttlDays) * 24 * time.Hour)
		token.ExpiresAt = &exp
	}
	if err := s.tokens.Create(ctx, token); err != nil {
		return nil, "", err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    user.ID,
		Username:  user.Username,
		Action:    "token.create",
		Resource:  name,
		CreatedAt: s.clock.Now(),
	})
	return token, tokenStr, nil
}

func (s *ApiTokenService) ListTokens(ctx context.Context, userID string) ([]*domain.ApiToken, error) {
	return s.tokens.ListByUser(ctx, userID)
}

func (s *ApiTokenService) RevokeToken(ctx context.Context, actor *domain.User, id string) error {
	token, err := s.tokens.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if token.UserID != actor.ID && actor.Role != domain.RoleAdmin {
		return fmt.Errorf("%w: 只能吊销自己的 Token", domain.ErrForbidden)
	}
	if err := s.tokens.Delete(ctx, id); err != nil {
		return err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    actor.ID,
		Username:  actor.Username,
		Action:    "token.delete",
		Resource:  token.Name,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

// Authenticate 校验 token 明文 → 返回 ApiToken
func (s *ApiTokenService) Authenticate(ctx context.Context, rawToken string) (*domain.ApiToken, error) {
	if rawToken == "" {
		return nil, domain.ErrUnauthorized
	}
	token, err := s.tokens.GetByHash(ctx, HashToken(rawToken))
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if token.ExpiresAt != nil && s.clock.Now().After(*token.ExpiresAt) {
		return nil, domain.ErrUnauthorized
	}
	_ = s.tokens.TouchLastUsed(ctx, token.ID)
	return token, nil
}
