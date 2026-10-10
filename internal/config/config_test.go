package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := Default()
	if c.Server.Listen != ":8443" {
		t.Fatalf("listen: %q", c.Server.Listen)
	}
	if c.Storage.Driver != "sqlite" {
		t.Fatalf("driver: %q", c.Storage.Driver)
	}
	if c.Auth.AccessTokenTTL != "15m" {
		t.Fatalf("access ttl: %q", c.Auth.AccessTokenTTL)
	}
	if c.Scheduler.TaskLogRetentionDays != 90 {
		t.Fatalf("retention: %d", c.Scheduler.TaskLogRetentionDays)
	}
	if c.Security.JWTSecretEnv != "ARGUS_JWT_SECRET" {
		t.Fatalf("jwt env: %q", c.Security.JWTSecretEnv)
	}
}

func TestLoadYamlAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `server:
  listen: ":9999"
security:
  jwt_secret_env: CUSTOM_JWT_ENV
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CUSTOM_JWT_ENV", "env-secret-0123456789abcdef0123456789abcdef")
	t.Setenv("ARGUS_MASTER_KEY", "master-key-0123456789abcdef0123456789abcdef")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":9999" {
		t.Fatalf("yaml override failed: %q", cfg.Server.Listen)
	}
	if cfg.Security.JWTSecret != "env-secret-0123456789abcdef0123456789abcdef" {
		t.Fatalf("env override failed: %q", cfg.Security.JWTSecret)
	}
	if cfg.Security.MasterKey != "master-key-0123456789abcdef0123456789abcdef" {
		t.Fatalf("master key env failed: %q", cfg.Security.MasterKey)
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"30s", 30, false},
		{"15m", 900, false},
		{"2h", 7200, false},
		{"", 0, true},
		{"15x", 0, true},
		{"abc", 0, true},
		{"-15m", 0, true}, // 负数拒绝
		{"0s", 0, true},   // 零拒绝
	}
	for _, c := range cases {
		got, err := ParseDuration(c.in, "test")
		if c.err {
			if err == nil {
				t.Fatalf("ParseDuration(%q): expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("ParseDuration(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	base := Default()
	base.Security.JWTSecret = "test-secret-0123456789abcdef0123456789abcdef"
	base.Security.MasterKey = "master-key-0123456789abcdef0123456789abcdef"

	if err := base.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	// 短 JWT secret 拒绝
	bad := base
	bad.Security.JWTSecret = "short"
	if err := bad.Validate(); err == nil {
		t.Fatal("short jwt secret accepted")
	}

	// 非法 TTL 拒绝
	bad = base
	bad.Auth.AccessTokenTTL = "15x"
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid ttl accepted")
	}
	bad = base
	bad.Auth.RefreshTokenTTL = "-1h"
	if err := bad.Validate(); err == nil {
		t.Fatal("negative ttl accepted")
	}

	// postgres 缺 DSN 拒绝
	bad = base
	bad.Storage.Driver = "postgres"
	bad.Storage.DSN = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("postgres without dsn accepted")
	}

	// 未知驱动拒绝
	bad = base
	bad.Storage.Driver = "mysql"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown driver accepted")
	}
}
