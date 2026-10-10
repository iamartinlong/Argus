package testutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/delivery/httpapi"
	"github.com/iamartinlong/Argus/internal/domain"
	appcrypto "github.com/iamartinlong/Argus/internal/infrastructure/crypto"
	"github.com/iamartinlong/Argus/internal/infrastructure/deploy"
	"github.com/iamartinlong/Argus/internal/infrastructure/notify"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"

	"golang.org/x/crypto/bcrypt"
)

// LiveTestServer 起真实监听端口的中心端（供 Docker 容器内的 agent 出站访问）。
type LiveTestServer struct {
	BaseURL    string
	Port       int
	Handler    http.Handler
	DB         *sqlite.DB
	userStore  *sqlite.UserStore
	AuthSvc    *application.AuthService
	UserSvc    *application.UserService
	CertSvc    *application.CertificateService
	DeploySvc  *application.DeployService
	AgentSvc   *application.AgentService
	ContactSvc *application.ContactService
}

// SeedUser 直接写入一个用户（绕过 audit，供测试登录用）。
func (s *LiveTestServer) SeedUser(t *testing.T, username, password string, role domain.Role) *domain.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
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
	if err := s.userStore.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}

// NewLiveTestServer 组装完整中心端并监听 0.0.0.0 随机端口。
// 内存 SQLite（:memory: + SetMaxOpenConns(1)）单连接，真实 HTTP 多请求数据一致。
func NewLiveTestServer(t *testing.T) *LiveTestServer {
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
	contactStore := sqlite.NewContactStore(db)
	contactSvc := application.NewContactService(contactStore, auditStore, application.RealClock{})
	eventBus := application.NewEventBus()
	certSvc := application.NewCertificateService(
		certStore, orderStore, accountStore, contactStore, auditStore, reg, aes, application.RealClock{}, 80, eventBus)

	ruleStore := sqlite.NewNotificationRuleStore(db)
	notifyLogStore := sqlite.NewNotificationLogStore(db)
	taskLogStore := sqlite.NewTaskLogStore(db)
	channelSvc := application.NewChannelService(sqlite.NewNotificationChannelStore(db), ruleStore, auditStore,
		notify.NewBuilder(), aes, application.RealClock{})
	notifySvc := application.NewNotificationService(ruleStore, notifyLogStore, sqlite.NewNotificationSendLogStore(db),
		sqlite.NewNotificationTemplateStore(db), auditStore, channelSvc, application.RealClock{})

	deployTargetStore := sqlite.NewDeployTargetStore(db)
	deployRecordStore := sqlite.NewDeployRecordStore(db)
	apiTokenStore := sqlite.NewApiTokenStore(db)
	agentStore := sqlite.NewAgentStore(db)
	agentTaskStore := sqlite.NewAgentTaskStore(db)

	deployRegistry := deploy.NewRegistry()
	deployRegistry.Register(deploy.Local{})
	deployRegistry.Register(deploy.NewSSH(30 * time.Second))
	deployRegistry.Register(deploy.NewWebhook())
	deployRegistry.Register(deploy.K8s{})
	deploySvc := application.NewDeployService(
		deployTargetStore, deployRecordStore, certStore, auditStore, deployRegistry, aes, application.RealClock{}, eventBus)
	tokenSvc := application.NewApiTokenService(apiTokenStore, auditStore, application.RealClock{})
	agentSvc := application.NewAgentService(agentStore, agentTaskStore, deployTargetStore, certStore,
		deployRecordStore, auditStore, aes, application.RealClock{}, eventBus)
	agentSvc.SetBinaryDir(filepath.Join(t.TempDir(), "agent-binaries"))
	deploySvc.SetAgentTaskCleaner(agentTaskStore)
	deployRegistry.Register(application.NewAgentAdapter(agentSvc, application.RealClock{}))

	handler := httpapi.NewHandler(cfg, httpapi.HandlerDeps{
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
		DeployService:       deploySvc,
		ApiTokens:           tokenSvc,
		AgentService:        agentSvc,
		ContactService:      contactSvc,
		DeployRecords:       deployRecordStore,
		SendLogs:            sqlite.NewNotificationSendLogStore(db),
		Agents:              agentStore,
		Cryptor:             aes,
		JWT:                 jwtSvc,
		ProviderNames:       []string{"acme"},
		AccessTTL:           accessTTL,
		RefreshTTL:          refreshTTL,
	})

	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	return &LiveTestServer{
		BaseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		Port:       port,
		Handler:    handler,
		DB:         db,
		userStore:  userStore,
		AuthSvc:    authSvc,
		UserSvc:    userSvc,
		CertSvc:    certSvc,
		DeploySvc:  deploySvc,
		AgentSvc:   agentSvc,
		ContactSvc: contactSvc,
	}
}

// testProviderRegistry 最小签发渠道注册表（ProviderEngine），e2e 仅导入证书不触发真实签发。
type testProviderRegistry struct{}

func (testProviderRegistry) Get(name string) (providers.CertificateProvider, error) {
	return nil, fmt.Errorf("未注册的证书渠道: %s", name)
}

func (testProviderRegistry) Names() []string { return []string{"acme"} }

// newTestRegistry 返回空签发渠道注册表
func newTestRegistry() testProviderRegistry {
	return testProviderRegistry{}
}
