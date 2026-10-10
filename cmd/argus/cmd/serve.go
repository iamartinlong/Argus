package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/delivery/httpapi"
	appcrypto "github.com/iamartinlong/Argus/internal/infrastructure/crypto"
	deployreg "github.com/iamartinlong/Argus/internal/infrastructure/deploy"
	"github.com/iamartinlong/Argus/internal/infrastructure/notify"
	acmeprovider "github.com/iamartinlong/Argus/internal/infrastructure/providers/acme"
	aliyunprovider "github.com/iamartinlong/Argus/internal/infrastructure/providers/aliyun"
	tencentprovider "github.com/iamartinlong/Argus/internal/infrastructure/providers/tencent"
	"github.com/iamartinlong/Argus/internal/infrastructure/providers"
	"github.com/iamartinlong/Argus/internal/infrastructure/scheduler"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/dbexec"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
	"github.com/iamartinlong/Argus/internal/domain"
)

// bootstrap 本地模式依赖装配（serve 与本地 CLI 共用）
type bootstrap struct {
	db         dbexec.SQLDB
	cfg        config.Config
	certSvc    *application.CertificateService
	accountSvc *application.ProviderAccountService
	dnsAccountSvc *application.DnsProviderAccountService
	deploySvc  *application.DeployService
	notifySvc  *application.NotificationService
	channelSvc *application.ChannelService
	tokenSvc   *application.ApiTokenService
	agentSvc   *application.AgentService
	contactSvc *application.ContactService
	runner     *scheduler.Runner
	schedSvc   *application.SchedulerService
	registry   *providers.Registry
	deployRecs *sqlite.DeployRecordStore
	sendLogs   *sqlite.NotificationSendLogStore
	agents     *sqlite.AgentStore
	stop       chan struct{} // close() 时关闭，通知后台 goroutine 退出
}

