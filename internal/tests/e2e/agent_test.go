//go:build integration

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
	"github.com/iamartinlong/Argus/internal/testutil"
)

// 信封：{code, data}
type envelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

// doRequest 发送 HTTP 请求并解析信封；body 为 nil 表示无请求体。
func doRequest(t *testing.T, method, url, token string, body any, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("解析信封失败: %v body=%s", err, raw)
	}
	if env.Code != 0 {
		t.Fatalf("请求 %s %s 失败 code=%d body=%s", method, url, env.Code, raw)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			t.Fatalf("解析 data 失败: %v data=%s", err, env.Data)
		}
	}
}

type loginData struct {
	AccessToken string `json:"access_token"`
}

type agentCreateData struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type targetData struct {
	ID string `json:"id"`
}

type certData struct {
	ID string `json:"id"`
}

type recordData struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Checksum string `json:"checksum"`
	Message  string `json:"message"`
}

// buildAgent 交叉编译 Linux 版 argus-agent 二进制到临时目录，返回路径。
func buildAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "argus-agent")
	cmd := exec.Command("go", "build", "-o", out, "../../../cmd/argus-agent")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构建 agent 失败: %v\n%s", err, b)
	}
	return out
}

// runAgentContainer 启动跑 argus-agent 的容器（agent 写文件到共享卷 mountPath）。
func runAgentContainer(t *testing.T, agentBin, agentJSON, volume, mountPath string) string {
	t.Helper()
	name := "argus-agent-e2e-" + testutil.RandSuffix()
	testutil.RunDocker(t, "run", "-d", "--name", name,
		"--add-host", "host.docker.internal:host-gateway",
		"-v", agentBin+":/usr/local/bin/argus-agent:ro",
		"-v", agentJSON+":/etc/agent.json:ro",
		"-v", volume+":"+mountPath,
		"alpine:3.20", "sh", "-c", "argus-agent --config /etc/agent.json")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	return name
}

