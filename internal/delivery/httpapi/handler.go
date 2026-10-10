package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/webassets"
)

type Handler struct {
	router    chi.Router
	jwt       jwtVerifier
	users     domain.UserStore
	certStore domain.CertificateStore
	orders    domain.OrderStore
	tasks     domain.TaskLogStore
	scan      ScanRunner
	tokens    *application.ApiTokenService
	auth      *application.AuthService
	audit     *application.AuditService
	notifySvc *application.NotificationService
	deployRecs domain.DeployRecordStore
	sendLogs  domain.NotificationSendLogStore
	accessTTL time.Duration
}

type jwtVerifier interface {
	VerifyAccessToken(token string) (*domain.User, error)
}

type HandlerDeps struct {
	Users               domain.UserStore
	CertStore           domain.CertificateStore
	TaskLogs            domain.TaskLogStore
	Orders              domain.OrderStore
	Auth                *application.AuthService
	UserService         *application.UserService
	AuditService        *application.AuditService
	CertService         *application.CertificateService
	AccountService      *application.ProviderAccountService
	DnsAccountService   *application.DnsProviderAccountService
	NotificationService *application.NotificationService
	ChannelService      *application.ChannelService
	DeployService       *application.DeployService
	ApiTokens           *application.ApiTokenService
	AgentService        *application.AgentService
	ContactService      *application.ContactService
	DeployRecords       domain.DeployRecordStore
	SendLogs            domain.NotificationSendLogStore
	Agents              domain.AgentStore
	ScanRunner          ScanRunner
	Cryptor             application.Cryptor
	JWT                 jwtVerifier
	ProviderNames       []string
	AccessTTL           time.Duration
	RefreshTTL          time.Duration
	AuditStore          domain.AuditStore
	SetupConfigPath     string
	AppVersion          string
	RunningAsService    bool
}

// ScanRunner 手动触发扫描（SchedulerService 实现）
type ScanRunner interface {
	RunScan(ctx context.Context) error
}

