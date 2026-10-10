package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/iamartinlong/Argus/internal/domain"
)

type fakeUserStore struct {
	mu    sync.Mutex
	users map[string]*domain.User
	byName map[string]*domain.User
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: map[string]*domain.User{}, byName: map[string]*domain.User{}}
}

func (f *fakeUserStore) GetByID(_ context.Context, id string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeUserStore) GetByUsername(_ context.Context, username string) (*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byName[username]; ok {
		return u, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeUserStore) List(context.Context) ([]*domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.User
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserStore) Create(_ context.Context, u *domain.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.ID] = u
	f.byName[u.Username] = u
	return nil
}

func (f *fakeUserStore) Update(_ context.Context, u *domain.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.users[u.ID]; !ok {
		return domain.ErrNotFound
	}
	f.users[u.ID] = u
	f.byName[u.Username] = u
	return nil
}

func (f *fakeUserStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		delete(f.users, id)
		delete(f.byName, u.Username)
		return nil
	}
	return domain.ErrNotFound
}

func (f *fakeUserStore) Count(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.users)), nil
}

type fakeSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*domain.RefreshSession
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]*domain.RefreshSession{}}
}

func (f *fakeSessionStore) Create(_ context.Context, s *domain.RefreshSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeSessionStore) GetByTokenHash(_ context.Context, hash string) (*domain.RefreshSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[hash]; ok {
		return s, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeSessionStore) Revoke(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[hash]; ok {
		now := time.Now()
		s.RevokedAt = &now
	}
	return nil
}

func (f *fakeSessionStore) RevokeAllByUser(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.sessions {
		if s.UserID == userID {
			now := time.Now()
			s.RevokedAt = &now
		}
	}
	return nil
}

type fakeAuditStore struct {
	mu   sync.Mutex
	logs []*domain.AuditLog
}

func (f *fakeAuditStore) Append(_ context.Context, l *domain.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, l)
	return nil
}

func (f *fakeAuditStore) Count(context.Context, domain.AuditQuery) (int64, error) { return 0, nil }

func (f *fakeAuditStore) DeleteOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

func (f *fakeAuditStore) List(context.Context, domain.AuditQuery, int, int) ([]*domain.AuditLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs, nil
}

type fakeTokenService struct {
	mu sync.Mutex
	n  int
}

func (f *fakeTokenService) IssueAccessToken(*domain.User) (string, error) {
	return "access-token", nil
}

func (f *fakeTokenService) IssueRefreshToken() (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	raw := "refresh-raw-token-" + string(rune('0'+f.n))
	return raw, HashToken(raw), nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func hashPassword(pwd string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	return string(b), err
}

func newTestAuthService(t *testing.T, users domain.UserStore, sessions domain.SessionStore) *AuthService {
	t.Helper()
	audit := &fakeAuditStore{}
	return NewAuthService(users, sessions, audit, &fakeTokenService{}, fixedClock{time.Now()},
		15*time.Minute, 720*time.Hour, 5, 15*time.Minute)
}

func newTestUserService(t *testing.T) *UserService {
	t.Helper()
	users := newFakeUserStore()
	return NewUserService(users, &fakeAuditStore{}, nil, fixedClock{time.Now()}, 12)
}

func seedUser(t *testing.T, users *fakeUserStore, username, password string, role domain.Role) *domain.User {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u := &domain.User{
		ID:           "u-" + username,
		Username:     username,
		PasswordHash: hash,
		Role:         role,
		Status:       domain.UserActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoginSuccess(t *testing.T) {
	users := newFakeUserStore()
	seedUser(t, users, "admin", "correct-horse-battery", domain.RoleAdmin)
	sessions := newFakeSessionStore()
	svc := newTestAuthService(t, users, sessions)

	result, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if result.AccessToken == "" || result.User.Role != domain.RoleAdmin {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	users := newFakeUserStore()
	seedUser(t, users, "admin", "correct-horse-battery", domain.RoleAdmin)
	svc := newTestAuthService(t, users, newFakeSessionStore())

	_, err := svc.Login(context.Background(), "admin", "wrong", "127.0.0.1", "test")
	if err != domain.ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestLoginDisabledUser(t *testing.T) {
	users := newFakeUserStore()
	u := seedUser(t, users, "admin", "correct-horse-battery", domain.RoleAdmin)
	u.Status = domain.UserDisabled
	svc := newTestAuthService(t, users, newFakeSessionStore())

	_, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "127.0.0.1", "test")
	if err != domain.ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestLoginLockoutAfterFailures(t *testing.T) {
	users := newFakeUserStore()
	seedUser(t, users, "admin", "correct-horse-battery", domain.RoleAdmin)
	svc := newTestAuthService(t, users, newFakeSessionStore())
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := svc.Login(ctx, "admin", "wrong", "10.0.0.1", "test")
		if err != domain.ErrUnauthorized {
			t.Fatalf("attempt %d: expected ErrUnauthorized, got %v", i, err)
		}
	}
	_, err := svc.Login(ctx, "admin", "correct-horse-battery", "10.0.0.1", "test")
	if !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("expected ErrLocked after 5 failures, got %v", err)
	}
}

func TestRefreshRotatesSession(t *testing.T) {
	users := newFakeUserStore()
	seedUser(t, users, "admin", "correct-horse-battery", domain.RoleAdmin)
	sessions := newFakeSessionStore()
	svc := newTestAuthService(t, users, sessions)
	ctx := context.Background()

	login, err := svc.Login(ctx, "admin", "correct-horse-battery", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	oldHash := HashToken(login.RawToken)

	refreshed, err := svc.Refresh(ctx, login.RawToken, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.RawToken == login.RawToken {
		t.Fatal("expected rotated refresh token")
	}
	// 旧 token 应已失效
	_, err = svc.Refresh(ctx, login.RawToken, "127.0.0.1", "test")
	if err != domain.ErrUnauthorized {
		t.Fatalf("expected old token rejected, got %v", err)
	}
	// 新 token 可用
	if _, err := svc.Refresh(ctx, refreshed.RawToken, "127.0.0.1", "test"); err != nil {
		t.Fatalf("new token should work: %v", err)
	}
	_ = oldHash
}
