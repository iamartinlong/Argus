package cmd

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/iamartinlong/Argus/internal/mcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "启动 MCP Server（stdio，供 AI Agent 连接）",
	Long: `以 stdio 传输启动 MCP（Model Context Protocol）Server，供 Claude Code、
Cline、Zed、Claude Desktop 等 AI Agent 调用 Argus 的证书管理能力。

本地模式（默认）：
  直连本机数据库，无需网络端口与 token（进程权限即信任，与 CLI 同等模型）。

远程模式：
  --server https://cert.example.com --token st_xxx（或环境变量 ARGUS_API_TOKEN），
  经服务端 REST API 代理，由服务端强制 token 的 scope 权限。

注意：stdout 是 MCP 协议通道，本命令不向其打印任何日志（诊断信息走 stderr）。
私钥下载工具默认不注册，需在 config.yaml 配置 mcp.allow_key_download: true。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if configPath == "" {
			configPath = "config.yaml"
		}
		// 本地模式：复用 serve 的装配（库/服务/调度器），但不启动 HTTP 与调度。
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()

		// TODO(M1b): 接入 ops := mcp.NewLocalOps(b) 与 mcp.Config{AllowKeyDownload: b.cfg.MCP.AllowKeyDownload}
		// （M1a 为零工具骨架，业务能力尚未接线）
		server := mcp.NewServer(nil, version, mcp.Config{})
		log.Printf("Argus %s MCP Server 启动（stdio）", version)
		return server.Run(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