func NewHandler(cfg config.Config, deps HandlerDeps) *Handler {
	h := &Handler{
		jwt:       deps.JWT,
		users:     deps.Users,
		certStore: deps.CertStore,
		orders:    deps.Orders,
		tasks:     deps.TaskLogs,
		scan:      deps.ScanRunner,
		tokens:    deps.ApiTokens,
		auth:      deps.Auth,
		audit:     deps.AuditService,
		notifySvc: deps.NotificationService,
		deployRecs: deps.DeployRecords,
		sendLogs:  deps.SendLogs,
		accessTTL: deps.AccessTTL,
	}
	authHandler := newAuthHandler(deps.Auth, deps.AccessTTL, deps.RefreshTTL, cfg.Auth.CookieSecure)
	userHandler := newUserHandler(deps.UserService)
	certHandler := newCertificateHandler(deps.CertService, deps.DeployService, deps.Cryptor, deps.AuditService)
	accountHandler := newAccountHandler(deps.AccountService, deps.ProviderNames)
	dnsAccountHandler := newDnsAccountHandler(deps.DnsAccountService)
	ruleHandler := newNotificationRuleHandler(deps.NotificationService, deps.TaskLogs)
	channelHandler := newChannelHandler(deps.ChannelService)
	sendLogHandler := newSendLogHandler(deps.NotificationService)
	templateHandler := newTemplateHandler(deps.NotificationService)
	deployHandler := newDeployHandler(deps.DeployService)
	tokenHandler := newTokenHandler(deps.ApiTokens)
	agentHandler := newAgentHandler(deps.AgentService)
	contactHandler := newContactHandler(deps.ContactService)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// 豁免：探活端点保持裸格式（运维标准）
		writeRawJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// OpenAPI 3 规范（Swagger UI 通过 /openapi.yaml 加载）
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(webassets.OpenAPISpec())
	})

	// Prometheus 指标（含用户/证书/部署等运营计数）：需管理员认证，避免匿名暴露运营信息。
	// 抓取端须携带管理员 JWT 或具备 admin 权限的 API Token。
	m := &metricsHandler{deps: deps, start: time.Now()}
	r.Group(func(r chi.Router) {
		r.Use(h.authenticate)
		r.Use(requireRole(domain.RoleAdmin))
		r.Get("/metrics", m.ServeHTTP)
	})

	// 初始安装向导：单个 /setup 挂载点（公开 status/init + 认证 restart），chi 同路径不可重复 Mount
	setupHandler := newSetupHandler(deps, cfg, h.authenticate)

	// 常规接口 60s 超时；证书签发/续期/吊销可达 ACME 5min 超时，不挂超时
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Mount("/auth", authHandler.Routes())
		})

		// Agent 出站轮询接口（agent token 认证，不走用户认证）
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Mount("/agent", agentHandler.AgentRoutes())
		})

		// 初始安装向导（未初始化前公开 status/init；restart 走认证管理员通道）
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Mount("/setup", setupHandler.routes())
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Use(h.authenticate)
			r.Get("/stats", h.stats)
			r.Put("/users/me/password", userHandler.changePassword)
			r.Get("/audit-logs", h.listAudit)
			r.Get("/audit-logs/export", h.exportAudit)
			r.Get("/task-logs", h.listTasks)
			r.Mount("/users", userHandler.Routes())
			r.Mount("/notification-rules", ruleHandler.Routes())
			r.Mount("/notification-channels", channelHandler.Routes())
			r.Mount("/notification-send-logs", sendLogHandler.Routes())
			r.Mount("/notification-templates", templateHandler.Routes())
			r.Mount("/api-tokens", tokenHandler.Routes())
			r.Mount("/settings/contacts", contactHandler.Routes())
			r.Mount("/agents", agentHandler.AdminRoutes())
		})

		// 任务日志导出与发送历史导出含运维细节，仅 admin/operator（发送历史列表在 Routes 内同样限制）
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Use(h.authenticate)
			r.Use(requireRole(domain.RoleAdmin, domain.RoleOperator))
			r.Get("/task-logs/export", h.exportTaskLogs)
			r.Get("/notification-send-logs/export", sendLogHandler.Export)
		})

		// 证书与渠道操作：ACME 签发可能超过 60s，不套用 Timeout
		r.Group(func(r chi.Router) {
			r.Use(h.authenticate)
			r.Mount("/certificates", certHandler.Routes())
			r.Mount("/acme-accounts", accountHandler.Routes())
			r.Mount("/dns-accounts", dnsAccountHandler.Routes())
			r.Mount("/deploy-targets", deployHandler.Routes())
			r.Post("/task-logs/scan", h.triggerScan)
		})
	})

	h.router = r
	return h
}

// WithWebUI 包装前端产物托管（SPA 回退）。
func WithWebUI(api http.Handler) http.Handler {
	return webassets.Handler(api)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	total, err := h.certStore.Count(r.Context())
	if err != nil {
		total = 0
	}
	expiring, err := h.certStore.CountExpiring(r.Context(), 30*24*time.Hour)
	if err != nil {
		expiring = 0
	}
	expired, err := h.certStore.CountExpired(r.Context())
	if err != nil {
		expired = 0
	}
	rate := 0.0
	if h.orders != nil {
		success, renewTotal, err := h.orders.RenewStats(r.Context(), time.Now().Add(-90*24*time.Hour))
		if err == nil && renewTotal > 0 {
			rate = float64(success) / float64(renewTotal)
		}
	}
	// 近 12 个月续期趋势
	trend := []map[string]any{}
	if h.orders != nil {
		points, err := h.orders.RenewalTrend(r.Context(), time.Now().AddDate(0, -11, 0))
		if err == nil {
			for _, pt := range points {
				trend = append(trend, map[string]any{
					"month":   pt.Month.Format("2006-01"),
					"success": pt.Success,
					"total":   pt.Total,
				})
			}
		}
	}
	// 部署/通知成功率
	deployRate := 0.0
	if h.deployRecs != nil {
		okN, _ := h.deployRecs.CountByStatus(r.Context(), "success")
		failN, _ := h.deployRecs.CountByStatus(r.Context(), "failed")
		if okN+failN > 0 {
			deployRate = float64(okN) / float64(okN+failN)
		}
	}
	notifyRate := 0.0
	if h.sendLogs != nil {
		okN, _ := h.sendLogs.CountByStatus(r.Context(), "success")
		failN, _ := h.sendLogs.CountByStatus(r.Context(), "failed")
		if okN+failN > 0 {
			notifyRate = float64(okN) / float64(okN+failN)
		}
	}
	writeOK(w, r, map[string]any{
		"total_certificates":      total,
		"expiring_within_30d":     expiring,
		"expired":                 expired,
		"auto_renew_success_rate": rate,
		"renewal_trend":           trend,
		"deploy_success_rate":     deployRate,
		"notify_success_rate":     notifyRate,
	})
}