func newBootstrap(configPath string, requireJWT bool) (*bootstrap, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		if requireJWT || !strings.Contains(err.Error(), "jwt secret") {
			return nil, err
		}
		// 本地 CLI 模式无需 JWT：允许缺失，serve 时仍会校验
		cfg.Security.JWTSecret = "cli-only-secret-not-used-0123456789abcdef"
	}
	// 后台 goroutine（心跳检测/日志清理）退出信号
	stop := make(chan struct{})
	db, err := storage.Open(cfg.Storage.Driver, cfg.Storage.DSN)
	if err != nil {
		return nil, err
	}
	masterKey := cfg.Security.MasterKey
	masterEnv := cfg.Security.MasterKeyEnv
	if masterEnv == "" {
		masterEnv = "ARGUS_MASTER_KEY"
	}
	if masterKey == "" {
		masterKey = os.Getenv(masterEnv)
	}
	if masterKey == "" {
		db.Close()
		return nil, fmt.Errorf("缺少主密钥：设置环境变量 %s 或 security.master_key（argus gen-keys 可生成）", masterEnv)
	}
	aes, err := appcrypto.NewAESGCM(masterKey)
	if err != nil {
		db.Close()
		return nil, err
	}
	registry := providers.NewRegistry()
	if cfg.ACME.CAFile != "" {
		acmeProvider, err := acmeprovider.NewWithCA(aes, cfg.ACME.CAFile)
		if err != nil {
			db.Close()
			return nil, err
		}
		registry.Register(acmeProvider)
	} else {
		registry.Register(acmeprovider.New(aes))
	}
	registry.Register(aliyunprovider.New(aes))
	registry.Register(tencentprovider.New(aes))

	auditStore := sqlite.NewAuditStore(db)
	certStore := sqlite.NewCertificateStore(db)
	orderStore := sqlite.NewOrderStore(db)
	accountStore := sqlite.NewProviderAccountStore(db)
	ruleStore := sqlite.NewNotificationRuleStore(db)
	notifyLogStore := sqlite.NewNotificationLogStore(db)
	sendLogStore := sqlite.NewNotificationSendLogStore(db)
	taskLogStore := sqlite.NewTaskLogStore(db)
	deployTargetStore := sqlite.NewDeployTargetStore(db)
	deployRecordStore := sqlite.NewDeployRecordStore(db)
	apiTokenStore := sqlite.NewApiTokenStore(db)
	agentStore := sqlite.NewAgentStore(db)
	agentTaskStore := sqlite.NewAgentTaskStore(db)

	accountSvc := application.NewProviderAccountService(accountStore, certStore, auditStore, registry, aes, application.RealClock{})
	dnsAccountStore := sqlite.NewDnsProviderAccountStore(db)
	dnsAccountSvc := application.NewDnsProviderAccountService(dnsAccountStore, certStore, auditStore, aes, application.RealClock{})
	contactStore := sqlite.NewContactStore(db)
	contactSvc := application.NewContactService(contactStore, auditStore, application.RealClock{})
	eventBus := application.NewEventBus()
	certSvc := application.NewCertificateService(
		certStore, orderStore, accountStore, contactStore, auditStore, registry, aes, application.RealClock{}, cfg.ACME.HTTP01Port, eventBus)
	certSvc.SetDnsAccounts(dnsAccountStore)

	channelStore := sqlite.NewNotificationChannelStore(db)
	channelSvc := application.NewChannelService(channelStore, ruleStore, auditStore,
		notify.NewBuilder(), aes, application.RealClock{})
	// 种子导入：config.yaml 通知渠道 → DB（一次性，表空时）；旧规则回填 channel_ids
	seedCount, _ := channelSvc.SeedFromConfig(context.Background(), func() []application.ChannelInput {
		return configChannels(cfg)
	})
	if seedCount > 0 {
		log.Printf("已导入 %d 个通知渠道（来自 config.yaml 初始化配置）", seedCount)
	}
	_, _ = channelSvc.BackfillLegacyRuleChannelIDs(context.Background())
	notifySvc := application.NewNotificationService(ruleStore, notifyLogStore, sendLogStore, sqlite.NewNotificationTemplateStore(db), auditStore, channelSvc, application.RealClock{})
	notifySvc.AttachEventBus(eventBus)

	deployRegistry := deployreg.NewRegistry()
	deployRegistry.Register(deployreg.Local{})
	deployRegistry.Register(deployreg.NewSSH(30 * time.Second))
	deployRegistry.Register(deployreg.NewWebhook())
	deployRegistry.Register(deployreg.K8s{})
	deploySvc := application.NewDeployService(
		deployTargetStore, deployRecordStore, certStore, auditStore, deployRegistry, aes, application.RealClock{}, eventBus)
	tokenSvc := application.NewApiTokenService(apiTokenStore, auditStore, application.RealClock{})
	agentSvc := application.NewAgentService(agentStore, agentTaskStore, deployTargetStore, certStore,
		deployRecordStore, auditStore, aes, application.RealClock{}, eventBus)
	agentSvc.SetBinaryDir(filepath.Join(filepath.Dir(cfg.Storage.DSN), "agent-binaries"))
	deploySvc.SetAgentTaskCleaner(agentTaskStore)
	deployRegistry.Register(application.NewAgentAdapter(agentSvc, application.RealClock{}))

	runner := scheduler.NewRunner(taskLogStore, application.RealClock{})
	schedSvc := application.NewSchedulerService(certSvc, notifySvc, runner, application.RealClock{},
		cfg.Scheduler.RenewDaysBefore, nil)
	if cfg.Scheduler.Enabled {
		scanMinutes := cfg.Scheduler.ScanMinutes
		if scanMinutes <= 0 {
			scanMinutes = 60
		}
		runner.StartWithSummary(scanMinutes, func(ctx context.Context) (string, error) {
			summaryStr, err := schedSvc.RunScan(ctx)
			if err != nil {
				// 上一轮尚未结束（扫描耗时 > 扫描间隔等）：跳过本轮，不算失败
				if errors.Is(err, application.ErrScanInProgress) {
					return "跳过本轮：上一轮扫描仍在执行", nil
				}
				return summaryStr, err
			}
			// 清理超时未回执的 agent 任务并联动部署记录（计入摘要）
			_ = agentSvc.ReconcileStaleAgentTasks(ctx, 10*time.Minute)
			_ = deploySvc.RetryFailed(ctx)
			// 摘要已由 RunScan 产生，部署/清理计数在 RunScan 内聚合（后续可扩展合并）
			return summaryStr, nil
		})
	}

	// 日志保留策略：独立每日任务（不依赖调度器开关）
	startMaintenance(cfg, db, taskLogStore, auditStore, notifyLogStore, sendLogStore, stop)

	// Agent 心跳超时检测：将超时未心跳的在线 Agent 置为离线
	go func() {
		ctx := context.Background()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n, err := agentSvc.ReconcileStaleOffline(ctx, 90*time.Second); err != nil {
					log.Printf("agent 心跳超时检测失败: %v", err)
				} else if n > 0 {
					log.Printf("已置 %d 个超时 Agent 为离线", n)
				}
			}
		}
	}()

	return &bootstrap{
		db: db, cfg: cfg, certSvc: certSvc, accountSvc: accountSvc, dnsAccountSvc: dnsAccountSvc, deploySvc: deploySvc,
		notifySvc: notifySvc, channelSvc: channelSvc, tokenSvc: tokenSvc, agentSvc: agentSvc,
		contactSvc: contactSvc,
		runner: runner, schedSvc: schedSvc, registry: registry,
		deployRecs: deployRecordStore, sendLogs: sendLogStore, agents: agentStore,
		stop: stop,
	}, nil
}

