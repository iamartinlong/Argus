package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"
)

// RemoteConfig 远程模式配置
type RemoteConfig struct {
	BaseURL string // 如 https://cert.example.com
	Token   string // API Token（st_xxx）
}

// RemoteOps CertOps 的远程实现：经既有 REST API 代理。
// 服务端强制 token 的 scope 与角色；本端不做额外信任假设。
type RemoteOps struct {
	cfg     RemoteConfig
	http    *http.Client
	longHTTP *http.Client
}

func NewRemoteOps(cfg RemoteConfig) *RemoteOps {
	return &RemoteOps{
		cfg:      cfg,
		http:     &http.Client{Timeout: 30 * time.Second},
		longHTTP: &http.Client{Timeout: 10 * time.Minute}, // 签发/部署可达数分钟
	}
}

// do 发送请求并解包信封（{code:data} / 错误 {code,message}）。
func (o *RemoteOps) do(method, path string, body any, out any) error {
	client := o.http
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete {
		client = o.longHTTP
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, strings.TrimRight(o.cfg.BaseURL, "/")+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Token", o.cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Message
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return remoteError(resp.StatusCode, e.Code, msg)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("解析响应数据失败: %w", err)
		}
	}
	return nil
}

// remoteError 把 HTTP 状态/错误码映射为 domain 错误（与本地语义一致）。
func remoteError(status int, code, msg string) error {
	switch {
	case status == http.StatusNotFound:
		return fmt.Errorf("%w: %s", domain.ErrNotFound, msg)
	case status == http.StatusConflict:
		return fmt.Errorf("%w: %s", domain.ErrConflict, msg)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", domain.ErrUnauthorized, msg)
	case status == http.StatusForbidden:
		return fmt.Errorf("%w: %s（token 权限不足）", domain.ErrForbidden, msg)
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", domain.ErrLocked, msg)
	case status == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", domain.ErrInvalidArgument, msg)
	default:
		return fmt.Errorf("服务端错误 %d: %s", status, msg)
	}
}

