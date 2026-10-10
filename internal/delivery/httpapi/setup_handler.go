package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
	appcrypto "github.com/iamartinlong/Argus/internal/infrastructure/crypto"
)

// setupHandler 可视化初始安装：公开 status/generate-keys/init，重启走认证管理员通道。
type setupHandler struct {
	users            domain.UserStore
	auditStore       domain.AuditStore
	minPassLen       int
	maxLoginFailures int
	lockoutMinutes   int
	cfg              config.Config
	configPath       string
	version          string
	runningAsService bool
	jwtFromEnv       bool
	masterFromEnv    bool
	// auth 认证中间件（完整模式由 NewHandler 注入；安装模式为 nil，restart 走重启令牌）。
	auth func(http.Handler) http.Handler
	// setupOnly 安装模式降级服务（无 auth/业务路由，初始化后必须重启进完整模式）。
	setupOnly bool
	// restartToken 安装模式重启令牌（/setup/init 下发）；restartInFlight 防止并发拉起多个子进程
	restartToken    string
	restartInFlight bool
	mu              sync.Mutex
}

func newSetupHandler(deps HandlerDeps, cfg config.Config, auth func(http.Handler) http.Handler) *setupHandler {
	minLen := cfg.Auth.MinPasswordLen
	if minLen <= 0 {
		minLen = 12
	}
	maxFail := cfg.Auth.MaxLoginFailures
	if maxFail <= 0 {
		maxFail = 5
	}
	lockout := cfg.Auth.LockoutMinutes
	if lockout <= 0 {
		lockout = 15
	}
	jwtEnvName := cfg.Security.JWTSecretEnv
	if jwtEnvName == "" {
		jwtEnvName = "ARGUS_JWT_SECRET"
	}
	jwtEnvVal, jwtEnv := os.LookupEnv(jwtEnvName)
	masterEnvName := cfg.Security.MasterKeyEnv
	if masterEnvName == "" {
		masterEnvName = "ARGUS_MASTER_KEY"
	}
	masterEnvVal, masterEnv := os.LookupEnv(masterEnvName)
	// 与 config.Load 对齐：ENV 接管时内存值以 ENV 为准（测试直接构造 cfg 时文件值为空）。
	if jwtEnv {
		cfg.Security.JWTSecret = jwtEnvVal
	}
	if masterEnv {
		cfg.Security.MasterKey = masterEnvVal
	}
	return &setupHandler{
		users:            deps.Users,
		auditStore:       deps.AuditStore,
		minPassLen:       minLen,
		maxLoginFailures: maxFail,
		lockoutMinutes:   lockout,
		cfg:              cfg,
		configPath:       deps.SetupConfigPath,
		version:          deps.AppVersion,
		runningAsService: deps.RunningAsService,
		jwtFromEnv:       jwtEnv && cfg.Security.JWTSecretEnv != "",
		masterFromEnv:    masterEnv,
		auth:             auth,
	}
}

// routes 单个 /setup 挂载点：公开 status/generate-keys/init；
// restart 在完整模式走认证管理员通道，安装模式（auth==nil）仅未初始化时公开。
func (s *setupHandler) routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/status", s.status)
	r.Post("/generate-keys", s.generateKeys)
	r.Post("/init", s.init)
	if s.auth != nil {
		r.Group(func(r chi.Router) {
			r.Use(s.auth)
			r.Use(requireRole(domain.RoleAdmin))
			r.Post("/restart", s.doRestart)
		})
	} else {
		r.Post("/restart", s.restartWithToken)
	}
	return r
}