// startMaintenance 每日日志保留清理（任务/审计/通知日志；0=禁用）+ WAL checkpoint
func startMaintenance(cfg config.Config, db dbexec.SQLDB, taskLogs *sqlite.TaskLogStore, audit *sqlite.AuditStore,
	notifyLogs *sqlite.NotificationLogStore, sendLogs *sqlite.NotificationSendLogStore, stop <-chan struct{}) {
	run := func() {
		now := time.Now().UTC()
		if cfg.Scheduler.TaskLogRetentionDays > 0 {
			cutoff := now.Add(-time.Duration(cfg.Scheduler.TaskLogRetentionDays) * 24 * time.Hour)
			if n, err := taskLogs.DeleteOlderThan(context.Background(), cutoff); err == nil && n > 0 {
				log.Printf("已清理 %d 条过期任务日志", n)
			}
		}
		if cfg.Scheduler.AuditLogRetentionDays > 0 {
			cutoff := now.Add(-time.Duration(cfg.Scheduler.AuditLogRetentionDays) * 24 * time.Hour)
			if n, err := audit.DeleteOlderThan(context.Background(), cutoff); err == nil && n > 0 {
				log.Printf("已清理 %d 条过期审计日志", n)
			}
		}
		if cfg.Scheduler.NotifyLogRetentionDays > 0 {
			ret := cfg.Scheduler.NotifyLogRetentionDays
			// 防重日志早于最长提醒提前期被清理 → 同一到期提醒会重复发送，故设下限
			if ret < application.MaxExpiryReminderDays {
				log.Printf("notify_log_retention_days=%d 小于最长提醒提前期 %d 天，已按下限清理以避免重复提醒",
					ret, application.MaxExpiryReminderDays)
				ret = application.MaxExpiryReminderDays
			}
			cutoff := now.Add(-time.Duration(ret) * 24 * time.Hour)
			if n, err := sendLogs.DeleteOlderThan(context.Background(), cutoff); err == nil && n > 0 {
				log.Printf("已清理 %d 条过期通知发送日志", n)
			}
			if n, err := notifyLogs.DeleteOlderThan(context.Background(), cutoff); err == nil && n > 0 {
				log.Printf("已清理 %d 条过期通知防重记录", n)
			}
		}
	}
	// 定期 checkpoint（WAL → 主库）：避免 WAL 无限增长、崩溃恢复依赖单一文件
	checkpoint := func() {
		if _, err := db.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			log.Printf("WAL checkpoint 失败: %v", err)
		}
	}
	checkpoint()
	run()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				checkpoint()
				run()
			}
		}
	}()
}

