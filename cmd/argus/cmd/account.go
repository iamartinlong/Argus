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

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "渠道账户管理",
}

var accountListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出渠道账户",
	RunE: func(cmd *cobra.Command, args []string) error {
		if remoteEnabled() {
			data, err := mustRemote().do("GET", "/api/v1/acme-accounts", nil)
			if err != nil {
				return err
			}
			var accounts []map[string]any
			if err := json.Unmarshal(data, &accounts); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\t名称\t渠道\t状态")
			for _, a := range accounts {
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", a["id"], a["name"], a["provider"], a["status"])
			}
			_ = w.Flush()
			return nil
		}
		b, err := newBootstrap(configPath, false)
		if err != nil {
			return err
		}
		defer b.close()
		accounts, err := b.accountSvc.List(context.Background())
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\t名称\t渠道\t状态")
		for _, a := range accounts {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Provider, a.Status)
		}
		_ = w.Flush()
		return nil
	},
}

var accountCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "创建渠道账户",
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		name, _ := cmd.Flags().GetString("name")
		credentials, _ := cmd.Flags().GetString("credentials") // key=value,key2=value2
		if provider == "" {
			return fmt.Errorf("--provider 必填 (acme/aliyun/tencent)")
		}
		credMap := parseCredentials(credentials)
		input := application.CreateAccountInput{
			Provider:    provider,
			Name:        name,
			Credentials: credMap,
		}
		if remoteEnabled() {
			body := map[string]any{
				"provider": provider, "name": name, "credentials": credMap,
			}
			data, err := mustRemote().do("POST", "/api/v1/acme-accounts", body)
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
		account, err := b.accountSvc.Create(context.Background(), input)
		if err != nil {
			return err
		}
		fmt.Printf("账户已创建: %s (%s)\n", account.Name, account.Provider)
		return nil
	},
}

func init() {
	accountCmd.AddCommand(accountListCmd)
	accountCmd.AddCommand(accountCreateCmd)
	accountCreateCmd.Flags().String("provider", "", "渠道类型 (acme/aliyun/tencent)")
	accountCreateCmd.Flags().String("name", "", "账户名称")
	accountCreateCmd.Flags().String("credentials", "", "云凭据 key=value,key2=value2（aliyun: access_key_id,access_key_secret,region,contact_email,contact_phone,contact_name；tencent: secret_id,secret_key,contact_email,contact_phone）")
}

func parseCredentials(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 && kv[0] != "" {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

var _ = domain.RoleAdmin