// restartWithToken 安装模式（无认证）下的重启：需携带 /setup/init 返回的一次性令牌。
// 令牌只发给完成初始化的调用方，因此初始化后匿名者无法重启进程，
// 而向导仍能完成"初始化→重启进完整模式"。
func (s *setupHandler) restartWithToken(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Setup-Token")
	s.mu.Lock()
	expected := s.restartToken
	inFlight := s.restartInFlight
	if expected != "" && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1 {
		s.restartInFlight = true
	}
	s.mu.Unlock()
	if expected == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		writeError(w, r, http.StatusForbidden, "缺少有效的安装重启令牌：请先完成初始化并使用 /setup/init 返回的 restart_token")
		return
	}
	if inFlight {
		writeError(w, r, http.StatusConflict, "重启已在进行中")
		return
	}
	s.doRestart(w, r)
}

// NewSetupOnlyHandler 安装模式专用 handler（serve 降级用）：仅 healthz + setup 公开接口。
func NewSetupOnlyHandler(cfg config.Config, users domain.UserStore, audit domain.AuditStore, configPath, version string, runningAsService bool) http.Handler {
	s := newSetupHandler(HandlerDeps{
		Users:            users,
		AuditStore:       audit,
		SetupConfigPath:  configPath,
		AppVersion:       version,
		RunningAsService: runningAsService,
	}, cfg, nil)
	s.setupOnly = true
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeRawJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			r.Mount("/setup", s.routes())
		})
	})
	return r
}

func (s *setupHandler) isInitialized(ctx context.Context) (bool, error) {
	if s.users == nil {
		return false, nil
	}
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *setupHandler) status(w http.ResponseWriter, r *http.Request) {
	done, err := s.isInitialized(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "查询初始化状态失败: "+err.Error())
		return
	}
	writeOK(w, r, map[string]any{
		"initialized":        done,
		"version":            s.version,
		"jwt_from_env":       s.jwtFromEnv,
		"master_from_env":    s.masterFromEnv,
		"min_password_len":   s.minPassLen,
		"config_file":        s.configPath != "",
		"running_as_service": s.runningAsService,
		"setup_only":         s.setupOnly,
	})
}

func (s *setupHandler) generateKeys(w http.ResponseWriter, r *http.Request) {
	jwtB := make([]byte, 48)
	masterB := make([]byte, 32)
	if _, err := rand.Read(jwtB); err != nil {
		writeError(w, r, http.StatusInternalServerError, "生成随机数失败")
		return
	}
	if _, err := rand.Read(masterB); err != nil {
		writeError(w, r, http.StatusInternalServerError, "生成随机数失败")
		return
	}
	writeOK(w, r, map[string]string{
		"jwt_secret": base64.StdEncoding.EncodeToString(jwtB),
		"master_key": base64.StdEncoding.EncodeToString(masterB),
	})
}

type setupInitRequest struct {
	JWTSecret        *string `json:"jwt_secret"`
	MasterKey        *string `json:"master_key"`
	MinPasswordLen   *int    `json:"min_password_len"`
	MaxLoginFailures *int    `json:"max_login_failures"`
	LockoutMinutes   *int    `json:"lockout_minutes"`
	AdminUsername    string  `json:"admin_username"`
	AdminPassword    string  `json:"admin_password"`
}

