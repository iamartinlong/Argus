package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iamartinlong/Argus/internal/domain"
)

// 工具所需 scope 常量（与 REST API Token 语义完全一致）
const (
	scopeCertRead   = domain.ScopeCertRead
	scopeCertWrite  = domain.ScopeCertWrite
	scopeDeployRead = domain.ScopeDeployRead
	scopeDeployWrite = domain.ScopeDeployWrite
	scopeAccountRead = domain.ScopeAccountRead
	scopeAdminRead   = domain.ScopeAdminRead
)

// toolSpec 工具声明：定义 + 所需 scope
type toolSpec struct {
	name  string
	scope string
}

// allTools v1 工具集（T13 由 AllowKeyDownload 条件注册）
var allTools = []toolSpec{
	{"list_certificates", scopeCertRead},
	{"get_certificate", scopeCertRead},
	{"list_deploy_targets", scopeDeployRead},
	{"list_accounts", scopeAccountRead},
	{"list_agents", scopeAdminRead},
	{"get_task_logs", scopeCertRead},
	{"issue_certificate", scopeCertWrite},
	{"renew_certificate", scopeCertWrite},
	{"revoke_certificate", scopeCertWrite},
	{"import_certificate", scopeCertWrite},
	{"deploy_certificate", scopeDeployWrite},
	{"test_deploy_target", scopeDeployWrite},
	{"download_certificate", scopeCertWrite},
}

// hasScope 本地模式 scope 收敛：空 Scopes 表示不过滤（全部注册）。
func hasScope(scopes []string, required string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if s == domain.ScopeAll || s == required {
			return true
		}
		if s == domain.ScopeRead && len(required) > 5 && required[len(required)-5:] == ":read" {
			return true
		}
	}
	return false
}

// registerTools 注册工具集：按 cfg.Scopes 收敛；T13 视 AllowKeyDownload。
func (s *Server) registerTools() {
	enabled := func(name string) bool {
		if name == "download_certificate" && !s.cfg.AllowKeyDownload {
			return false // 私钥下载默认不注册（D5）
		}
		return hasScope(s.cfg.Scopes, scopeOf(name))
	}
	for _, spec := range allTools {
		if !enabled(spec.name) {
			continue
		}
		s.addTool(spec.name)
	}
}

func scopeOf(name string) string {
	for _, t := range allTools {
		if t.name == name {
			return t.scope
		}
	}
	return domain.ScopeAdminRead
}