// pageResponse 分页信封（data 层）
type pageResponse struct {
	Items  any   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeOK(w, r, pageResponse{Items: []any{}, Total: 0, Limit: 0, Offset: 0})
		return
	}
	limit, offset := pagination(r)
	logs, err := h.tasks.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	total, _ := h.tasks.Count(r.Context())
	out := make([]taskLogResponse, 0, len(logs))
	for _, l := range logs {
		out = append(out, toTaskLogResponse(l))
	}
	writeOK(w, r, pageResponse{Items: out, Total: total, Limit: limit, Offset: offset})
}

type taskLogResponse struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	Detail     string  `json:"detail"`
	Summary    string  `json:"summary,omitempty"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

func toTaskLogResponse(l *domain.TaskLog) taskLogResponse {
	out := taskLogResponse{
		ID:        l.ID,
		Type:      string(l.Type),
		Status:    string(l.Status),
		Detail:    l.Detail,
		Summary:   l.Summary,
		StartedAt: l.StartedAt.Format(time.RFC3339),
	}
	if l.FinishedAt != nil {
		ts := l.FinishedAt.Format(time.RFC3339)
		out.FinishedAt = &ts
	}
	return out
}

func (h *Handler) triggerScan(w http.ResponseWriter, r *http.Request) {
	p := userFrom(r.Context())
	if p == nil || (p.User.Role != domain.RoleAdmin && p.User.Role != domain.RoleOperator) {
		writeError(w, r, http.StatusForbidden, "无权限")
		return
	}
	if h.scan == nil {
		writeError(w, r, http.StatusNotImplemented, "调度器未启用")
		return
	}
	done := make(chan error, 1)
	go func() {
		// 扫描独立于请求生命周期执行：客户端断开不中止扫描，
		// 避免 ACME 续期中途取消、任务日志停在 running（总数上限兜底防泄漏）
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		done <- h.scan.RunScan(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			if errors.Is(err, application.ErrScanInProgress) {
				writeError(w, r, http.StatusConflict, "上一轮扫描仍在执行，请稍后重试")
				return
			}
			writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, r, map[string]bool{"ok": true})
	case <-r.Context().Done():
		writeError(w, r, http.StatusGatewayTimeout, "请求超时：扫描仍在后台执行，请稍后在任务日志查看结果")
	}
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	p := userFrom(r.Context())
	if p == nil || p.User.Role != domain.RoleAdmin {
		writeError(w, r, http.StatusForbidden, "无权限")
		return
	}
	limit, offset := pagination(r)
	filter := domain.AuditQuery{
		Username: r.URL.Query().Get("username"),
		Action:   r.URL.Query().Get("action"),
		Keyword:  r.URL.Query().Get("keyword"),
	}
	logs, err := h.audit.List(r.Context(), filter, limit, offset)
	if err != nil {
		writeError(w, r, mapError(err), err.Error())
		return
	}
	total, _ := h.audit.Count(r.Context(), filter)
	writeOK(w, r, pageResponse{Items: logs, Total: total, Limit: limit, Offset: offset})
}
