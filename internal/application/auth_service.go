package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/iamartinlong/Argus/internal/domain"
)

type TokenService interface {
	IssueAccessToken(user *domain.User) (string, error)
	IssueRefreshToken() (string, string, error) // returns rawToken, tokenHash
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

// Now 统一返回 UTC（与存储层 updated_at/比较逻辑一致，避免本地时区偏移混用）
func (RealClock) Now() time.Time { return time.Now().UTC() }

type AuthService struct {
	users      domain.UserStore
	sessions   domain.SessionStore
	audit      domain.AuditStore
	tokens     TokenService
	clock      Clock
	accessTTL  time.Duration
	refreshTTL time.Duration
	limiter    *LoginLimiter
}

func NewAuthService(
	users domain.UserStore,
	sessions domain.SessionStore,
	audit domain.AuditStore,
	tokens TokenService,
	clock Clock,
	accessTTL, refreshTTL time.Duration,
	maxFailures int,
	lockout time.Duration,
) *AuthService {
	return &AuthService{
		users:      users,
		sessions:   sessions,
		audit:      audit,
		tokens:     tokens,
		clock:      clock,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		limiter:    NewLoginLimiter(maxFailures, lockout),
	}
}

type LoginResult struct {
	AccessToken       string
	User              *domain.User
	Refresh           *domain.RefreshSession
	RawToken          string // 仅此一次返回给调用方写入 cookie
	MustChangePassword bool
}

// dummyPasswordHash 登录时用户不存在分支用于等时比较的固定 bcrypt 值（防用户名枚举）。
// 与真实口令同为 bcrypt.DefaultCost；对应明文无实际用途，不可用于登录。
const dummyPasswordHash = "$2a$10$nt9OYR/lVtBDGGCpfAwf1u6glYFkn1xk5mZQnH0VxlREUzdEBzqx."

func (s *AuthService) Login(ctx context.Context, username, password, ip, userAgent string) (*LoginResult, error) {
	if err := s.limiter.Check(username, ip); err != nil {
		return nil, err
	}
	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		// 用户不存在也执行一次 bcrypt 比较，抹平与"密码错误"分支的响应时延差（防时序侧信道枚举用户名）。
		// 不调用 limiter.RecordFailure：避免匿名请求对任意用户名触发锁定（与既有逻辑一致）。
		_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
		s.recordFailure(ctx, username, ip, userAgent, "user not found")
		return nil, domain.ErrUnauthorized
	}
	if user.Status != domain.UserActive {
		s.recordFailure(ctx, username, ip, userAgent, "account disabled")
		return nil, domain.ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		s.limiter.RecordFailure(username, ip)
		s.recordFailure(ctx, username, ip, userAgent, "wrong password")
		return nil, domain.ErrUnauthorized
	}
	s.limiter.Reset(username, ip)
	s.recordSuccess(ctx, user, ip, userAgent)

	accessToken, err := s.tokens.IssueAccessToken(user)
	if err != nil {
		return nil, err
	}
	raw, hash, err := s.tokens.IssueRefreshToken()
	if err != nil {
		return nil, err
	}
	session := &domain.RefreshSession{
		ID:        hash,
		UserID:    user.ID,
		ExpiresAt: s.clock.Now().Add(s.refreshTTL),
		CreatedAt: s.clock.Now(),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}
	return &LoginResult{AccessToken: accessToken, User: user, Refresh: session, RawToken: raw, MustChangePassword: user.MustChangePassword}, nil
}

func (s *AuthService) Refresh(ctx context.Context, rawToken, ip, userAgent string) (*LoginResult, error) {
	if rawToken == "" {
		return nil, domain.ErrUnauthorized
	}
	hash := HashToken(rawToken)
	session, err := s.sessions.GetByTokenHash(ctx, hash)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if session.RevokedAt != nil || s.clock.Now().After(session.ExpiresAt) {
		return nil, domain.ErrUnauthorized
	}
	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil || user.Status != domain.UserActive {
		return nil, domain.ErrUnauthorized
	}
	// 轮换：旧会话作废，签发新会话
	if err := s.sessions.Revoke(ctx, hash); err != nil {
		return nil, err
	}
	accessToken, err := s.tokens.IssueAccessToken(user)
	if err != nil {
		return nil, err
	}
	rawNew, hashNew, err := s.tokens.IssueRefreshToken()
	if err != nil {
		return nil, err
	}
	newSession := &domain.RefreshSession{
		ID:        hashNew,
		UserID:    user.ID,
		ExpiresAt: s.clock.Now().Add(s.refreshTTL),
		CreatedAt: s.clock.Now(),
	}
	if err := s.sessions.Create(ctx, newSession); err != nil {
		return nil, err
	}
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    user.ID,
		Username:  user.Username,
		Action:    domain.AuditRefresh,
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: s.clock.Now(),
	})
	return &LoginResult{AccessToken: accessToken, User: user, Refresh: newSession, RawToken: rawNew, MustChangePassword: user.MustChangePassword}, nil
}

func (s *AuthService) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return s.sessions.Revoke(ctx, HashToken(rawToken))
}

func (s *AuthService) RevokeAllUserSessions(ctx context.Context, userID string) error {
	return s.sessions.RevokeAllByUser(ctx, userID)
}

func (s *AuthService) recordSuccess(ctx context.Context, user *domain.User, ip, userAgent string) {
	_ = s.audit.Append(ctx, &domain.AuditLog{
		UserID:    user.ID,
		Username:  user.Username,
		Action:    domain.AuditLoginSuccess,
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: s.clock.Now(),
	})
}

func (s *AuthService) recordFailure(ctx context.Context, username, ip, userAgent, detail string) {
	_ = s.audit.Append(ctx, &domain.AuditLog{
		Username:  username,
		Action:    domain.AuditLoginFailure,
		Detail:    detail,
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: s.clock.Now(),
	})
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// LoginLimiter 简单内存滑动窗口限流：按 user@ip 维度。
type LoginLimiter struct {
	mu          sync.Mutex
	maxFailures int
	lockout     time.Duration
	clock       Clock
	failures    map[string]int
	locks       map[string]time.Time
}

func NewLoginLimiter(maxFailures int, lockout time.Duration) *LoginLimiter {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	return &LoginLimiter{
		maxFailures: maxFailures,
		lockout:     lockout,
		clock:       RealClock{},
		failures:    make(map[string]int),
		locks:       make(map[string]time.Time),
	}
}

func (l *LoginLimiter) key(username, ip string) string { return username + "@" + ip }

func (l *LoginLimiter) Check(username, ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.key(username, ip)
	if until, ok := l.locks[key]; ok {
		if l.clock.Now().Before(until) {
			return fmt.Errorf("%w: 尝试次数过多，请 %v 后再试", domain.ErrLocked, until.Sub(l.clock.Now()).Round(time.Minute))
		}
		delete(l.locks, key)
		delete(l.failures, key)
	}
	return nil
}

func (l *LoginLimiter) RecordFailure(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.key(username, ip)
	l.failures[key]++
	if l.failures[key] >= l.maxFailures {
		l.locks[key] = l.clock.Now().Add(l.lockout)
		delete(l.failures, key)
	}
}

func (l *LoginLimiter) Reset(username, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.key(username, ip)
	delete(l.failures, key)
	delete(l.locks, key)
}
