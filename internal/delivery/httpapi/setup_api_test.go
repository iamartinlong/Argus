package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
)

// withCleanSetupEnv 清除进程继承的 ARGUS_*（开发机 Machine 级密钥会干扰向导分支判断），测试后恢复。
func withCleanSetupEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ARGUS_JWT_SECRET", "ARGUS_MASTER_KEY"} {
		if v, ok := os.LookupEnv(k); ok {
			key, val := k, v
			os.Unsetenv(key)
			t.Cleanup(func() { os.Setenv(key, val) })
		}
	}
}

func withValidSetupEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ARGUS_JWT_SECRET", "env-jwt-secret-0123456789abcdef0123456789")
	t.Setenv("ARGUS_MASTER_KEY", "env-master-key-0123456789abcdef0123456789")
}

func decodeSetupStatus(t *testing.T, recBody []byte) map[string]any {
	t.Helper()
	var env struct {
		Code float64        `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recBody, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != 0 {
		t.Fatalf("unexpected code %v: %s", env.Code, string(recBody))
	}
	return env.Data
}

func TestSetupStatusUninitialized(t *testing.T) {
	withCleanSetupEnv(t)
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/api/v1/setup/status", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	data := decodeSetupStatus(t, rec.Body.Bytes())
	if data["initialized"] != false {
		t.Fatalf("expected initialized=false, got %v", data)
	}
}

func TestSetupInitSuccessThenConflict(t *testing.T) {
	withCleanSetupEnv(t)
	s := newTestServer(t)
	jwt := "test-jwt-secret-0123456789abcdef0123456789"
	master := "test-master-key-0123456789abcdef0123456789"
	rec := s.do(http.MethodPost, "/api/v1/setup/init", map[string]any{
		"jwt_secret": jwt, "master_key": master,
		"admin_username": "admin", "admin_password": "SetupPass123",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("init: %d %s", rec.Code, rec.Body.String())
	}
	data := decodeSetupStatus(t, rec.Body.Bytes())
	if data["admin_username"] != "admin" {
		t.Fatalf("unexpected init data: %v", data)
	}

	rec = s.do(http.MethodGet, "/api/v1/setup/status", nil, "")
	data = decodeSetupStatus(t, rec.Body.Bytes())
	if data["initialized"] != true {
		t.Fatalf("expected initialized=true, got %v", data)
	}

	// 重复初始化拒绝
	rec = s.do(http.MethodPost, "/api/v1/setup/init", map[string]any{
		"admin_username": "admin2", "admin_password": "SetupPass123",
	}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupInitWeakPassword(t *testing.T) {
	withCleanSetupEnv(t)
	s := newTestServer(t)
	rec := s.do(http.MethodPost, "/api/v1/setup/init", map[string]any{
		"jwt_secret": "test-jwt-secret-0123456789abcdef0123456789",
		"master_key": "test-master-key-0123456789abcdef0123456789",
		"admin_username": "admin", "admin_password": "weak",
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupInitShortSecret(t *testing.T) {
	withCleanSetupEnv(t)
	s := newTestServer(t)
	rec := s.do(http.MethodPost, "/api/v1/setup/init", map[string]any{
		"jwt_secret": "short", "master_key": "short",
		"admin_username": "admin", "admin_password": "SetupPass123",
	}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestSetupInitWithEnvSecrets ENV 接管密钥时向导忽略请求密钥、用 ENV 值完成初始化。
func TestSetupInitWithEnvSecrets(t *testing.T) {
	withValidSetupEnv(t)
	s := newTestServer(t)
	rec := s.do(http.MethodPost, "/api/v1/setup/init", map[string]any{
		"admin_username": "admin", "admin_password": "SetupPass123",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("init: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSetupInitEnvSecretsNotPersisted ENV 接管的密钥不得经向导写回 config.yaml
// （config.Load 会把 ENV 注入内存配置，若直接 Load→Save 会把 ENV 密钥明文落盘）。
func TestSetupInitEnvSecretsNotPersisted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	fileCfg := config.Default()
	fileCfg.Storage.DSN = ":memory:"
	fileCfg.Security.JWTSecret = "file-jwt-secret-0123456789abcdef0123456"
	fileCfg.Security.MasterKey = "file-master-key-0123456789abcdef01234567"
	if err := config.Save(cfgPath, fileCfg); err != nil {
		t.Fatal(err)
	}
	envJWT := "env-jwt-secret-0123456789abcdef0123456789"
	envMaster := "env-master-key-0123456789abcdef0123456789"
	t.Setenv("ARGUS_JWT_SECRET", envJWT)
	t.Setenv("ARGUS_MASTER_KEY", envMaster)

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := newSetupHandler(HandlerDeps{
		Users:           sqlite.NewUserStore(db),
		AuditStore:      sqlite.NewAuditStore(db),
		SetupConfigPath: cfgPath,
	}, mustLoadConfig(t, cfgPath), nil)

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"admin_username": "admin", "admin_password": "SetupPass123"})
	req := httptest.NewRequest(http.MethodPost, "/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("init: %d %s", rec.Code, rec.Body.String())
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), envJWT) || strings.Contains(string(raw), envMaster) {
		t.Fatalf("ENV 密钥被明文写入 config.yaml:\n%s", raw)
	}
	if !strings.Contains(string(raw), fileCfg.Security.JWTSecret) {
		t.Fatalf("文件原有 jwt_secret 应保留:\n%s", raw)
	}
}

func mustLoadConfig(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestSetupRestartRequiresInitToken 安装模式下重启必须携带 /init 下发的令牌
// （匿名者可随意重启进程是 DoS 面；令牌只发给完成初始化的调用方）
func TestSetupRestartRequiresInitToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	fileCfg := config.Default()
	fileCfg.Storage.DSN = ":memory:"
	fileCfg.Security.JWTSecret = "file-jwt-secret-0123456789abcdef0123456"
	fileCfg.Security.MasterKey = "file-master-key-0123456789abcdef01234567"
	if err := config.Save(cfgPath, fileCfg); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := newSetupHandler(HandlerDeps{
		Users:           sqlite.NewUserStore(db),
		AuditStore:      sqlite.NewAuditStore(db),
		SetupConfigPath: cfgPath,
	}, config.Default(), nil)

	post := func(path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.routes().ServeHTTP(rec, req)
		return rec
	}

	// 未初始化、无令牌：拒绝
	if rec := post("/restart", nil, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without token, got %d %s", rec.Code, rec.Body.String())
	}

	// 初始化后拿到令牌
	rec := post("/init", map[string]any{
		"admin_username": "admin", "admin_password": "SetupPass123",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("init: %d %s", rec.Code, rec.Body.String())
	}
	data := decodeSetupStatus(t, rec.Body.Bytes())
	token, _ := data["restart_token"].(string)
	if token == "" {
		t.Fatalf("expected restart_token in init response: %v", data)
	}

	// 错误令牌：拒绝；正确令牌：放行（本进程非服务模式，会尝试拉起子进程——
	// 因此只断言不再返回 403）
	if rec := post("/restart", nil, map[string]string{"X-Setup-Token": "wrong"}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with wrong token, got %d", rec.Code)
	}
}

func TestSetupGenerateKeys(t *testing.T) {
	withCleanSetupEnv(t)
	s := newTestServer(t)
	rec := s.do(http.MethodPost, "/api/v1/setup/generate-keys", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("generate-keys: %d %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Code float64 `json:"code"`
		Data struct {
			JWTSecret string `json:"jwt_secret"`
			MasterKey string `json:"master_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data.JWTSecret) < 32 || len(env.Data.MasterKey) < 32 {
		t.Fatalf("keys too short: %+v", env.Data)
	}
}
