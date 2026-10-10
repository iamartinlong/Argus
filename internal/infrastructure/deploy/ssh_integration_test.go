//go:build integration

package deploy

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func dockerAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("SKIP_DOCKER") != "" {
		return false
	}
	out, err := exec.Command("docker", "info").CombinedOutput()
	if err != nil {
		t.Logf("docker 不可用，跳过：%s", strings.TrimSpace(string(out)))
		return false
	}
	return true
}

func runDocker(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v 失败: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// genEd25519Key 生成测试 SSH 密钥对
func genEd25519Key(t *testing.T) (publicKey string, privateKeyPEM string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub := ssh.MarshalAuthorizedKey(sshKey.PublicKey())
	privPEM, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(pub)), string(pem.EncodeToMemory(privPEM))
}

// genTestCert 生成自签证书 + 私钥
func genTestCert(t *testing.T, cn string) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// TestSSHDeployToNginxContainer 全链路：SSH 推送证书 → Nginx 容器加载 → TLS 验证
func TestSSHDeployToNginxContainer(t *testing.T) {
	if !dockerAvailable(t) {
		t.Skip("docker 不可用")
	}

	volume := "argus-certs-" + randSuffix()
	sshdName := "argus-sshd-" + randSuffix()
	nginxName := "argus-nginx-" + randSuffix()
	pubKey, privKey := genEd25519Key(t)
	certPEM, keyPEM := genTestCert(t, "example.com")

	// 1. 共享卷
	runDocker(t, "volume", "create", volume)
	t.Cleanup(func() { runDocker(t, "volume", "rm", "-f", volume) })

	// 2. nginx 配置（临时文件）
	tmpDir := t.TempDir()
	nginxConf := filepath.Join(tmpDir, "default.conf")
	conf := `server {
    listen 443 ssl;
    server_name example.com;
    ssl_certificate /etc/nginx/certs/cert.pem;
    ssl_certificate_key /etc/nginx/certs/key.pem;
    location / { return 200 'deployed'; }
}`
	if err := os.WriteFile(nginxConf, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	// panubo/sshd 授权密钥文件
	authKeysFile := filepath.Join(tmpDir, "authorized_keys")
	if err := os.WriteFile(authKeysFile, []byte(pubKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 3. sshd 容器（公钥认证，卷挂 /data/certs）
	runDocker(t, "run", "-d", "--name", sshdName, "-p", "2222:22",
		"-e", "SSH_ENABLE_ROOT=true",
		"-v", authKeysFile+":/etc/authorized_keys/root",
		"-v", volume+":/data/certs", "panubo/sshd")
	t.Cleanup(func() { runDocker(t, "rm", "-f", sshdName) })

	// 4. nginx 容器（共享卷，443 端口映射）
	// 先用有效证书初始化卷（nginx 启动时证书文件必须存在且为有效 PEM）
	initCert := filepath.Join(tmpDir, "init-cert.pem")
	initKey := filepath.Join(tmpDir, "init-key.pem")
	if err := os.WriteFile(initCert, []byte(certPEM), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(initKey, []byte(keyPEM), 0o644); err != nil {
		t.Fatal(err)
	}
	runDocker(t, "run", "--rm", "-v", tmpDir+":/init", "-v", volume+":/etc/nginx/certs",
		"nginx:alpine", "sh", "-c", "cp /init/init-cert.pem /etc/nginx/certs/cert.pem && cp /init/init-key.pem /etc/nginx/certs/key.pem")
	runDocker(t, "run", "-d", "--name", nginxName, "-p", "4443:443",
		"-v", volume+":/etc/nginx/certs", "-v", nginxConf+":/etc/nginx/conf.d/default.conf",
		"nginx:alpine")
	t.Cleanup(func() { runDocker(t, "rm", "-f", nginxName) })

	// 等待 sshd 就绪（轮询 SSH 端口）
	waitPort(t, "127.0.0.1:2222", 30*time.Second)
	time.Sleep(3 * time.Second)

	// 5. 通过 SSH 适配器部署证书
	sshAdapter := NewSSH(20 * time.Second)
	config := map[string]any{
		"host":        "127.0.0.1",
		"port":        2222,
		"user":        "root",
		"private_key": privKey,
		"cert_path":   "/data/certs/cert.pem",
		"key_path":    "/data/certs/key.pem",
		"hook":        "echo deployed",
	}
	result, err := sshAdapter.Deploy(context.Background(), Payload{
		PrimaryDomain:  "example.com",
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
	}, config)
	if err != nil {
		t.Fatalf("SSH 部署: %v", err)
	}
	if result.Checksum == "" {
		t.Fatal("缺少校验和")
	}

	// 5.1 known_hosts 严格校验：错误指纹应拒绝连接（防中间人）
	badConfig := map[string]any{
		"host": "127.0.0.1", "port": 2222, "user": "root",
		"private_key": privKey, "known_hosts": "127.0.0.1 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIWRvbm9sbHl3cm9uZ2tleWZvcnRlc3Q=",
	}
	if err := sshAdapter.Test(context.Background(), badConfig); err == nil {
		t.Fatal("known_hosts 错误指纹应拒绝连接")
	}

	// 5.2 正确指纹应通过：known_hosts 需含服务器实际协商的主机公钥
	//（ssh 客户端可能协商 ecdsa/rsa/ed25519 任一，等同 ssh-keyscan 抓取全部）
	hostKeys := strings.TrimSpace(runDocker(t, "exec", sshdName, "sh", "-c",
		"cat /etc/ssh/keys/ssh_host_ecdsa_key.pub /etc/ssh/keys/ssh_host_ed25519_key.pub /etc/ssh/keys/ssh_host_rsa_key.pub"))
	var khEntries []string
	for _, line := range strings.Split(hostKeys, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			khEntries = append(khEntries, "[127.0.0.1]:2222 "+line)
		}
	}
	goodConfig := map[string]any{
		"host": "127.0.0.1", "port": 2222, "user": "root",
		"private_key": privKey, "known_hosts": strings.Join(khEntries, "\n"),
	}
	if err := sshAdapter.Test(context.Background(), goodConfig); err != nil {
		t.Fatalf("known_hosts 正确指纹应通过: %v", err)
	}

	// 6. 验证文件已推送（sshd 容器内）
	fileCheck := runDocker(t, "exec", sshdName, "ls", "-l", "/data/certs/")
	if !strings.Contains(fileCheck, "cert.pem") || !strings.Contains(fileCheck, "key.pem") {
		t.Fatalf("文件未推送：%s", fileCheck)
	}

	// 7. 触发 nginx reload（生产场景 hook 为 reload 命令）
	runDocker(t, "exec", nginxName, "nginx", "-s", "reload")

	// 8. TLS 验证：连接 4443，检查证书 CN
	waitPort(t, "127.0.0.1:4443", 30*time.Second)
	conn, err := tls.Dial("tcp", "127.0.0.1:4443", &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("TLS 连接失败（证书未生效？）: %v", err)
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		t.Fatal("无对端证书")
	}
	if certs[0].Subject.CommonName != "example.com" {
		t.Fatalf("证书 CN 不符：%s", certs[0].Subject.CommonName)
	}
	if certs[0].NotAfter.Before(time.Now()) {
		t.Fatal("证书已过期")
	}
	t.Logf("部署验证通过：nginx 已加载 CN=%s 的新证书", certs[0].Subject.CommonName)
}

// TestSSHConnectionFailure 错误路径
func TestSSHConnectionFailure(t *testing.T) {
	if !dockerAvailable(t) {
		t.Skip("docker 不可用")
	}
	adapter := NewSSH(3 * time.Second)
	err := adapter.Test(context.Background(), map[string]any{
		"host": "127.0.0.1", "port": 9, "user": "root",
		"private_key": "invalid-key",
	})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// TestIISOnLinuxRejected iis 目标指向 Linux 主机：Test 应报告非 PowerShell 环境
func TestIISOnLinuxRejected(t *testing.T) {
	if !dockerAvailable(t) {
		t.Skip("docker 不可用")
	}
	volume := "argus-iis-" + randSuffix()
	sshdName := "argus-iis-sshd-" + randSuffix()
	pubKey, privKey := genEd25519Key(t)
	tmpDir := t.TempDir()
	authKeysFile := filepath.Join(tmpDir, "authorized_keys")
	if err := os.WriteFile(authKeysFile, []byte(pubKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDocker(t, "volume", "create", volume)
	t.Cleanup(func() { runDocker(t, "volume", "rm", "-f", volume) })
	runDocker(t, "run", "-d", "--name", sshdName, "-p", "2222:22",
		"-e", "SSH_ENABLE_ROOT=true",
		"-v", authKeysFile+":/etc/authorized_keys/root",
		"-v", volume+":/data", "panubo/sshd")
	t.Cleanup(func() { runDocker(t, "rm", "-f", sshdName) })
	waitPort(t, "127.0.0.1:2222", 30*time.Second)
	time.Sleep(3 * time.Second)

	// Linux 容器无 PowerShell → ssh+IIS 场景 Test 应报 PowerShell 探测失败
	sshAdapter := NewSSH(10 * time.Second)
	err := sshAdapter.Test(context.Background(), map[string]any{
		"host": "127.0.0.1", "port": 2222, "user": "root",
		"private_key": privKey, "scene": "iis", "pfx_path": "C:/x.pfx", "bindings": []int{443},
	})
	if err == nil {
		t.Fatal("expected PowerShell probe failure on Linux target")
	}
	if !strings.Contains(err.Error(), "PowerShell") {
		t.Fatalf("expected PowerShell error, got: %v", err)
	}
	t.Logf("iis Test correctly rejected Linux target: %v", err)
}

func waitPort(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("端口 %s 未在 %v 内就绪", addr, timeout)
}

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
