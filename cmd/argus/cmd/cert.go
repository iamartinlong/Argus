package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/domain"
)

var certCmd = &cobra.Command{
	Use:   "cert",
	Short: "证书管理",
}

var certListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出证书",
	RunE: func(cmd *cobra.Command, args []string) error {
		if remoteEnabled() {
			data, err := mustRemote().do("GET", "/api/v1/certificates", nil)
			if err != nil {
				return err
			}
			var certs []map[string]any
			if err := json.Unmarshal(data, &certs); err != nil {
				return err
			}
			printCertificates(certs)
			return nil
		}
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()
		certs, err := b.certSvc.List(context.Background())
		if err != nil {
			return err
		}
		rows := make([]map[string]any, 0, len(certs))
		for _, c := range certs {
			rows = append(rows, map[string]any{
				"id": c.ID, "domain": c.PrimaryDomain, "status": string(c.Status),
				"provider": c.Provider, "not_after": c.NotAfter.Format("2006-01-02"),
			})
		}
		printCertificates(rows)
		return nil
	},
}

var certIssueCmd = &cobra.Command{
	Use:   "issue",
	Short: "签发证书（--domain 必填）",
	RunE: func(cmd *cobra.Command, args []string) error {
		domain, _ := cmd.Flags().GetString("domain")
		account, _ := cmd.Flags().GetString("account")
		verification, _ := cmd.Flags().GetString("verification")
		targets, _ := cmd.Flags().GetString("targets")
		if domain == "" {
			return fmt.Errorf("--domain 必填")
		}
		var targetIDs []string
		if targets != "" {
			targetIDs = splitCSV(targets)
		}
		body := map[string]any{
			"domains": []string{domain}, "verification": verification,
			"deploy_target_ids": targetIDs,
		}
		if account != "" {
			body["account_id"] = account
		}
		if remoteEnabled() {
			data, err := mustRemote().do("POST", "/api/v1/certificates", body)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()
		if account == "" {
			accounts, err := b.accountSvc.List(context.Background())
			if err != nil {
				return err
			}
			if len(accounts) == 0 {
				return fmt.Errorf("请先创建渠道账户（argus account create）")
			}
			account = accounts[0].ID
		}
		cert, err := b.certSvc.Issue(context.Background(), applicationIssueInput(domain, account, verification, targetIDs))
		if err != nil {
			return err
		}
		printCertificate(cert)
		return nil
	},
}

var certRenewCmd = &cobra.Command{
	Use:   "renew",
	Short: "续期证书（--id 必填）",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id 必填")
		}
		if remoteEnabled() {
			data, err := mustRemote().do("POST", "/api/v1/certificates/"+id+"/renew", nil)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()
		cert, err := b.certSvc.Renew(context.Background(), id, nil, "")
		if err != nil {
			return err
		}
		printCertificate(cert)
		return nil
	},
}

var certRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "吊销证书（--id 必填）",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id 必填")
		}
		if remoteEnabled() {
			_, err := mustRemote().do("POST", "/api/v1/certificates/"+id+"/revoke", nil)
			return err
		}
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()
		return b.certSvc.Revoke(context.Background(), id)
	},
}

var certDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "部署证书到绑定目标（--id 必填）",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id 必填")
		}
		if remoteEnabled() {
			_, err := mustRemote().do("POST", "/api/v1/certificates/"+id+"/deploy", nil)
			return err
		}
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()
		cert, err := b.certSvc.Get(context.Background(), id)
		if err != nil {
			return err
		}
		return b.deploySvc.DeployToTargets(context.Background(), id, cert.DeployTargetIDs)
	},
}

func init() {
	certCmd.AddCommand(certListCmd)
	certCmd.AddCommand(certIssueCmd)
	certCmd.AddCommand(certRenewCmd)
	certCmd.AddCommand(certRevokeCmd)
	certCmd.AddCommand(certDeployCmd)
	certIssueCmd.Flags().String("domain", "", "域名")
	certIssueCmd.Flags().String("account", "", "渠道账户 ID（默认第一个）")
	certIssueCmd.Flags().String("verification", "http01", "验证方式 (http01/dns01)")
	certIssueCmd.Flags().String("targets", "", "部署目标 ID 列表（逗号分隔）")
	certRenewCmd.Flags().String("id", "", "证书 ID")
	certRevokeCmd.Flags().String("id", "", "证书 ID")
	certDeployCmd.Flags().String("id", "", "证书 ID")
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func printCertificates(rows []map[string]any) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\t域名\t状态\t渠道\t到期")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%v\t%v\t%v\t%v\n", r["id"], r["domain"], r["status"], r["provider"], r["not_after"])
	}
	_ = w.Flush()
}

func printCertificate(c *domain.Certificate) {
	fmt.Printf("ID: %s\n域名: %s\n状态: %s\n渠道: %s\n到期: %s\n",
		c.ID, c.PrimaryDomain, c.Status, c.Provider, c.NotAfter.Format("2006-01-02"))
}

func applicationIssueInput(domain, account, verification string, targetIDs []string) application.IssueInput {
	return application.IssueInput{
		Domains:         []string{domain},
		AccountID:       account,
		Verification:    domainVerification(verification),
		DeployTargetIDs: targetIDs,
	}
}

func domainVerification(v string) domain.VerificationType {
	if v == "dns01" {
		return domain.VerifyDNS01
	}
	return domain.VerifyHTTP01
}

// mustRemote 获取远程客户端（带缓存）
var remoteClientCache *remoteClient

func mustRemote() *remoteClient {
	if remoteClientCache == nil {
		c, err := newRemoteClient()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		remoteClientCache = c
	}
	return remoteClientCache
}
