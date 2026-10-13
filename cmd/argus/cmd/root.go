package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "1.1.1"

	// 全局远程模式参数
	remoteServer string
	remoteToken  string
	configPath   string

	// ServiceMode 由宿主（Windows 服务）置 true，供 /setup/restart 降级为手动指引。
	ServiceMode bool
)

func isServiceMode() bool { return ServiceMode }

var rootCmd = &cobra.Command{
	Use:   "argus",
	Short: "Argus - 永不眨眼的 SSL 证书守护者",
	Long: `Argus 得名于希腊神话中的百眼巨人阿耳戈斯——无论何时总有一双眼睛
在注视着。SSL 证书自动化的核心正是如此：永远盯着、到期自动续。

Argus 是集中式 SSL 证书管理平台：ACME/云厂商签发、自动续期、
多服务器部署推送与到期通知。
本地模式直接操作本机数据；指定 --server 与 --token 时切换为远程 API 模式。`,
	Version: version,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "配置文件路径 (YAML)")
	rootCmd.PersistentFlags().StringVar(&remoteServer, "server", "", "远程服务地址 (如 https://cert.example.com)，启用远程 API 模式")
	rootCmd.PersistentFlags().StringVar(&remoteToken, "token", "", "API Token（远程模式必需；也可用环境变量 ARGUS_API_TOKEN）")
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(initAdminCmd)
	rootCmd.AddCommand(resetPasswordCmd)
	rootCmd.AddCommand(genKeysCmd)
	rootCmd.AddCommand(certCmd)
	rootCmd.AddCommand(accountCmd)
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Argus", version, "- 永不眨眼的 SSL 证书守护者")
	},
}

// apiToken 返回远程模式 token（flag 或环境变量）
func apiToken() string {
	if remoteToken != "" {
		return remoteToken
	}
	return os.Getenv("ARGUS_API_TOKEN")
}

// remoteEnabled 是否远程 API 模式
func remoteEnabled() bool {
	return remoteServer != ""
}