func (o *RemoteOps) ListCertificates(ctx context.Context, f ListFilter) ([]*domain.Certificate, error) {
	var certs []*domain.Certificate
	if err := o.do(http.MethodGet, "/api/v1/certificates", nil, &certs); err != nil {
		return nil, err
	}
	// REST 列表不支持服务端过滤：客户端过滤（与本地实现同规则）
	now := time.Now()
	out := make([]*domain.Certificate, 0, len(certs))
	for _, c := range certs {
		c.UpdateStatus(now)
		if f.Status != "" && string(c.Status) != f.Status {
			continue
		}
		if f.ExpiringWithinDays > 0 {
			if c.NotAfter.IsZero() || c.NotAfter.After(now.Add(time.Duration(f.ExpiringWithinDays)*24*time.Hour)) {
				continue
			}
		}
		if f.Keyword != "" && !certMatchesKeyword(c, f.Keyword) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (o *RemoteOps) GetCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error) {
	if id != "" {
		var c domain.Certificate
		if err := o.do(http.MethodGet, "/api/v1/certificates/"+id, nil, &c); err != nil {
			return nil, err
		}
		return &c, nil
	}
	certs, err := o.ListCertificates(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	name := normalizeDomainName(domainName)
	var match *domain.Certificate
	for _, c := range certs {
		if normalizeDomainName(c.PrimaryDomain) == name || certMatchesKeyword(c, name) {
			if match == nil || c.NotAfter.After(match.NotAfter) {
				match = c
			}
		}
	}
	if match == nil {
		return nil, fmt.Errorf("%w: 未找到域名 %s 的证书", domain.ErrNotFound, domainName)
	}
	return match, nil
}

func (o *RemoteOps) IssueCertificate(ctx context.Context, in IssueParams) (*domain.Certificate, error) {
	var c domain.Certificate
	err := o.do(http.MethodPost, "/api/v1/certificates", map[string]any{
		"domains":                 in.Domains,
		"account_id":              in.AccountID,
		"verification":            in.Verification,
		"dns_provider_account_id": in.DNSProviderAccountID,
		"deploy_target_ids":       in.DeployTargetIDs,
		"notify_events":           in.NotifyEvents,
	}, &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (o *RemoteOps) RenewCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error) {
	cert, err := o.GetCertificate(ctx, id, domainName)
	if err != nil {
		return nil, err
	}
	var c domain.Certificate
	if err := o.do(http.MethodPost, "/api/v1/certificates/"+cert.ID+"/renew", map[string]any{
		"dns_provider_account_id": cert.DNSProviderAccountID,
	}, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (o *RemoteOps) RevokeCertificate(ctx context.Context, id, domainName string) error {
	cert, err := o.GetCertificate(ctx, id, domainName)
	if err != nil {
		return err
	}
	return o.do(http.MethodPost, "/api/v1/certificates/"+cert.ID+"/revoke", nil, nil)
}

func (o *RemoteOps) ImportCertificate(ctx context.Context, in ImportParams) (*domain.Certificate, error) {
	var c domain.Certificate
	err := o.do(http.MethodPost, "/api/v1/certificates/import", map[string]any{
		"domains":       in.Domains,
		"certificate":   in.Certificate,
		"private_key":   in.PrivateKey,
		"verification":  "dns01",
		"notify_events": []string{},
	}, &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (o *RemoteOps) ListTargets(ctx context.Context) ([]*domain.DeployTarget, error) {
	var targets []*domain.DeployTarget
	if err := o.do(http.MethodGet, "/api/v1/deploy-targets", nil, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func (o *RemoteOps) DeployCertificate(ctx context.Context, id, domainName string, targetIDs []string) error {
	cert, err := o.GetCertificate(ctx, id, domainName)
	if err != nil {
		return err
	}
	// 未指定目标时由服务端部署证书绑定的全部目标
	return o.do(http.MethodPost, "/api/v1/certificates/"+cert.ID+"/deploy", map[string]any{
		"target_ids": targetIDs,
	}, nil)
}

func (o *RemoteOps) TestTarget(ctx context.Context, id string) error {
	return o.do(http.MethodPost, "/api/v1/deploy-targets/"+id+"/test", nil, nil)
}

func (o *RemoteOps) ListAccounts(ctx context.Context, typ string) ([]AccountBrief, error) {
	if typ != "" && typ != "acme" && typ != "dns" {
		return nil, fmt.Errorf("%w: type 仅支持 acme / dns", domain.ErrInvalidArgument)
	}
	var out []AccountBrief
	if typ == "" || typ == "acme" {
		var accounts []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Status   string `json:"status"`
		}
		if err := o.do(http.MethodGet, "/api/v1/acme-accounts", nil, &accounts); err != nil {
			return nil, err
		}
		for _, a := range accounts {
			out = append(out, AccountBrief{ID: a.ID, Name: a.Name, Provider: a.Provider, Status: a.Status})
		}
	}
	if typ == "" || typ == "dns" {
		var accounts []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Status   string `json:"status"`
		}
		if err := o.do(http.MethodGet, "/api/v1/dns-accounts", nil, &accounts); err != nil {
			return nil, err
		}
		for _, a := range accounts {
			out = append(out, AccountBrief{ID: a.ID, Name: a.Name, Provider: a.Provider, Status: a.Status})
		}
	}
	return out, nil
}

func (o *RemoteOps) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	var agents []*domain.Agent
	if err := o.do(http.MethodGet, "/api/v1/agents", nil, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

func (o *RemoteOps) TaskLogs(ctx context.Context, limit int) ([]*domain.TaskLog, error) {
	if limit <= 0 {
		limit = 20
	}
	var logs []*domain.TaskLog
	if err := o.do(http.MethodGet, fmt.Sprintf("/api/v1/task-logs?limit=%d", limit), nil, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// DownloadCertificate 远程模式不提供私钥下载：远程无法确认服务端
// mcp.allow_key_download 开关状态，按最小暴露原则不注册该工具（见实施计划 §4.5）。
func (o *RemoteOps) DownloadCertificate(ctx context.Context, id, format string) ([]byte, string, error) {
	return nil, "", fmt.Errorf("%w: 远程模式不支持私钥下载，请在服务端本机使用", domain.ErrForbidden)
}