func (b *bootstrap) close() {
	if b.stop != nil {
		close(b.stop) // 通知后台 goroutine（心跳检测/日志清理）退出
	}
	if b.runner != nil {
		b.runner.Stop()
	}
	b.db.Close()
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 Web 服务与调度器",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 重启延迟（/setup/restart 拉起的子进程等待父进程释放端口）
		if d := os.Getenv("ARGUS_RESTART_DELAY"); d != "" {
			if secs, err := config.ParseDuration(d, "ARGUS_RESTART_DELAY"); err == nil && secs > 0 {
				log.Printf("等待 %s 后启动（重启接力）...", d)
				time.Sleep(time.Duration(secs) * time.Second)
			}
		}
		// 默认配置路径：保证 /setup/restart 拉起的子进程与向导写回的目标一致
		if configPath == "" {
			configPath = "config.yaml"
		}
		// 配置文件不存在 → 首次安装，直接进入安装模式（quickstart 首启流程）
		if _, err := os.Stat(configPath); err != nil && errors.Is(err, fs.ErrNotExist) {
			log.Printf("配置文件 %s 不存在，进入可视化安装模式...", configPath)
			return runSetupOnly()
		}
		b, err := newBootstrap(configPath, true)
		if err != nil {
			if isMissingSecrets(err) {
				log.Printf("检测到未初始化（%v），进入可视化安装模式...", err)
				return runSetupOnly()
			}
			return err
		}
		defer b.close()
		return b.runServer()
	},
}

// isMissingSecrets 启动失败是否因缺密钥/未初始化（可降级为安装向导模式）。
func isMissingSecrets(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "jwt secret") || strings.Contains(msg, "master key") || strings.Contains(msg, "主密钥")
}