func (s *setupHandler) init(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	done, err := s.isInitialized(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "查询初始化状态失败: "+err.Error())
		return
	}
	if done {
		writeError(w, r, http.StatusConflict, "系统已初始化，无需重复安装")
		return
	}

	var req setupInitRequest
	if !readJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.AdminUsername)
	if username == "" || len(username) > 64 {
		writeError(w, r, http.StatusBadRequest, "管理员用户名不能为空且不超过 64 字符")
		return
	}
	if req.AdminPassword == "" {
		writeError(w, r, http.StatusBadRequest, "管理员密码不能为空")
		return
	}

	effMin := s.minPassLen
	if req.MinPasswordLen != nil {
		if *req.MinPasswordLen < 8 || *req.MinPasswordLen > 64 {
			writeError(w, r, http.StatusBadRequest, "min_password_len 需在 8~64 之间")
			return
		}
		effMin = *req.MinPasswordLen
	}
	effMaxFail := s.maxLoginFailures
	if req.MaxLoginFailures != nil {
		if *req.MaxLoginFailures < 1 || *req.MaxLoginFailures > 100 {
			writeError(w, r, http.StatusBadRequest, "max_login_failures 需在 1~100 之间")
			return
		}
		effMaxFail = *req.MaxLoginFailures
	}
	effLockout := s.lockoutMinutes
	if req.LockoutMinutes != nil {
		if *req.LockoutMinutes < 1 || *req.LockoutMinutes > 1440 {
			writeError(w, r, http.StatusBadRequest, "lockout_minutes 需在 1~1440 之间")
			return
		}
		effLockout = *req.LockoutMinutes
	}

	// 密钥：ENV 接管时忽略请求值（运行时以 ENV 为准，避免误导）。
	newJWT := s.cfg.Security.JWTSecret
	if !s.jwtFromEnv && req.JWTSecret != nil && strings.TrimSpace(*req.JWTSecret) != "" {
		newJWT = strings.TrimSpace(*req.JWTSecret)
	}
	newMaster := s.cfg.Security.MasterKey
	if !s.masterFromEnv && req.MasterKey != nil && strings.TrimSpace(*req.MasterKey) != "" {
		newMaster = strings.TrimSpace(*req.MasterKey)
	}
	if len(newJWT) < 32 {
		writeError(w, r, http.StatusBadRequest, "jwt_secret 长度至少 32 字符（可点“一键生成”）")
		return
	}
	if len(newMaster) < 32 {
		writeError(w, r, http.StatusBadRequest, "master_key 长度至少 32 字节（可点“一键生成”）")
		return
	}
	if _, err := appcrypto.NewAESGCM(newMaster); err != nil {
		writeError(w, r, http.StatusBadRequest, "master_key 不合法: "+err.Error())
		return
	}

	// 密码按新 min 长度 + 复杂度预检（与 UserService 规则一致）。
	if err := validateSetupPassword(req.AdminPassword, effMin); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	secretsChanged := newJWT != s.cfg.Security.JWTSecret || newMaster != s.cfg.Security.MasterKey
	authChanged := effMin != s.minPassLen || effMaxFail != s.maxLoginFailures || effLockout != s.lockoutMinutes

	// 写回 config.yaml（有路径时）。
	// 用 LoadFileRaw 读取文件原始值（不注入 ENV）：ENV 接管的密钥字段保持文件原样，
	// 避免 ENV 值经 Load→Save 往返被明文落盘；校验用副本才合并 ENV 值。
	if s.configPath != "" {
		fileCfg, err := config.LoadFileRaw(s.configPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// 向导打开后配置文件被删除：回退默认配置完成初始化
				fileCfg = config.Default()
			} else {
				writeError(w, r, http.StatusInternalServerError, "读取配置文件失败: "+err.Error())
				return
			}
		}
		// ENV 接管的密钥字段保持文件原值不动（运行时以 ENV 为准，且不把 ENV 值落盘）
		if !s.jwtFromEnv {
			fileCfg.Security.JWTSecret = newJWT
		}
		if !s.masterFromEnv {
			fileCfg.Security.MasterKey = newMaster
		}
		fileCfg.Auth.MinPasswordLen = effMin
		fileCfg.Auth.MaxLoginFailures = effMaxFail
		fileCfg.Auth.LockoutMinutes = effLockout
		// 校验副本：ENV 接管的密钥以运行时（ENV）值参与校验
		validCfg := fileCfg
		if s.jwtFromEnv {
			validCfg.Security.JWTSecret = s.cfg.Security.JWTSecret
		}
		if s.masterFromEnv {
			validCfg.Security.MasterKey = s.cfg.Security.MasterKey
		}
		if err := validCfg.Validate(); err != nil {
			writeError(w, r, http.StatusBadRequest, "配置校验失败: "+err.Error())
			return
		}
		if raw, err := os.ReadFile(s.configPath); err == nil {
			_ = os.WriteFile(s.configPath+".bak", raw, 0o600)
		}
		if err := config.Save(s.configPath, fileCfg); err != nil {
			writeError(w, r, http.StatusInternalServerError, "写回配置文件失败: "+err.Error())
			return
		}
		// 运行时配置保持 ENV 合并后的口径（ENV 覆盖文件值）
		s.cfg = fileCfg
		if s.jwtFromEnv {
			s.cfg.Security.JWTSecret = newJWT
		}
		if s.masterFromEnv {
			s.cfg.Security.MasterKey = newMaster
		}
		s.minPassLen = effMin
		s.maxLoginFailures = effMaxFail
		s.lockoutMinutes = effLockout
	} else {
		// 无配置文件路径（测试/默认）：仅更新内存。
		s.cfg.Security.JWTSecret = newJWT
		s.cfg.Security.MasterKey = newMaster
		s.cfg.Auth.MinPasswordLen = effMin
		s.cfg.Auth.MaxLoginFailures = effMaxFail
		s.cfg.Auth.LockoutMinutes = effLockout
		s.minPassLen = effMin
		s.maxLoginFailures = effMaxFail
		s.lockoutMinutes = effLockout
	}

	// 创建初始管理员（向导内已设强密码，不强制改密）。
	// 用新 min 长度构造独立 UserService，避免启动时旧 min 与本次提交不一致。
	userSvc := application.NewUserService(s.users, s.auditStore, nil, application.RealClock{}, effMin)
	actor := &domain.User{ID: "system", Username: "system", Role: domain.RoleAdmin}
	if _, err := userSvc.CreateUser(r.Context(), actor, username, req.AdminPassword, domain.RoleAdmin); err != nil {
		writeError(w, r, mapError(err), "创建管理员失败: "+err.Error())
		return
	}

	// 安装模式下的重启令牌：仅发给完成初始化的调用方，
	// 使"初始化→重启进完整模式"的向导流程可用，同时避免匿名者可随意重启
	restartToken, err := newRestartToken()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "生成重启令牌失败: "+err.Error())
		return
	}
	s.restartToken = restartToken

	writeOK(w, r, map[string]any{
		"ok":              true,
		"admin_username":  username,
		"needs_restart":   secretsChanged || authChanged,
		"secrets_changed": secretsChanged,
		"restart_token":   restartToken,
	})
}