// addTool 注册单个工具（handler 薄封装：参数绑定 → ops 调用 → 结果结构化）。
func (s *Server) addTool(name string) {
	ops := s.ops
	switch name {
	case "list_certificates":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "list_certificates",
			Description: "列出 SSL 证书。可按状态或临期天数过滤，用于巡检与到期预警。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			Status             string `json:"status,omitempty" jsonschema:"状态过滤：valid/expiring/expired/pending/revoked/failed，空为全部"`
			ExpiringWithinDays int    `json:"expiring_within_days,omitempty" jsonschema:"只返回 N 天内到期的证书"`
			Keyword            string `json:"keyword,omitempty" jsonschema:"域名模糊匹配"`
		}) (*mcp.CallToolResult, any, error) {
			certs, err := ops.ListCertificates(ctx, ListFilter{
				Status:             in.Status,
				ExpiringWithinDays: in.ExpiringWithinDays,
				Keyword:            in.Keyword,
			})
			if err != nil {
				return toolError(err)
			}
			return toolOK(certBriefs(certs, time.Now().UTC()))
		})

	case "get_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "get_certificate",
			Description: "查询证书详情（域名/SAN/有效期/渠道/状态）。id 与 domain 二选一。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			ID     string `json:"id,omitempty" jsonschema:"证书 ID"`
			Domain string `json:"domain,omitempty" jsonschema:"域名（与 id 二选一，取到期最晚的匹配证书）"`
		}) (*mcp.CallToolResult, any, error) {
			if in.ID == "" && in.Domain == "" {
				return toolError(fmt.Errorf("%w: id 与 domain 至少提供一个", domain.ErrInvalidArgument))
			}
			cert, err := ops.GetCertificate(ctx, in.ID, in.Domain)
			if err != nil {
				return toolError(err)
			}
			return toolOK(certBrief(cert, time.Now().UTC()))
		})

	case "list_deploy_targets":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "list_deploy_targets",
			Description: "列出部署目标（通道 × 应用场景 × 状态）。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
			targets, err := ops.ListTargets(ctx)
			if err != nil {
				return toolError(err)
			}
			out := make([]map[string]any, 0, len(targets))
			for _, t := range targets {
				out = append(out, map[string]any{
					"id": t.ID, "name": t.Name, "type": t.Type,
					"enabled": t.Enabled, "last_status": t.LastStatus, "last_error": t.LastError,
				})
			}
			return toolOK(out)
		})

	case "list_accounts":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "list_accounts",
			Description: "列出渠道账户（SSL 签发渠道与 DNS 验证账户，凭据已脱敏）。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			Type string `json:"type,omitempty" jsonschema:"账户类型：acme 表示 SSL 渠道，dns 表示 DNS 验证账户，空为全部"`
		}) (*mcp.CallToolResult, any, error) {
			accounts, err := ops.ListAccounts(ctx, in.Type)
			if err != nil {
				return toolError(err)
			}
			out := make([]map[string]any, 0, len(accounts))
			for _, a := range accounts {
				out = append(out, map[string]any{
					"id": a.ID, "name": a.Name, "provider": a.Provider, "status": a.Status,
				})
			}
			return toolOK(out)
		})

	case "list_agents":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "list_agents",
			Description: "列出 Agent 及其在线状态（用于确认部署通道可用性）。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
			agents, err := ops.ListAgents(ctx)
			if err != nil {
				return toolError(err)
			}
			out := make([]map[string]any, 0, len(agents))
			for _, a := range agents {
				out = append(out, map[string]any{
					"id": a.ID, "name": a.Name, "status": a.Status,
					"version": a.Version, "os": a.OS,
				})
			}
			return toolOK(out)
		})

	case "get_task_logs":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "get_task_logs",
			Description: "查询最近的任务日志（证书扫描、自动续期、到期提醒等）。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			Limit int `json:"limit,omitempty" jsonschema:"返回条数，默认 20，最大 100"`
		}) (*mcp.CallToolResult, any, error) {
			limit := in.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 100 {
				limit = 100
			}
			logs, err := ops.TaskLogs(ctx, limit)
			if err != nil {
				return toolError(err)
			}
			out := make([]map[string]any, 0, len(logs))
			for _, l := range logs {
				out = append(out, map[string]any{
					"id": l.ID, "type": l.Type, "status": l.Status,
					"detail": l.Detail, "started_at": l.StartedAt,
				})
			}
			return toolOK(out)
		})

	case "issue_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name: "issue_certificate",
			Description: "为域名签发 SSL 证书（破坏性低但受 CA 速率限制；ACME 签发可能耗时数分钟）。" +
				"成功后可按需自动部署到指定目标。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			Domains              []string `json:"domains" jsonschema:"域名列表（第一个为主域名）"`
			AccountID            string   `json:"account_id" jsonschema:"渠道账户 ID（list_accounts 获取）"`
			Verification         string   `json:"verification,omitempty" jsonschema:"验证方式：http01 或 dns01，默认 http01"`
			DNSProviderAccountID string   `json:"dns_provider_account_id,omitempty" jsonschema:"DNS 验证账户 ID（dns01 时推荐）"`
			DeployTargetIDs      []string `json:"deploy_target_ids,omitempty" jsonschema:"签发成功后自动部署的目标 ID"`
			NotifyEvents         []string `json:"notify_events,omitempty" jsonschema:"订阅的通知事件"`
		}) (*mcp.CallToolResult, any, error) {
			if len(in.Domains) == 0 {
				return toolError(fmt.Errorf("%w: domains 不能为空", domain.ErrInvalidArgument))
			}
			verification := in.Verification
			if verification == "" {
				verification = "http01"
			}
			cert, err := ops.IssueCertificate(ctx, IssueParams{
				Domains:              in.Domains,
				AccountID:            in.AccountID,
				Verification:         verification,
				DNSProviderAccountID: in.DNSProviderAccountID,
				DeployTargetIDs:      in.DeployTargetIDs,
				NotifyEvents:         in.NotifyEvents,
			})
			if err != nil {
				return toolError(err)
			}
			return toolOK(certBrief(cert, time.Now().UTC()))
		})

	case "renew_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "renew_certificate",
			Description: "续期证书（受 CA 速率限制，可能耗时数分钟）。id 与 domain 二选一。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			ID     string `json:"id,omitempty" jsonschema:"证书 ID"`
			Domain string `json:"domain,omitempty" jsonschema:"域名（与 id 二选一）"`
		}) (*mcp.CallToolResult, any, error) {
			if in.ID == "" && in.Domain == "" {
				return toolError(fmt.Errorf("%w: id 与 domain 至少提供一个", domain.ErrInvalidArgument))
			}
			cert, err := ops.RenewCertificate(ctx, in.ID, in.Domain)
			if err != nil {
				return toolError(err)
			}
			return toolOK(certBrief(cert, time.Now().UTC()))
		})

	case "revoke_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "revoke_certificate",
			Description: "吊销证书。警告：此操作不可逆，吊销后证书立即失效，依赖它的服务会中断。id 与 domain 二选一。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			ID     string `json:"id,omitempty" jsonschema:"证书 ID"`
			Domain string `json:"domain,omitempty" jsonschema:"域名（与 id 二选一）"`
		}) (*mcp.CallToolResult, any, error) {
			if in.ID == "" && in.Domain == "" {
				return toolError(fmt.Errorf("%w: id 与 domain 至少提供一个", domain.ErrInvalidArgument))
			}
			if err := ops.RevokeCertificate(ctx, in.ID, in.Domain); err != nil {
				return toolError(err)
			}
			return toolOK(map[string]any{"revoked": true})
		})

	case "import_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "import_certificate",
			Description: "导入既有证书（PEM 格式的证书与私钥），纳入 Argus 统一管理。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			Domains     []string `json:"domains" jsonschema:"域名列表（第一个为主域名）"`
			Certificate string   `json:"certificate" jsonschema:"证书 PEM（可含证书链）"`
			PrivateKey  string   `json:"private_key" jsonschema:"私钥 PEM"`
		}) (*mcp.CallToolResult, any, error) {
			if in.Certificate == "" || in.PrivateKey == "" {
				return toolError(fmt.Errorf("%w: certificate 与 private_key 不能为空", domain.ErrInvalidArgument))
			}
			cert, err := ops.ImportCertificate(ctx, ImportParams{
				Domains:     in.Domains,
				Certificate: in.Certificate,
				PrivateKey:  in.PrivateKey,
			})
			if err != nil {
				return toolError(err)
			}
			return toolOK(certBrief(cert, time.Now().UTC()))
		})

	case "deploy_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "deploy_certificate",
			Description: "把证书推送部署到目标（SSH / Agent / 本机 / K8s / Webhook）。未指定 target_ids 时部署证书绑定的全部目标。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			ID        string   `json:"id,omitempty" jsonschema:"证书 ID"`
			Domain    string   `json:"domain,omitempty" jsonschema:"域名（与 id 二选一）"`
			TargetIDs []string `json:"target_ids,omitempty" jsonschema:"目标 ID 列表，空为证书绑定的全部目标"`
		}) (*mcp.CallToolResult, any, error) {
			if in.ID == "" && in.Domain == "" {
				return toolError(fmt.Errorf("%w: id 与 domain 至少提供一个", domain.ErrInvalidArgument))
			}
			if err := ops.DeployCertificate(ctx, in.ID, in.Domain, in.TargetIDs); err != nil {
				return toolError(err)
			}
			return toolOK(map[string]any{"deployed": true})
		})

	case "test_deploy_target":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name:        "test_deploy_target",
			Description: "测试部署目标的连通性（SSH 连接 / Agent 在线 / 本机可写等）。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			ID string `json:"id" jsonschema:"部署目标 ID"`
		}) (*mcp.CallToolResult, any, error) {
			if in.ID == "" {
				return toolError(fmt.Errorf("%w: id 不能为空", domain.ErrInvalidArgument))
			}
			if err := ops.TestTarget(ctx, in.ID); err != nil {
				return toolError(err)
			}
			return toolOK(map[string]any{"reachable": true})
		})

	case "download_certificate":
		mcp.AddTool(s.sdk, &mcp.Tool{
			Name: "download_certificate",
			Description: "下载证书打包文件（含私钥）。警告：输出包含私钥，属敏感数据；" +
				"仅在 mcp.allow_key_download 开启时可用，且每次调用都会审计。",
		}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			ID     string `json:"id" jsonschema:"证书 ID"`
			Format string `json:"format" jsonschema:"服务器格式：nginx/apache/iis/tomcat/other"`
		}) (*mcp.CallToolResult, any, error) {
			if in.ID == "" {
				return toolError(fmt.Errorf("%w: id 不能为空", domain.ErrInvalidArgument))
			}
			data, filename, err := ops.DownloadCertificate(ctx, in.ID, in.Format)
			if err != nil {
				return toolError(err)
			}
			// 私钥内容不外泄到对话文本：返回文件名与大小，内容经审计后另行分发
			return toolOK(map[string]any{
				"filename": filename,
				"bytes":    len(data),
				"notice":   "含私钥的打包文件已生成；请通过受控通道获取，不要在对话中粘贴私钥内容",
			})
		})
	}
}