// waitDeploySuccess 轮询部署记录直到 success/failed 或超时。
func waitDeploySuccess(t *testing.T, baseURL, token, certID string, timeout time.Duration) recordData {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last recordData
	for time.Now().Before(deadline) {
		var recs []recordData
		doRequest(t, "GET", baseURL+"/api/v1/certificates/"+certID+"/deploy-records", token, nil, &recs)
		if len(recs) > 0 {
			last = recs[0]
			if last.Status == string(domain.DeploySuccess) || last.Status == string(domain.DeployFailed) {
				return last
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("部署未在 %v 内完成, last=%+v", timeout, last)
	return last
}

// setupCommon 公共流程：建 admin 用户 + 登录 + 建 agent + 建目标 + 导入证书。
func setupCommon(t *testing.T, s *testutil.LiveTestServer, targetName string, config map[string]any, certCN string) (token, agentToken, certID string) {
	t.Helper()
	s.SeedUser(t, "admin", "Admin@2026Argus!", domain.RoleAdmin)

	var login loginData
	doRequest(t, "POST", s.BaseURL+"/api/v1/auth/login", "", map[string]any{
		"username": "admin", "password": "Admin@2026Argus!",
	}, &login)
	token = login.AccessToken

	var ag agentCreateData
	doRequest(t, "POST", s.BaseURL+"/api/v1/agents", token, map[string]any{"name": "e2e-agent"}, &ag)
	agentToken = ag.Token

	cfg := map[string]any{"agent_id": ag.ID}
	for k, v := range config {
		cfg[k] = v
	}
	var target targetData
	doRequest(t, "POST", s.BaseURL+"/api/v1/deploy-targets", token, map[string]any{
		"name": targetName, "type": "agent", "config": cfg,
	}, &target)

	certPEM, keyPEM, err := testutil.GenTestCert(certCN)
	if err != nil {
		t.Fatal(err)
	}
	var cert certData
	doRequest(t, "POST", s.BaseURL+"/api/v1/certificates/import", token, map[string]any{
		"domains":           []string{certCN},
		"certificate":       certPEM,
		"private_key":       keyPEM,
		"verification":      "http01",
		"deploy_target_ids": []string{target.ID},
	}, &cert)

	return token, agentToken, cert.ID
}

// TestAgentDeployGenericContainer agent 通道 + 通用场景：
// 容器内 agent 出站连宿主机中心端，拉取任务写证书/私钥到共享卷。
func TestAgentDeployGenericContainer(t *testing.T) {
	if !testutil.DockerAvailable(t) {
		t.Skip("docker 不可用")
	}
	s := testutil.NewLiveTestServer(t)
	agentBin := buildAgent(t)
	volume := "argus-e2e-gen-" + testutil.RandSuffix()
	testutil.RunDocker(t, "volume", "create", volume)
	t.Cleanup(func() { testutil.RunDocker(t, "volume", "rm", "-f", volume) })

	token, agentToken, certID := setupCommon(t, s, "e2e-generic", map[string]any{
		"cert_path": "/data/cert.pem",
		"key_path":  "/data/key.pem",
	}, "generic.example.com")

	// 写 agent.json（容器经 host.docker.internal 出站连宿主机中心端）
	agentDir := t.TempDir()
	agentJSON := filepath.Join(agentDir, "agent.json")
	cfgJSON := fmt.Sprintf(`{"server_url":"http://host.docker.internal:%d","token":%q,"poll_interval":2}`, s.Port, agentToken)
	if err := os.WriteFile(agentJSON, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	name := runAgentContainer(t, agentBin, agentJSON, volume, "/data")

	// 触发部署
	doRequest(t, "POST", s.BaseURL+"/api/v1/certificates/"+certID+"/deploy", token, map[string]any{}, nil)

	// 轮询部署记录 success
	rec := waitDeploySuccess(t, s.BaseURL, token, certID, 90*time.Second)
	if rec.Status != string(domain.DeploySuccess) {
		t.Fatalf("部署失败: %+v", rec)
	}
	if rec.Checksum == "" {
		t.Fatal("部署成功但校验和为空")
	}

	// 验证共享卷内文件已由容器内 agent 写入
	certOut := testutil.RunDocker(t, "exec", name, "sh", "-c", "cat /data/cert.pem")
	if !strings.Contains(certOut, "BEGIN CERTIFICATE") {
		t.Fatalf("证书未写入: %s", certOut)
	}
	keyOut := testutil.RunDocker(t, "exec", name, "sh", "-c", "cat /data/key.pem")
	if !strings.Contains(keyOut, "PRIVATE KEY") {
		t.Fatalf("私钥未写入: %s", keyOut)
	}
	t.Logf("agent 通用场景部署成功: %+v", rec)
}

// TestAgentDeployNginxContainer agent 通道 + nginx 场景：
// 容器内 nginx + agent，agent 拉取任务写证书到 /etc/nginx/certs 并触发 reload。
func TestAgentDeployNginxContainer(t *testing.T) {
	if !testutil.DockerAvailable(t) {
		t.Skip("docker 不可用")
	}
	s := testutil.NewLiveTestServer(t)
	agentBin := buildAgent(t)
	volume := "argus-e2e-nginx-" + testutil.RandSuffix()
	testutil.RunDocker(t, "volume", "create", volume)
	t.Cleanup(func() { testutil.RunDocker(t, "volume", "rm", "-f", volume) })

	token, agentToken, certID := setupCommon(t, s, "e2e-nginx", map[string]any{
		"cert_path": "/etc/nginx/certs/cert.pem",
		"key_path":  "/etc/nginx/certs/key.pem",
		"hook":      "nginx -s reload",
		"is_windows": false,
	}, "nginx.example.com")

	agentDir := t.TempDir()
	agentJSON := filepath.Join(agentDir, "agent.json")
	cfgJSON := fmt.Sprintf(`{"server_url":"http://host.docker.internal:%d","token":%q,"poll_interval":2}`, s.Port, agentToken)
	if err := os.WriteFile(agentJSON, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// nginx:alpine 容器：先启动 nginx，再运行 agent（agent 写文件 + reload）
	name := "argus-agent-e2e-nginx-" + testutil.RandSuffix()
	testutil.RunDocker(t, "run", "-d", "--name", name,
		"--add-host", "host.docker.internal:host-gateway",
		"-v", agentBin+":/usr/local/bin/argus-agent:ro",
		"-v", agentJSON+":/etc/agent.json:ro",
		"-v", volume+":/etc/nginx/certs",
		"nginx:alpine", "sh", "-c", "nginx; argus-agent --config /etc/agent.json")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	doRequest(t, "POST", s.BaseURL+"/api/v1/certificates/"+certID+"/deploy", token, map[string]any{}, nil)

	rec := waitDeploySuccess(t, s.BaseURL, token, certID, 90*time.Second)
	if rec.Status != string(domain.DeploySuccess) {
		t.Fatalf("部署失败: %+v", rec)
	}
	if rec.Checksum == "" {
		t.Fatal("部署成功但校验和为空")
	}

	// 验证 nginx 证书目录文件已写入
	certOut := testutil.RunDocker(t, "exec", name, "sh", "-c", "cat /etc/nginx/certs/cert.pem")
	if !strings.Contains(certOut, "BEGIN CERTIFICATE") {
		t.Fatalf("nginx 证书未写入: %s", certOut)
	}
	keyOut := testutil.RunDocker(t, "exec", name, "sh", "-c", "cat /etc/nginx/certs/key.pem")
	if !strings.Contains(keyOut, "PRIVATE KEY") {
		t.Fatalf("nginx 私钥未写入: %s", keyOut)
	}
	t.Logf("agent nginx 场景部署成功: %+v", rec)
}
