package application

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/iamartinlong/Argus/internal/domain"
)

type UserService struct {
	users      domain.UserStore
	audit      domain.AuditStore
	auth       *AuthService
	clock      Clock
	minPassLen int
}

func NewUserService(
	users domain.UserStore,
	audit domain.AuditStore,
	auth *AuthService,
	clock Clock,
	minPassLen int,
) *UserService {
	if minPassLen <= 0 {
		minPassLen = 12
	}
	return &UserService{
		users:      users,
		audit:      audit,
		auth:       auth,
		clock:      clock,
		minPassLen: minPassLen,
	}
}

func (s *UserService) CreateUser(
	ctx context.Context,
	actor *domain.User,
	username, password string,
	role domain.Role,
) (*domain.User, error) {
	if username == "" || len(username) > 64 {
		return nil, domain.ErrInvalidArgument
	}
	if !role.Valid() {
		return nil, domain.ErrInvalidArgument
	}
	if err := s.validatePassword(password); err != nil {
		return nil, err
	}
	if _, err := s.users.GetByUsername(ctx, username); err == nil {
		return nil, domain.ErrConflict
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &domain.User{
		ID:           newID(),
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Status:       domain.UserActive,
		CreatedAt:    s.clock.Now(),
		UpdatedAt:    s.clock.Now(),
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    actor.ID,
		Username:  actor.Username,
		Action:    domain.AuditUserCreate,
		Resource:  username,
		CreatedAt: s.clock.Now(),
	})
	return user, nil
}

func (s *UserService) UpdateUser(
	ctx context.Context,
	actor *domain.User,
	id string,
	role domain.Role,
	status domain.UserStatus,
) (*domain.User, error) {
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if role != "" && !role.Valid() {
		return nil, domain.ErrInvalidArgument
	}
	if role != "" {
		user.Role = role
	}
	if status != "" {
		user.Status = status
	}
	user.UpdatedAt = s.clock.Now()
	if err := s.users.Update(ctx, user); err != nil {
		return nil, err
	}
	if user.Status == domain.UserDisabled {
		_ = s.auth.RevokeAllUserSessions(ctx, user.ID)
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    actor.ID,
		Username:  actor.Username,
		Action:    domain.AuditUserUpdate,
		Resource:  id,
		CreatedAt: s.clock.Now(),
	})
	return user, nil
}

func (s *UserService) DeleteUser(ctx context.Context, actor *domain.User, id string) error {
	if actor.ID == id {
		return fmt.Errorf("%w: 不能删除当前登录用户", domain.ErrInvalidArgument)
	}
	if err := s.users.Delete(ctx, id); err != nil {
		return err
	}
	_ = s.auth.RevokeAllUserSessions(ctx, id)
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    actor.ID,
		Username:  actor.Username,
		Action:    domain.AuditUserDelete,
		Resource:  id,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

func (s *UserService) ChangePassword(
	ctx context.Context,
	user *domain.User,
	oldPassword, newPassword string,
) error {
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)) != nil {
		return domain.ErrUnauthorized
	}
	if err := s.validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	user.MustChangePassword = false
	user.UpdatedAt = s.clock.Now()
	if err := s.users.Update(ctx, user); err != nil {
		return err
	}
	if s.auth != nil {
		// 改密后吊销该用户全部 Refresh 会话（07-auth §7.3 会话失效策略）
		_ = s.auth.RevokeAllUserSessions(ctx, user.ID)
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    user.ID,
		Username:  user.Username,
		Action:    domain.AuditPassword,
		Resource:  user.ID,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

// ResetPassword 管理员重置密码（忘记密码场景）：无需旧密码。
// 校验密码策略 → 更新哈希 → 清除强制改密标志 → 吊销该用户全部 Refresh 会话 → 审计。
// 注意：无状态 JWT  access token 在其 TTL 内仍有效（最长 access_token_ttl）。
func (s *UserService) ResetPassword(
	ctx context.Context,
	actor *domain.User,
	user *domain.User,
	newPassword string,
) error {
	if user == nil {
		return domain.ErrNotFound
	}
	if err := s.validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	user.MustChangePassword = false
	user.UpdatedAt = s.clock.Now()
	if err := s.users.Update(ctx, user); err != nil {
		return err
	}
	if s.auth != nil {
		_ = s.auth.RevokeAllUserSessions(ctx, user.ID)
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    actor.ID,
		Username:  actor.Username,
		Action:    domain.AuditPasswordReset,
		Resource:  user.ID,
		CreatedAt: s.clock.Now(),
	})
	return nil
}

func (s *UserService) ListUsers(ctx context.Context) ([]*domain.User, error) {
	return s.users.List(ctx)
}

func (s *UserService) CountUsers(ctx context.Context) (int64, error) {
	return s.users.Count(ctx)
}

func (s *UserService) validatePassword(password string) error {
	if len(password) < s.minPassLen {
		return fmt.Errorf("%w: 密码长度至少 %d 位", domain.ErrInvalidArgument, s.minPassLen)
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("%w: 密码必须包含大写字母、小写字母和数字", domain.ErrInvalidArgument)
	}
	return nil
}
