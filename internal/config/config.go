package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Storage  StorageConfig  `yaml:"storage"`
	Security SecurityConfig `yaml:"security"`
	Auth     AuthConfig     `yaml:"auth"`
	ACME     ACMEConfig     `yaml:"acme"`
	Notify   NotifyConfig   `yaml:"notification"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

type SchedulerConfig struct {
	Enabled                 bool `yaml:"enabled"` // 默认 true（零值时构造时补齐）
	ScanMinutes             int  `yaml:"scan_minutes"` // 扫描间隔（默认 60）
	RenewDaysBefore         int  `yaml:"renew_days_before"` // 到期前 N 天自动续期（默认 30）
	TaskLogRetentionDays    int  `yaml:"task_log_retention_days"` // 任务日志保留天数（默认 90，0 禁用清理）
	AuditLogRetentionDays   int  `yaml:"audit_log_retention_days"` // 审计日志保留天数（默认 0=禁用）
	NotifyLogRetentionDays  int  `yaml:"notify_log_retention_days"` // 通知发送/防重日志保留天数（默认 0=禁用）
}

type NotifyConfig struct {
	SMTP    SMTPConfig    `yaml:"smtp"`
	Webhook WebhookConfig `yaml:"webhook"`
	IM      IMConfig      `yaml:"im"`
}

type SMTPConfig struct {
	Host        string `yaml:"host" json:"host"`
	Port        int    `yaml:"port" json:"port"`
	Username    string `yaml:"username" json:"username"`
	Password    string `yaml:"password" json:"password"`         // 界面配置的密码（AES-GCM 加密存储）
	PasswordEnv string `yaml:"password_env" json:"password_env"` // 兼容：密码来自环境变量（config.yaml 种子渠道用）
	From        string `yaml:"from" json:"from"`
	UseTLS      bool   `yaml:"use_tls" json:"use_tls"` // true=465 隐式 TLS；false 尝试 STARTTLS
}

type WebhookConfig struct {
	URL    string `yaml:"url" json:"url"`
	Secret string `yaml:"secret" json:"secret"`
}

type IMConfig struct {
	WeCom    IMWebhook `yaml:"wecom"`
	DingTalk IMWebhook `yaml:"dingtalk"`
	Feishu   IMWebhook `yaml:"feishu"`
}

type IMWebhook struct {
	WebhookURL string `yaml:"webhook_url" json:"webhook_url"`
	Key        string `yaml:"key" json:"key"` // 企业微信机器人 key / 钉钉签名 secret
}

type ACMEConfig struct {
	CAFile     string `yaml:"ca_file"`      // 自定义 CA 池（自建 ACME CA / 测试），空则系统信任
	HTTP01Port int    `yaml:"http01_port"`  // HTTP-01 验证服务器端口（0 默认 80）
}

type ServerConfig struct {
	Listen string `yaml:"listen"`
}

type StorageConfig struct {
	Driver string `yaml:"driver"` // sqlite | postgres
	DSN    string `yaml:"dsn"`
}

type SecurityConfig struct {
	JWTSecretEnv string `yaml:"jwt_secret_env"`
	JWTSecret    string `yaml:"jwt_secret"`
	MasterKeyEnv string `yaml:"master_key_env"`
	// MasterKey 优先读环境变量（MasterKeyEnv），为空时读本字段（可视化安装向导落盘）。
	// 文件权限建议 0600；已有 ENV 部署不受影响（ENV 覆盖文件值）。
	MasterKey string `yaml:"master_key"`
}

type AuthConfig struct {
	AccessTokenTTL   string `yaml:"access_token_ttl"`   // e.g. 15m
	RefreshTokenTTL  string `yaml:"refresh_token_ttl"`  // e.g. 720h
	MaxLoginFailures int    `yaml:"max_login_failures"`
	LockoutMinutes   int    `yaml:"lockout_minutes"`
	MinPasswordLen   int    `yaml:"min_password_len"`
	CookieSecure     bool   `yaml:"cookie_secure"` // 生产经 TLS 时为 true
}

func Default() Config {
	return Config{
		Server: ServerConfig{Listen: ":8443"},
		Storage: StorageConfig{
			Driver: "sqlite",
			DSN:    "./data/argus.db",
		},
		Security: SecurityConfig{
			JWTSecretEnv: "ARGUS_JWT_SECRET",
			MasterKeyEnv: "ARGUS_MASTER_KEY",
		},
		Auth: AuthConfig{
			AccessTokenTTL:   "15m",
			RefreshTokenTTL:  "720h",
			MaxLoginFailures: 5,
			LockoutMinutes:   15,
			MinPasswordLen:   12,
		},
		Scheduler: SchedulerConfig{
			Enabled:              true,
			ScanMinutes:          60,
			RenewDaysBefore:      30,
			TaskLogRetentionDays: 90,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config file: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config file: %w", err)
		}
	}
	if v := os.Getenv(cfg.Security.JWTSecretEnv); v != "" {
		cfg.Security.JWTSecret = v
	}
	if v := os.Getenv(cfg.Security.MasterKeyEnv); v != "" {
		cfg.Security.MasterKey = v
	}
	return cfg, nil
}

// LoadFileRaw 读取文件配置但不注入环境变量密钥（安装向导回写用），
// 保证 ENV 中的密钥不会被意外持久化到磁盘。
func LoadFileRaw(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config file: %w", err)
	}
	return cfg, nil
}

// Save 将配置写回 YAML 文件（可视化安装向导用）。
// 调用方负责先备份原文件；写入权限 0600（Windows 下 ACL 由系统继承，此处尽力约束）。
func Save(path string, cfg Config) error {
	if path == "" {
		return fmt.Errorf("config path is empty")
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			if i == 0 {
				return path[:1]
			}
			return path[:i]
		}
	}
	return "."
}

func (c Config) Validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	if c.Storage.Driver != "sqlite" && c.Storage.Driver != "postgres" {
		return fmt.Errorf("storage.driver must be sqlite or postgres, got %q", c.Storage.Driver)
	}
	if c.Storage.Driver == "postgres" && c.Storage.DSN == "" {
		return fmt.Errorf("storage.dsn is required for postgres driver")
	}
	if c.Security.JWTSecret == "" {
		return fmt.Errorf("jwt secret is required (set %s env or security.jwt_secret)", c.Security.JWTSecretEnv)
	}
	if len(c.Security.JWTSecret) < 32 {
		return fmt.Errorf("jwt secret must be at least 32 characters")
	}
	// MasterKey 与 crypto/aes.decodeKey 对齐：hex/base64 解码 32B 或原文 >=32。
	// 此处用长度作轻量代理（hex 64 / base64 44 / 原文 >=32 均通过），精确校验由 NewAESGCM 完成。
	if c.Security.MasterKey == "" {
		return fmt.Errorf("master key is required (set %s env or security.master_key)", c.Security.MasterKeyEnv)
	}
	if len(c.Security.MasterKey) < 32 {
		return fmt.Errorf("master key must be >= 32 bytes (hex/base64/raw)")
	}
	// TTL 合法性（解析失败/负数直接拒绝，避免登录静默失效）
	for _, ttl := range []struct {
		v    string
		name string
	}{
		{c.Auth.AccessTokenTTL, "auth.access_token_ttl"},
		{c.Auth.RefreshTokenTTL, "auth.refresh_token_ttl"},
	} {
		if ttl.v == "" {
			continue
		}
		n, err := ParseDuration(ttl.v, ttl.name)
		if err != nil || n <= 0 {
			return fmt.Errorf("%s must be a positive duration (Ns/Nm/Nh), got %q", ttl.name, ttl.v)
		}
	}
	if c.Scheduler.ScanMinutes > 24*60 {
		return fmt.Errorf("scheduler.scan_minutes must be <= 1440, got %d", c.Scheduler.ScanMinutes)
	}
	return nil
}

func ParseDuration(s, name string) (int64, error) {
	// minimal duration parser: Ns/Nm/Nh（拒绝负数）
	if s == "" {
		return 0, fmt.Errorf("%s is empty", name)
	}
	unit := s[len(s)-1]
	num, err := strconv.ParseInt(strings.TrimSuffix(s, string(unit)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, s)
	}
	if num <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", name, s)
	}
	switch unit {
	case 's':
		return num, nil
	case 'm':
		return num * 60, nil
	case 'h':
		return num * 3600, nil
	default:
		return 0, fmt.Errorf("unsupported %s unit %q", name, s)
	}
}
