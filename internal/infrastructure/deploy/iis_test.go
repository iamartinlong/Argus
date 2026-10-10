package deploy

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSSHIISValidation SSH 通道 IIS 场景的配置校验（必填在连接前拦截）
func TestSSHIISValidation(t *testing.T) {
	ssh := NewSSH(5 * time.Second)
	ctx := context.Background()
	payload := Payload{PrimaryDomain: "iis.example.com", CertificatePEM: "C", PrivateKeyPEM: "K"}

	cases := []struct {
		name   string
		config map[string]any
	}{
		{"missing host", map[string]any{"scene": "iis", "user": "admin", "pfx_path": "C:/c.pfx", "bindings": []int{443}}},
		{"missing user", map[string]any{"scene": "iis", "host": "10.0.0.1", "pfx_path": "C:/c.pfx", "bindings": []int{443}}},
		{"missing pfx_path", map[string]any{"scene": "iis", "host": "10.0.0.1", "user": "admin", "bindings": []int{443}}},
		{"missing bindings", map[string]any{"scene": "iis", "host": "10.0.0.1", "user": "admin", "pfx_path": "C:/c.pfx"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ssh.Deploy(ctx, payload, c.config); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// TestSSHTomcatValidation SSH 通道 Tomcat 场景：缺 pfx_path 应在连接前拦截
func TestSSHTomcatValidation(t *testing.T) {
	ssh := NewSSH(5 * time.Second)
	ctx := context.Background()
	payload := Payload{PrimaryDomain: "tom.example.com", CertificatePEM: "C", PrivateKeyPEM: "K"}
	if _, err := ssh.Deploy(ctx, payload, map[string]any{
		"scene": "tomcat", "host": "10.0.0.1", "user": "admin",
	}); err == nil {
		t.Fatal("expected pfx_path validation error")
	}
}

func TestRenderIISScriptV2(t *testing.T) {
	script := RenderIISScriptV2(`C:\certs\x.pfx`, "pw123", []int{443, 8443}, "Default Web Site")
	checks := []string{
		"Import-PfxCertificate",
		"netsh http show sslcert",
		"netsh http delete sslcert",
		"netsh http add sslcert",
		"Import-Module WebAdministration", // 站点绑定部分
		"Default Web Site",
		"443, 8443",
		"Remove-Item $pfxPath",
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Fatalf("script missing %q\n%s", c, script)
		}
	}
	if strings.Contains(script, "{site_name}") || strings.Contains(script, "{pfx_path}") {
		t.Fatal("unreplaced placeholder")
	}
	// SNI 场景：显式 hostname 时应含 HostHeader+SNI 与 hostnameport
	scriptSNI := RenderIISScriptV2WithHostname(`C:\certs\x.pfx`, "pw123", []int{443}, "SiteA", "a.example.com")
	if !strings.Contains(scriptSNI, "a.example.com") || !strings.Contains(scriptSNI, "SslFlags 1") || !strings.Contains(scriptSNI, "hostnameport") {
		t.Fatalf("SNI script missing hostname/SNI\n%s", scriptSNI)
	}
}

func TestRenderIISScriptV2WithoutSite(t *testing.T) {
	script := RenderIISScriptV2("C:/x.pfx", "pw", []int{443}, "")
	if strings.Contains(script, "WebAdministration") {
		t.Fatal("site binding section should be omitted without site_name")
	}
}

func TestIISScriptV2SiteSectionRendered(t *testing.T) {
	section := strings.ReplaceAll(IISSiteBindingSection, "{site_name}", "MySite")
	section = strings.ReplaceAll(section, "{iis_bindings}", "443")
	if !strings.Contains(section, "New-WebBinding -Name 'MySite'") {
		t.Fatalf("unexpected site section: %s", section)
	}
	if !strings.Contains(section, "AddSslCertificate") {
		t.Fatal("expected AddSslCertificate")
	}
}

func TestRenderIISAgentScriptV2(t *testing.T) {
	script := RenderIISAgentScriptV2(`C:\certs\x.pfx`, "pw123", []int{443}, "Default Web Site", "todo.example.com")
	checks := []string{
		"Import-PfxCertificate",
		"netsh http add sslcert",
		"appcmd.exe",
		"Default Web Site",
		"todo.example.com",
		"443",
		"Remove-Item $pfxPath",
		"hostnameport",
		"sslFlags='1'",
	}
	for _, c := range checks {
		if !strings.Contains(script, c) {
			t.Fatalf("script missing %q\n%s", c, script)
		}
	}
	// appcmd 站点绑定语法（SNI：hostname 存于 $h，${port} 防止作用域解析）
	if !strings.Contains(script, "$h = 'todo.example.com'") {
		t.Fatalf("appcmd hostname variable missing\n%s", script)
	}
	if !strings.Contains(script, "bindingInformation='*:${port}:$h',sslFlags='1']") {
		t.Fatalf("appcmd SNI binding missing\n%s", script)
	}
	// 幂等：绑定前先查重，已存在则删除重建（避免重复添加报错）
	if !strings.Contains(script, "appcmd list binding") {
		t.Fatalf("appcmd binding dedup check missing\n%s", script)
	}
	if !strings.Contains(script, "/-bindings.[protocol='https',bindingInformation='*:${port}:$h',sslFlags='1']") {
		t.Fatalf("appcmd binding delete-before-add missing SNI\n%s", script)
	}
	// 不应依赖 WebAdministration 模块
	if strings.Contains(script, "WebAdministration") {
		t.Fatal("agent script should not require WebAdministration")
	}
	if strings.Contains(script, "{site_name}") || strings.Contains(script, "{pfx_path}") {
		t.Fatal("unreplaced placeholder")
	}
	// 多站点 SNI 回归：不同 hostname 的脚本应互不覆盖 ipport
	script2 := RenderIISAgentScriptV2(`C:\certs\x.pfx`, "pw123", []int{443}, "SiteB", "b.example.com")
	if !strings.Contains(script2, "b.example.com") || strings.Contains(script2, "todo.example.com") {
		t.Fatalf("second site hostname not isolated\n%s", script2)
	}
}

func TestRenderIISAgentScriptV2WithoutSite(t *testing.T) {
	script := RenderIISAgentScriptV2("C:/x.pfx", "pw", []int{443}, "", "todo.example.com")
	if strings.Contains(script, "appcmd") {
		t.Fatal("appcmd site binding should be omitted without site_name")
	}
	if !strings.Contains(script, "netsh http add sslcert") {
		t.Fatal("netsh port binding should always be present")
	}
}

func TestIISAgentSiteSectionRendered(t *testing.T) {
	section := strings.ReplaceAll(IISSiteBindingSectionAppCmd, "{site_name}", "MySite")
	section = strings.ReplaceAll(section, "{iis_bindings}", "443, 8443")
	section = strings.ReplaceAll(section, "{hostname}", "todo.example.com")
	if !strings.Contains(section, "appcmd.exe") || !strings.Contains(section, "MySite") {
		t.Fatalf("unexpected site section: %s", section)
	}
	if !strings.Contains(section, "443, 8443") || !strings.Contains(section, "todo.example.com") {
		t.Fatalf("binding info missing: %s", section)
	}
}
