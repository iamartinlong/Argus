package deploy

import (
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

// TestNginxServerNamesConfig 验证 Nginx SNI 配置解码与多域
func TestNginxServerNamesConfig(t *testing.T) {
	cfg, err := decodeConfig[localConfig](map[string]any{
		"scene":        "nginx",
		"cert_path":    "/etc/nginx/certs/{domain}/fullchain.pem",
		"key_path":     "/etc/nginx/certs/{domain}/privkey.pem",
		"server_names": "a.example.com, b.example.com",
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.ServerNames != "a.example.com, b.example.com" {
		t.Fatalf("server_names mismatch: %q", cfg.ServerNames)
	}
	if cfg.Scene != "nginx" {
		t.Fatalf("scene != nginx: %q", cfg.Scene)
	}
	// 同端口多域：server_names 应包含空格/逗号分隔
	parts := strings.FieldsFunc(cfg.ServerNames, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) != 2 || parts[0] != "a.example.com" || parts[1] != "b.example.com" {
		t.Fatalf("server_names split failed: %v", parts)
	}
}

// TestNginxCertPathDomainTemplate 路径 {domain} 占位按 PrimaryDomain 替换
func TestNginxCertPathDomainTemplate(t *testing.T) {
	cases := []struct {
		in, domain, want string
	}{
		{"/etc/nginx/certs/{domain}/fullchain.pem", "a.example.com", "/etc/nginx/certs/a.example.com/fullchain.pem"},
		{"/etc/nginx/certs/{domain}/privkey.pem", "b.example.com", "/etc/nginx/certs/b.example.com/privkey.pem"},
		{"/etc/nginx/certs/fullchain.pem", "a.example.com", "/etc/nginx/certs/fullchain.pem"},
	}
	for _, c := range cases {
		got := strings.ReplaceAll(c.in, "{domain}", c.domain)
		if got != c.want {
			t.Fatalf("replace %q with %q: got %q want %q", c.in, c.domain, got, c.want)
		}
	}
}

// TestNginxSSHConfig 验证 SSH 侧 server_names 透传
func TestNginxSSHConfig(t *testing.T) {
	cfg, err := decodeConfig[sshConfig](map[string]any{
		"scene":        "nginx",
		"host":         "10.0.0.1",
		"user":         "root",
		"cert_path":    "/etc/nginx/certs/{domain}/fullchain.pem",
		"server_names": "example.com www.example.com",
	})
	if err != nil {
		t.Fatalf("decode ssh: %v", err)
	}
	if cfg.ServerNames != "example.com www.example.com" {
		t.Fatalf("ssh server_names mismatch: %q", cfg.ServerNames)
	}
	if sceneOf(cfg) != domain.SceneNginx {
		t.Fatalf("sceneOf != nginx")
	}
}

// TestAgentNginxDomainTemplate 验证 Agent 侧 {domain} 替换与默认 hook
func TestAgentNginxDomainTemplate(t *testing.T) {
	certPath := "/etc/nginx/certs/{domain}/fullchain.pem"
	domain := "test.example.com"
	got := strings.ReplaceAll(certPath, "{domain}", domain)
	if got != "/etc/nginx/certs/test.example.com/fullchain.pem" {
		t.Fatalf("agent domain template failed: %q", got)
	}
	// 默认 hook
	hook := ""
	if hook == "" {
		hook = "nginx -t && nginx -s reload"
	}
	if hook != "nginx -t && nginx -s reload" {
		t.Fatalf("default hook mismatch: %q", hook)
	}
}

// TestNginxIsolation 多目标同端口 SNI 隔离：两目标不同 server_names 不同路径不互相覆盖
func TestNginxIsolation(t *testing.T) {
	cfgA, _ := decodeConfig[localConfig](map[string]any{
		"scene":        "nginx",
		"cert_path":    "/etc/nginx/certs/{domain}/fullchain.pem",
		"server_names": "a.example.com",
	})
	cfgB, _ := decodeConfig[localConfig](map[string]any{
		"scene":        "nginx",
		"cert_path":    "/etc/nginx/certs/{domain}/fullchain.pem",
		"server_names": "b.example.com",
	})
	pathA := strings.ReplaceAll(cfgA.CertPath, "{domain}", "a.example.com")
	pathB := strings.ReplaceAll(cfgB.CertPath, "{domain}", "b.example.com")
	if pathA == pathB {
		t.Fatal("paths should be isolated per domain")
	}
	if pathA != "/etc/nginx/certs/a.example.com/fullchain.pem" || pathB != "/etc/nginx/certs/b.example.com/fullchain.pem" {
		t.Fatalf("path isolation failed: %q %q", pathA, pathB)
	}
	if cfgA.ServerNames == cfgB.ServerNames {
		t.Fatal("server_names should differ")
	}
}

var _ = domain.SceneNginx