// newRestartToken 生成安装模式重启令牌（32 字节随机 hex）
func newRestartToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validateSetupPassword(pw string, minLen int) error {
	if len(pw) < minLen {
		return fmt.Errorf("密码长度至少 %d 位", minLen)
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range pw {
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
		return fmt.Errorf("密码必须包含大写字母、小写字母和数字")
	}
	return nil
}

// doRestart 尝试拉起新进程后退出（服务模式下降级为手动指引）。
// 完整模式经 requireRole(admin) 前置；安装模式仅在未初始化时经 restartWhenUninitialized 放行。
func (s *setupHandler) doRestart(w http.ResponseWriter, r *http.Request) {
	if s.runningAsService {
		writeOK(w, r, map[string]any{
			"restarting":           false,
			"needs_manual_restart": true,
			"message":              "当前以 Windows 服务运行，请执行 Restart-Service Argus 使新配置生效",
			"service_name":         "Argus",
			"powershell":           "Restart-Service Argus",
		})
		return
	}
	exe, err := os.Executable()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "获取可执行路径失败: "+err.Error())
		return
	}
	env := append(os.Environ(), "ARGUS_RESTART_DELAY=2s")
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = env
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		writeError(w, r, http.StatusInternalServerError, "拉起新进程失败: "+err.Error()+"，请手动重启服务")
		return
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	writeOK(w, r, map[string]any{"restarting": true})
}
