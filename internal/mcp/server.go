// Package mcp 实现 MCP（Model Context Protocol）Server：让三方 AI Agent
// （Claude Code / Cline / Zed / Claude Desktop 等）以自然语言完成证书的查询、
// 签发、续期、吊销、部署等生命周期管理。
//
// 分层约束（NFR-3）：本包只依赖 application 层接口，不反向依赖 delivery/httpapi；
// 它是与 Web / REST API / CLI 并列的第四个交付界面。
//
// 传输（M1）：仅 stdio——
//   - 本地模式：直连数据库（进程文件访问即信任，与 CLI 同等模型）
//   - 远程模式：经既有 REST API 代理（服务端强制 API Token + scope）
//
// HTTP streamable HTTP 传输、resources/prompts、OAuth 属 M2，此处不占位实现。
package mcp

import (
	"context"
	"log/slog"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iamartinlong/Argus/internal/domain"
)

// CertOps MCP 工具所需的业务能力集合。由本地（ops_local.go）与远程
// （ops_remote.go）两种实现提供；工具层只面对此接口，不关心数据通路。
type CertOps interface {
	ListCertificates(ctx context.Context, f ListFilter) ([]*domain.Certificate, error)
	GetCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error)
	IssueCertificate(ctx context.Context, in IssueParams) (*domain.Certificate, error)
	RenewCertificate(ctx context.Context, id, domainName string) (*domain.Certificate, error)
	RevokeCertificate(ctx context.Context, id, domainName string) error
	ImportCertificate(ctx context.Context, in ImportParams) (*domain.Certificate, error)
	ListTargets(ctx context.Context) ([]*domain.DeployTarget, error)
	DeployCertificate(ctx context.Context, id, domainName string, targetIDs []string) error
	TestTarget(ctx context.Context, id string) error
	ListAccounts(ctx context.Context, typ string) ([]AccountBrief, error)
	ListAgents(ctx context.Context) ([]*domain.Agent, error)
	TaskLogs(ctx context.Context, limit int) ([]*domain.TaskLog, error)
	DownloadCertificate(ctx context.Context, id, format string) ([]byte, string, error) // data, filename
}

// ListFilter 证书列表过滤条件（对应 T1 list_certificates 入参）
type ListFilter struct {
	Status             string // valid | expiring | expired | pending | revoked | failed（空=全部）
	ExpiringWithinDays int    // >0 时只返回 N 天内到期的
	Keyword            string // 域名模糊匹配
}

// IssueParams 签发参数（对齐 application.IssueInput，对应 T7）
type IssueParams struct {
	Domains               []string
	AccountID             string
	Verification          string // http01 | dns01
	DNSProviderAccountID  string // 可选：复用 DNS 验证账户
	DeployTargetIDs       []string // 可选：签发成功后自动部署
	NotifyEvents          []string // 可选：订阅的通知事件
}

// ImportParams 导入参数（对应 T10）
type ImportParams struct {
	Domains     []string
	Certificate string // PEM
	PrivateKey  string // PEM
}

// AccountBrief 渠道账户摘要（脱敏；对应 T4）
type AccountBrief struct {
	ID       string
	Name     string
	Provider string
	Status   string
}

// Config MCP Server 配置
type Config struct {
	// AllowKeyDownload 是否注册私钥下载工具（T13）。默认 false——私钥经 AI
	// 通道外泄面最大，需显式开启且每次调用审计。
	AllowKeyDownload bool
	// Scopes 本地模式工具收敛（空=全部注册）。远程模式由 token 自身 scope
	// 在服务端强制，客户端仅作预过滤。
	Scopes []string
}

// Server MCP Server 装配：SDK Server + 业务能力 + 配置。
type Server struct {
	sdk *mcp.Server
	ops CertOps
	cfg Config
}

// NewServer 装配 MCP Server 并注册工具。
// version 取二进制版本号（与 `argus version` 一致）。
func NewServer(ops CertOps, version string, cfg Config) *Server {
	sdk := mcp.NewServer(&mcp.Implementation{
		Name:        "argus",
		Title:       "Argus",
		Description: "SSL 证书管理平台：查询、签发、续期、吊销、部署证书",
		Version:     version,
	}, &mcp.ServerOptions{
		// stdout 是 JSON-RPC 协议通道：任何日志必须走 stderr，
		// 否则会污染协议帧导致客户端解析失败。
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		Instructions: "Argus 是集中式 SSL 证书管理平台。" +
			"可用工具查询证书与部署目标、签发/续期/吊销证书、推送部署。" +
			"吊销操作不可逆；签发与续期受 CA 速率限制约束，可能耗时数分钟。",
	})
	s := &Server{sdk: sdk, ops: ops, cfg: cfg}
	s.registerTools()
	return s
}

// Run 在 stdio 上服务，阻塞至客户端断开或 ctx 取消。
func (s *Server) Run(ctx context.Context) error {
	return s.sdk.Run(ctx, &mcp.StdioTransport{})
}

// registerTools 注册工具集（按 cfg.Scopes 收敛；T13 视 AllowKeyDownload）。
// M1a 阶段为零工具（仅验证协议骨架），M1b 填充 13 个工具。
func (s *Server) registerTools() {
	// TODO(M1b): registerTools
}