// certBriefs / certBrief 结构化证书摘要（避免把 PEM/私钥等大字段喂给模型）
func certBriefs(certs []*domain.Certificate, now time.Time) []map[string]any {
	out := make([]map[string]any, 0, len(certs))
	for _, c := range certs {
		out = append(out, certBrief(c, now))
	}
	return out
}

// certBrief now 显式传入（与 domain.ComputeCoverage 同风格），避免内部读时钟
// 导致调用方无法确定性地测试。
func certBrief(c *domain.Certificate, now time.Time) map[string]any {
	days := int64(0)
	if !c.NotAfter.IsZero() {
		days = int64(c.NotAfter.Sub(now).Hours() / 24)
	}
	return map[string]any{
		"id":              c.ID,
		"domains":         c.Domains,
		"primary_domain":  c.PrimaryDomain,
		"provider":        c.Provider,
		"status":          string(c.Status),
		"not_before":      c.NotBefore,
		"not_after":       c.NotAfter,
		"days_to_expire":  days,
		"verification":    string(c.Verification),
		"deploy_targets":  c.DeployTargetIDs,
	}
}

// toolOK / toolError 统一结果封装
func toolOK(data any) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(data)}},
	}, nil, nil
}

func toolError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil, nil
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}", err.Error())
	}
	return string(b)
}