// runSetupOnly 降级安装模式：仅挂载 healthz + setup 公开接口 + 前端，供首次可视化配置。
func runSetupOnly() error {
	cfg, cfgPath := loadLenientConfig(configPath)
	db, err := storage.Open(cfg.Storage.Driver, cfg.Storage.DSN)
	if err != nil {
		return err
	}
	defer db.Close()
	userStore := sqlite.NewUserStore(db)
	// 已有用户说明系统初始化过：此时进安装模式只会因密钥配错/库被换等原因发生，
	// 必须大声失败而不是降级成公开的安装服务（/setup/* 无鉴权，且业务 API 全部消失）。
	if n, err := userStore.Count(context.Background()); err == nil && n > 0 {
		return fmt.Errorf("检测到系统已初始化（存在 %d 个用户），拒绝进入安装模式；请检查 security 密钥配置或环境变量是否正确", n)
	}
	auditStore := sqlite.NewAuditStore(db)
	handler := httpapi.NewSetupOnlyHandler(cfg, userStore, auditStore, cfgPath, version, isServiceMode())
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           httpapi.WithWebUI(handler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("Argus %s 安装模式 listening on %s（请浏览器打开完成初始配置）", version, cfg.Server.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadLenientConfig 宽容加载：配置文件不存在时回退默认值（首次安装），其他错误透出。
func loadLenientConfig(path string) (config.Config, string) {
	if path == "" {
		return config.Default(), ""
	}
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "read config file") {
			log.Printf("配置文件 %s 不存在，使用默认配置进入安装模式", path)
			cfg = config.Default()
		} else {
			log.Printf("配置文件读取告警: %v，使用默认配置进入安装模式", err)
			cfg = config.Default()
		}
	}
	return cfg, path
}

func (b *bootstrap) runServer() error {
	accessTTL, err := durationConfig(b.cfg.Auth.AccessTokenTTL, "auth.access_token_ttl")
	if err != nil {
		return err
	}
	refreshTTL, err := durationConfig(b.cfg.Auth.RefreshTokenTTL, "auth.refresh_token_ttl")
	if err != nil {
		return err
	}
	masterKey := b.cfg.Security.MasterKey
	if masterKey == "" {
		masterKey = os.Getenv("ARGUS_MASTER_KEY")
	}
	aes, err := appcrypto.NewAESGCM(masterKey)
	if err != nil {
		return fmt.Errorf("初始化凭据加密器失败（检查 security.master_key/%s）: %w", b.cfg.Security.MasterKeyEnv, err)
	}

	userStore := sqlite.NewUserStore(b.db)
	sessionStore := sqlite.NewSessionStore(b.db)
	auditStore := sqlite.NewAuditStore(b.db)
	certStore := sqlite.NewCertificateStore(b.db)
	orderStore := sqlite.NewOrderStore(b.db)
	taskLogStore := sqlite.NewTaskLogStore(b.db)

	jwtSvc, err := appcrypto.NewJWTService(b.cfg.Security.JWTSecret, accessTTL)
	if err != nil {
		return err
	}
	auditSvc := application.NewAuditService(auditStore)
	authSvc := application.NewAuthService(
		userStore, sessionStore, auditStore, jwtSvc, application.RealClock{},
		accessTTL, refreshTTL, b.cfg.Auth.MaxLoginFailures,
		time.Duration(b.cfg.Auth.LockoutMinutes)*time.Minute)
	userSvc := application.NewUserService(userStore, auditStore, authSvc, application.RealClock{}, b.cfg.Auth.MinPasswordLen)

	apiHandler := httpapi.NewHandler(b.cfg, httpapi.HandlerDeps{
		Users:               userStore,
		CertStore:           certStore,
		TaskLogs:            taskLogStore,
		Orders:              orderStore,
		Auth:                authSvc,
		UserService:         userSvc,
		AuditService:        auditSvc,
		AuditStore:          auditStore,
		CertService:         b.certSvc,
		AccountService:      b.accountSvc,
		DnsAccountService:   b.dnsAccountSvc,
		NotificationService: b.notifySvc,
		ChannelService:      b.channelSvc,
		DeployService:       b.deploySvc,
		DeployRecords:       b.deployRecs,
		SendLogs:            b.sendLogs,
		Agents:              b.agents,
		ApiTokens:           b.tokenSvc,
		AgentService:        b.agentSvc,
		ContactService:      b.contactSvc,
		ScanRunner:          scanAdapter{runner: b.runner, sched: b.schedSvc},
		Cryptor:             aes,
		JWT:                 jwtSvc,
		ProviderNames:       b.registry.Names(),
		AccessTTL:           accessTTL,
		RefreshTTL:          refreshTTL,
		SetupConfigPath:     configPath,
		AppVersion:          version,
		RunningAsService:    isServiceMode(),
	})
	handler := httpapi.WithWebUI(apiHandler)

	srv := &http.Server{
		Addr:              b.cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      6 * time.Minute, // 覆盖 ACME 签发最长 5 分钟
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("Argus %s listening on %s", version, b.cfg.Server.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

var initAdminCmd = &cobra.Command{
	Use:   "init-admin",
	Short: "创建初始管理员（系统中无用户时）",
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")
		return initAdmin(username, password)
	},
}

func init() {
	initAdminCmd.Flags().String("username", "admin", "管理员用户名")
	initAdminCmd.Flags().String("password", "", "管理员密码（不传则自动生成）")
}

func initAdmin(username, password string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	db, err := storage.Open(cfg.Storage.Driver, cfg.Storage.DSN)
	if err != nil {
		return err
	}
	defer db.Close()
	userStore := sqlite.NewUserStore(db)
	count, err := userStore.Count(context.Background())
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("系统中已存在用户，无需初始化")
	}
	pwd := password
	if pwd == "" {
		pwd, err = randomPassword()
		if err != nil {
			return fmt.Errorf("生成初始密码失败: %w", err)
		}
	}
	actor := &domain.User{ID: "system", Username: "system", Role: domain.RoleAdmin}
	userSvc := application.NewUserService(
		userStore, sqlite.NewAuditStore(db), nil, application.RealClock{}, cfg.Auth.MinPasswordLen)
	user, err := userSvc.CreateUser(context.Background(), actor, username, pwd, domain.RoleAdmin)
	if err != nil {
		return err
	}
	user.MustChangePassword = true
	if err := userStore.Update(context.Background(), user); err != nil {
		return err
	}
	fmt.Printf("已创建初始管理员: %s\n", user.Username)
	if password == "" {
		fmt.Printf("初始密码: %s（请登录后立即修改）\n", pwd)
	}
	return nil
}

var genKeysCmd = &cobra.Command{
	Use:   "gen-keys",
	Short: "生成随机密钥（环境变量或 config.yaml security 段）",
	Run: func(cmd *cobra.Command, args []string) {
		jwtB := make([]byte, 48)
		masterB := make([]byte, 32)
		_, _ = rand.Read(jwtB)
		_, _ = rand.Read(masterB)
		fmt.Println("JWT 密钥（环境变量 ARGUS_JWT_SECRET 或 security.jwt_secret）:")
		fmt.Println(base64.StdEncoding.EncodeToString(jwtB))
		fmt.Println("主密钥（环境变量 ARGUS_MASTER_KEY 或 security.master_key）:")
		fmt.Println(base64.StdEncoding.EncodeToString(masterB))
	},
}

// configChannels 将 config.yaml 通知配置转为种子渠道（仅首次初始化使用）
func configChannels(cfg config.Config) []application.ChannelInput {
	var out []application.ChannelInput
	if cfg.Notify.SMTP.Host != "" {
		out = append(out, application.ChannelInput{
			Type:   domain.ChannelSMTP,
			Config: map[string]any{"host": cfg.Notify.SMTP.Host, "port": cfg.Notify.SMTP.Port, "username": cfg.Notify.SMTP.Username, "password_env": cfg.Notify.SMTP.PasswordEnv, "from": cfg.Notify.SMTP.From, "use_tls": cfg.Notify.SMTP.UseTLS},
		})
	}
	if cfg.Notify.Webhook.URL != "" {
		out = append(out, application.ChannelInput{
			Type:   domain.ChannelWebhook,
			Config: map[string]any{"url": cfg.Notify.Webhook.URL, "secret": cfg.Notify.Webhook.Secret},
		})
	}
	if cfg.Notify.IM.WeCom.WebhookURL != "" {
		out = append(out, application.ChannelInput{
			Type:   domain.ChannelWeCom,
			Config: map[string]any{"webhook_url": cfg.Notify.IM.WeCom.WebhookURL, "key": cfg.Notify.IM.WeCom.Key},
		})
	}
	if cfg.Notify.IM.DingTalk.WebhookURL != "" {
		out = append(out, application.ChannelInput{
			Type:   domain.ChannelDingTalk,
			Config: map[string]any{"webhook_url": cfg.Notify.IM.DingTalk.WebhookURL, "key": cfg.Notify.IM.DingTalk.Key},
		})
	}
	if cfg.Notify.IM.Feishu.WebhookURL != "" {
		out = append(out, application.ChannelInput{
			Type:   domain.ChannelFeishu,
			Config: map[string]any{"webhook_url": cfg.Notify.IM.Feishu.WebhookURL},
		})
	}
	return out
}

// scanAdapter 手动触发扫描：复用调度器的任务日志包装
type scanAdapter struct {
	runner *scheduler.Runner
	sched  *application.SchedulerService
}

func (a scanAdapter) RunScan(ctx context.Context) error {
	return a.runner.RunTaskWithSummary(ctx, domain.TaskScanCertificates, a.sched.RunScan)
}

func durationConfig(s, name string) (time.Duration, error) {
	secs, err := config.ParseDuration(s, name)
	if err != nil {
		return 0, err
	}
	return time.Duration(secs) * time.Second, nil
}

// randomPassword 生成随机初始密码；随机源失败必须报错，不能退化成可预测密码
func randomPassword() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---------------- 远程 API 客户端 ----------------

type remoteClient struct {
	baseURL string
	token   string
	http    *http.Client
	// 写操作（签发/续期/部署等）可能耗时较长，使用独立超时
	longHTTP *http.Client
}

func newRemoteClient() (*remoteClient, error) {
	token := apiToken()
	if token == "" {
		return nil, fmt.Errorf("远程模式需要 --token 或环境变量 ARGUS_API_TOKEN")
	}
	return &remoteClient{
		baseURL:  strings.TrimRight(remoteServer, "/"),
		token:    token,
		http:     &http.Client{Timeout: 30 * time.Second},
		longHTTP: &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (c *remoteClient) do(method, path string, body any) ([]byte, error) {
	client := c.http
	// 写操作可能触发 ACME 签发/部署（可达数分钟），使用长超时
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete {
		client = c.longHTTP
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Token", c.token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		// 错误信封：{code, message, trace_id, ...}
		var e struct {
			Message string `json:"message"`
			Error   string `json:"error"`
			Code    string `json:"code"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Message
		if msg == "" {
			msg = e.Error
		}
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		if e.Code != "" {
			return nil, fmt.Errorf("API %d (%s): %s", resp.StatusCode, e.Code, msg)
		}
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, msg)
	}
	// 成功信封：解包 data 层
	var env struct {
		Code any            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err == nil && env.Data != nil && len(env.Data) > 0 && env.Data[0] != 'n' {
		return env.Data, nil
	}
	return data, nil
}
