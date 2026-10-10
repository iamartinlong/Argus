package application

import (
	"context"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

func newUserServiceWithAuth(t *testing.T) (*UserService, *AuthService, *fakeUserStore, *fakeSessionStore) {
	t.Helper()
	users := newFakeUserStore()
	sessions := newFakeSessionStore()
	audit := &fakeAuditStore{}
	auth := NewAuthService(users, sessions, audit, &fakeTokenService{}, fixedClock{time.Now()},
		15*time.Minute, 720*time.Hour, 5, 15*time.Minute)
	svc := NewUserService(users, audit, auth, fixedClock{time.Now()}, 12)
	return svc, auth, users, sessions
}

func TestPasswordPolicy(t *testing.T) {
	svc := newTestUserService(t)
	actor := &domain.User{ID: "a", Username: "admin", Role: domain.RoleAdmin}
	ctx := context.Background()

	cases := []struct {
		name string
		pwd  string
		ok   bool
	}{
		{"valid", "Abcd12345678", true},
		{"too short", "Ab1", false},
		{"no uppercase", "abcd12345678", false},
		{"no lowercase", "ABCD12345678", false},
		{"no digit", "Abcdefghijkl", false},
		{"only letters", "AbcdefghijkL", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.CreateUser(ctx, actor, "u-"+c.name, c.pwd, domain.RoleOperator)
			if c.ok && err != nil {
				t.Fatalf("expected success, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("expected error, got success")
			}
		})
	}
}

func TestMustChangePasswordPropagation(t *testing.T) {
	users := newFakeUserStore()
	u := seedUser(t, users, "admin", "CorrectHorse123", domain.RoleAdmin)
	u.MustChangePassword = true
	sessions := newFakeSessionStore()
	svc := newTestAuthService(t, users, sessions)

	result, err := svc.Login(context.Background(), "admin", "CorrectHorse123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.MustChangePassword {
		t.Fatal("expected must_change_password=true")
	}
	// 改密后标志清除
	us := NewUserService(users, &fakeAuditStore{}, svc, fixedClock{time.Now()}, 12)
	if err := us.ChangePassword(context.Background(), u, "CorrectHorse123", "NewCorrect456"); err != nil {
		t.Fatal(err)
	}
	if u.MustChangePassword {
		t.Fatal("expected must_change_password cleared after change")
	}
}

func TestChangePasswordRevokesSessions(t *testing.T) {
	svc, auth, users, sessions := newUserServiceWithAuth(t)
	u := seedUser(t, users, "admin", "CorrectHorse123", domain.RoleAdmin)
	ctx := context.Background()

	// 登录两次，产生两个会话
	login1, err := auth.Login(ctx, "admin", "CorrectHorse123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	login2, err := auth.Login(ctx, "admin", "CorrectHorse123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	// 改密
	if err := svc.ChangePassword(ctx, u, "CorrectHorse123", "NewCorrect456"); err != nil {
		t.Fatal(err)
	}

	// 所有旧 refresh 会话应已吊销
	for name, raw := range map[string]string{"login1": login1.RawToken, "login2": login2.RawToken} {
		sess, err := sessions.GetByTokenHash(ctx, HashToken(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if sess.RevokedAt == nil {
			t.Fatalf("%s: expected session revoked after password change", name)
		}
	}
}

func TestChangePasswordRejectsWrongOldPassword(t *testing.T) {
	svc := newTestUserService(t)
	users := newFakeUserStore()
	u := seedUser(t, users, "admin", "CorrectHorse123", domain.RoleAdmin)
	us := NewUserService(users, &fakeAuditStore{}, nil, fixedClock{time.Now()}, 12)
	if err := us.ChangePassword(context.Background(), u, "WrongOld123", "NewCorrect456"); err != domain.ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	_ = svc
}

func TestResetPassword(t *testing.T) {
	svc, auth, users, sessions := newUserServiceWithAuth(t)
	u := seedUser(t, users, "admin", "CorrectHorse123", domain.RoleAdmin)
	u.MustChangePassword = true
	ctx := context.Background()
	actor := &domain.User{ID: "a", Username: "admin", Role: domain.RoleAdmin}

	// 登录产生会话
	login, err := auth.Login(ctx, "admin", "CorrectHorse123", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}

	// 免旧密码重置
	if err := svc.ResetPassword(ctx, actor, u, "BrandNewPass456"); err != nil {
		t.Fatal(err)
	}
	if u.MustChangePassword {
		t.Fatal("expected must_change_password cleared after reset")
	}

	// 新密码可登录，旧密码失败
	if _, err := auth.Login(ctx, "admin", "BrandNewPass456", "127.0.0.1", "test"); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if _, err := auth.Login(ctx, "admin", "CorrectHorse123", "127.0.0.1", "test"); err == nil {
		t.Fatal("expected old password rejected")
	}

	// 旧会话被吊销
	sess, err := sessions.GetByTokenHash(ctx, HashToken(login.RawToken))
	if err != nil {
		t.Fatal(err)
	}
	if sess.RevokedAt == nil {
		t.Fatal("expected session revoked after reset")
	}

	// 弱密码拒绝
	if err := svc.ResetPassword(ctx, actor, u, "weak"); err == nil {
		t.Fatal("expected weak password rejected")
	}
}
