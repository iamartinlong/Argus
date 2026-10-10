package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/config"
	appcrypto "github.com/iamartinlong/Argus/internal/infrastructure/crypto"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
	"github.com/iamartinlong/Argus/internal/infrastructure/notify"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
	"github.com/iamartinlong/Argus/internal/domain"
)

type testServer struct {
	t         *testing.T
	handler   *Handler
	db        *sqlite.DB
	userStore *sqlite.UserStore
	cookies   []*http.Cookie
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	cfg := config.Default()
	cfg.Storage.DSN = ":memory:"
	cfg.Security.JWTSecret = "test-secret-test-secret-test-secret-1234"
	accessTTL := 15 * time.Minute
	refreshTTL := 720 * time.Hour

	db, err := sqlite.Open(cfg.Storage.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	userStore := sqlite.NewUserStore(db)
	sessionStore := sqlite.NewSessionStore(db)
	auditStore := sqlite.NewAuditStore(db)
	certStore := sqlite.NewCertificateStore(db)
	orderStore := sqlite.NewOrderStore(db)
	accountStore := sqlite.NewProviderAccountStore(db)

	aes, err := appcrypto.NewAESGCM("test-master-key-test-master-key-1234")
	if err != nil {
		t.Fatal(err)
	}

	jwtSvc, err := appcrypto.NewJWTService(cfg.Security.JWTSecret, accessTTL)
	if err != nil {
		t.Fatal(err)
	}
	authSvc := application.NewAuthService(userStore, sessionStore, auditStore, jwtSvc,
		application.RealClock{}, accessTTL, refreshTTL, 5, 15*time.Minute)
	userSvc := application.NewUserService(userStore, auditStore, authSvc, application.RealClock{}, 12)
	auditSvc := application.NewAuditService(auditStore)
	reg := newTestRegistry()
	accountSvc := application.NewProviderAccountService(accountStore, certStore, auditStore, reg, aes, application.RealClock{})
	certSvc := application.NewCertificateService(
		certStore, orderStore, accountStore, sqlite.NewContactStore(db), auditStore, reg, aes, application.RealClock{}, 80, application.NewEventBus())
	ruleStore := sqlite.NewNotificationRuleStore(db)
	notifyLogStore := sqlite.NewNotificationLogStore(db)
	taskLogStore := sqlite.NewTaskLogStore(db)
	channelSvc := application.NewChannelService(sqlite.NewNotificationChannelStore(db), ruleStore, auditStore,
		notify.NewBuilder(), aes, application.RealClock{})
	notifySvc := application.NewNotificationService(ruleStore, notifyLogStore, sqlite.NewNotificationSendLogStore(db), sqlite.NewNotificationTemplateStore(db), auditStore, channelSvc, application.RealClock{})
	deploySvc := application.NewDeployService(
		sqlite.NewDeployTargetStore(db), sqlite.NewDeployRecordStore(db), certStore, auditStore,
		testDeployRegistry{}, aes, application.RealClock{}, application.NewEventBus())
	tokenSvc := application.NewApiTokenService(sqlite.NewApiTokenStore(db), auditStore, application.RealClock{})
	agentSvc := application.NewAgentService(
		sqlite.NewAgentStore(db), sqlite.NewAgentTaskStore(db),
		sqlite.NewDeployTargetStore(db), certStore, sqlite.NewDeployRecordStore(db),
		auditStore, aes, application.RealClock{}, application.NewEventBus())
	agentSvc.SetBinaryDir(filepath.Join(t.TempDir(), "agent-binaries"))

	handler := NewHandler(cfg, HandlerDeps{
		Users:               userStore,
		CertStore:           certStore,
		TaskLogs:            taskLogStore,
		Orders:              orderStore,
		Auth:                authSvc,
		UserService:         userSvc,
		AuditService:        auditSvc,
		AuditStore:          auditStore,
		CertService:         certSvc,
		AccountService:      accountSvc,
		NotificationService: notifySvc,
		ChannelService:      channelSvc,
		DeployRecords:       sqlite.NewDeployRecordStore(db),
		SendLogs:            sqlite.NewNotificationSendLogStore(db),
		Agents:              sqlite.NewAgentStore(db),
		DeployService:       deploySvc,
		ApiTokens:           tokenSvc,
		AgentService:        agentSvc,
		ContactService:      application.NewContactService(sqlite.NewContactStore(db), auditStore, application.RealClock{}),
		Cryptor:             aes,
		JWT:                 jwtSvc,
		ProviderNames:       []string{"acme"},
		AccessTTL:           accessTTL,
		RefreshTTL:          refreshTTL,
	})
	return &testServer{t: t, handler: handler, db: db, userStore: userStore}
}

// testDeployRegistry 含 local/ssh/agent 适配器的部署注册表
type testDeployRegistry struct{}

func (testDeployRegistry) Get(t string) (deploy.Adapter, bool) {
	switch t {
	case "local":
		return deploy.Local{}, true
	case "ssh":
		return deploy.NewSSH(5 * time.Second), true
	}
	return nil, false
}

func (s *testServer) seedUser(username, password string, role domain.Role) *domain.User {
	s.t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.t.Fatal(err)
	}
	user := &domain.User{
		ID:           "seed-" + username,
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Status:       domain.UserActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.userStore.Create(s.t.Context(), user); err != nil {
		s.t.Fatal(err)
	}
	return user
}

func (s *testServer) do(method, path string, body any, token string) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			s.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	if token != "" {
		switch {
		case strings.HasPrefix(token, "st_"):
			req.Header.Set("X-API-Token", token)
		case strings.HasPrefix(token, "ag_"):
			req.Header.Set("X-Agent-Token", token)
		default:
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	for _, c := range s.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		s.cookies = append(s.cookies, c)
	}
	return rec
}

func (s *testServer) login(username, password string) string {
	s.t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": username, "password": password,
	}, "")
	if rec.Code != http.StatusOK {
		s.t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	decodeData(s.t, rec, &resp)
	return resp.AccessToken
}

