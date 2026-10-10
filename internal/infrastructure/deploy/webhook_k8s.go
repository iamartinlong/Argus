package deploy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Webhook 通用 Webhook 目标：POST 证书信息
type Webhook struct {
	client *http.Client
}

func NewWebhook() *Webhook {
	return &Webhook{client: &http.Client{Timeout: 15 * time.Second}}
}

func (Webhook) Type() string { return "webhook" }

func (Webhook) IsAsync() bool { return false }

type webhookConfig struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

func (w *Webhook) Deploy(_ context.Context, payload Payload, config map[string]any) (*Result, error) {
	cfg, err := decodeConfig[webhookConfig](config)
	if err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("webhook 目标缺少 url")
	}
	// 私钥明文出站：强制 HTTPS（防中间人窃取私钥）
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("webhook url 非法: %s", cfg.URL)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("webhook 目标包含私钥明文，必须使用 https://（当前 %s）", cfg.URL)
	}
	body := map[string]any{
		"event":           "certificate.deployed",
		"domain":          payload.PrimaryDomain,
		"domains":         payload.Domains,
		"certificate_pem": payload.CertificatePEM,
		"private_key_pem": payload.PrivateKeyPEM,
		"time":            time.Now().Format(time.RFC3339),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write(data)
		req.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("webhook 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return &Result{Checksum: ChecksumOf(payload), Message: fmt.Sprintf("webhook 已通知 %s", cfg.URL)}, nil
}

func (w *Webhook) Test(_ context.Context, config map[string]any) error {
	cfg, err := decodeConfig[webhookConfig](config)
	if err != nil {
		return err
	}
	if cfg.URL == "" {
		return fmt.Errorf("url 不能为空")
	}
	return nil
}

// K8s Kubernetes Secret 目标：调用 kubectl 应用 Secret（M5 可切换 client-go）
type K8s struct{}

func (K8s) Type() string { return "k8s" }

func (K8s) IsAsync() bool { return false }

type k8sConfig struct {
	Kubeconfig       string `json:"kubeconfig"`        // 可选，kubectl --kubeconfig
	Namespace        string `json:"namespace"`
	SecretName       string `json:"secret_name"`
	CertKeyInSecret  string `json:"cert_key"`          // Secret 中证书字段名（默认 tls.crt）
	KeyKeyInSecret   string `json:"private_key"`       // Secret 中私钥字段名（默认 tls.key）
	RolloutDeployment string `json:"rollout_deployment"` // 可选，部署后滚动重启
}

// kubectlWithTimeout 执行 kubectl（60s 超时，防挂起阻塞调度）
func kubectlWithTimeout(ctx context.Context, args []string, stdin string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "kubectl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func (K8s) Deploy(ctx context.Context, payload Payload, config map[string]any) (*Result, error) {
	cfg, err := decodeConfig[k8sConfig](config)
	if err != nil {
		return nil, err
	}
	if cfg.SecretName == "" {
		return nil, fmt.Errorf("k8s 目标缺少 secret_name")
	}
	certKey := cfg.CertKeyInSecret
	if certKey == "" {
		certKey = "tls.crt"
	}
	keyKey := cfg.KeyKeyInSecret
	if keyKey == "" {
		keyKey = "tls.key"
	}
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: kubernetes.io/tls
data:
  %s: %s
  %s: %s
`, cfg.SecretName, cfg.Namespace, certKey, base64.StdEncoding.EncodeToString([]byte(payload.CertificatePEM)),
		keyKey, base64.StdEncoding.EncodeToString([]byte(payload.PrivateKeyPEM)))

	args := []string{"apply", "-f", "-"}
	if cfg.Kubeconfig != "" {
		args = append([]string{"--kubeconfig", cfg.Kubeconfig}, args...)
	}
	out, err := kubectlWithTimeout(ctx, args, manifest)
	if err != nil {
		return nil, fmt.Errorf("kubectl apply 失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	msg := fmt.Sprintf("Secret %s/%s 已更新", cfg.Namespace, cfg.SecretName)
	if cfg.RolloutDeployment != "" {
		args = []string{"rollout", "restart", "deployment/" + cfg.RolloutDeployment, "-n", cfg.Namespace}
		if cfg.Kubeconfig != "" {
			args = append([]string{"--kubeconfig", cfg.Kubeconfig}, args...)
		}
		if out, err := kubectlWithTimeout(ctx, args, ""); err != nil {
			return nil, fmt.Errorf("rollout restart 失败: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		msg += "；Deployment 已重启"
	}
	return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
}

func (K8s) Test(_ context.Context, config map[string]any) error {
	cfg, err := decodeConfig[k8sConfig](config)
	if err != nil {
		return err
	}
	if cfg.SecretName == "" {
		return fmt.Errorf("secret_name 不能为空")
	}
	return nil
}