func TestHealthz(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/healthz", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
}

func TestUnauthenticatedAccess(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/api/v1/users", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestFullAuthFlow(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)

	// admin 登录
	adminToken := s.login("admin", "adminpass123456")
	if adminToken == "" {
		t.Fatal("empty admin token")
	}

	// 错误密码
	rec := s.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "wrong",
	}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// admin 创建 operator
	rec = s.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "op1", "password": "Op1password123", "role": "operator",
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}

	// 重复用户名冲突
	rec = s.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "op1", "password": "Op1password123", "role": "operator",
	}, adminToken)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}

	// operator 登录
	opToken := s.login("op1", "Op1password123")

	// operator 不可管理用户
	rec = s.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "op2", "password": "Op2password123", "role": "operator",
	}, opToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for operator, got %d", rec.Code)
	}

	// operator 可改自己的密码
	rec = s.do(http.MethodPut, "/api/v1/users/me/password", map[string]any{
		"old_password": "Op1password123", "new_password": "Op1newpassword456",
	}, opToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}

	// 刷新会话（cookie 已在 cookies 中）
	rec = s.do(http.MethodPost, "/api/v1/auth/refresh", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}

	// 登出后刷新应失败
	rec = s.do(http.MethodPost, "/api/v1/auth/logout", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: %d", rec.Code)
	}
	rec = s.do(http.MethodPost, "/api/v1/auth/refresh", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", rec.Code)
	}

	// 审计日志可查（分页信封）
	rec = s.do(http.MethodGet, "/api/v1/audit-logs", nil, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit logs: %d %s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	decodeData(t, rec, &page)
}

func TestLoginLockout(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)

	for i := 0; i < 5; i++ {
		rec := s.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
			"username": "admin", "password": "wrong-password",
		}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, rec.Code)
		}
	}
	rec := s.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123456",
	}, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after lockout, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestLoginResponseMustChangePassword(t *testing.T) {
	s := newTestServer(t)
	u := s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	u.MustChangePassword = true
	if err := s.userStore.Update(s.t.Context(), u); err != nil {
		t.Fatal(err)
	}

	rec := s.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123456",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		MustChangePassword bool `json:"must_change_password"`
	}
	decodeData(t, rec, &resp)
}

func TestPasswordComplexityEnforced(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "weak", "password": "onlylowercase", "role": "operator",
	}, adminToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for weak password, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestOperatorCannotReadAuditLogs(t *testing.T) {
	s := newTestServer(t)
	s.seedUser("admin", "adminpass123456", domain.RoleAdmin)
	adminToken := s.login("admin", "adminpass123456")

	rec := s.do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "op1", "password": "Op1password123", "role": "operator",
	}, adminToken)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create op: %d", rec.Code)
	}
	opToken := s.login("op1", "Op1password123")

	rec = s.do(http.MethodGet, "/api/v1/audit-logs", nil, opToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for operator audit read, got %d", rec.Code)
	}
}
