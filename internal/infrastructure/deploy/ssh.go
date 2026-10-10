package deploy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iamartinlong/Argus/internal/domain"

	"golang.org/x/crypto/ssh"
)

// SSH Agentless 通道：sftp 推送证书 + 远程 hook 执行。
// 按场景（Scene）分发：通用/Nginx/Apache 推 PEM+Key+hook；Tomcat 合成 PFX 推送；
// Windows IIS 走 deployIISOverSSH（PFX + PowerShell 导入 + https 绑定）。
type SSH struct {
	timeout time.Duration
}

func NewSSH(timeout time.Duration) *SSH {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &SSH{timeout: timeout}
}

func (SSH) Type() string { return "ssh" }

func (SSH) IsAsync() bool { return false }

type sshConfig struct {
	Scene      string `json:"scene"` // 场景：generic/nginx/apache/tomcat/iis
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	PrivateKey string `json:"private_key"` // PEM 私钥内容（AES-GCM 加密存储于 config_enc）
	Password   string `json:"password"`    // 可选密码认证
	CertPath   string `json:"cert_path"`
	KeyPath    string `json:"key_path"`
	Hook       string `json:"hook"`      // 部署后命令（模板）
	KnownHosts string `json:"known_hosts"` // known_hosts 内容；提供时严格校验主机指纹（防中间人）
	// Tomcat / IIS 场景
	PfxPath  string `json:"pfx_path"`  // PFX 落盘路径
	Bindings []int  `json:"bindings"`  // https 绑定端口（IIS）
	SiteName string `json:"site_name"` // IIS 站点名（可选）
	Hostname string `json:"hostname"`  // IIS 主机名（SNI，多站点必填）
	ServerNames string `json:"server_names"` // Nginx SNI，多域

}

// sceneOf 解析场景，缺省为通用
func sceneOf(cfg *sshConfig) domain.DeployScene {
	s := domain.DeployScene(cfg.Scene)
	if !s.Valid() {
		return domain.SceneGeneric
	}
	return s
}

func (s *SSH) Deploy(ctx context.Context, payload Payload, config map[string]any) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := decodeConfig[sshConfig](config)
	if err != nil {
		return nil, err
	}
	if cfg.Host == "" || cfg.User == "" {
		return nil, fmt.Errorf("ssh 目标缺少 host/user")
	}
	// 场景必填校验在连接前（尽早失败）
	switch sceneOf(cfg) {
	case domain.SceneTomcat:
		if cfg.PfxPath == "" {
			return nil, fmt.Errorf("Tomcat 场景缺少 pfx_path")
		}
	case domain.SceneIIS:
		if cfg.PfxPath == "" {
			return nil, fmt.Errorf("IIS 场景缺少 pfx_path")
		}
		if len(cfg.Bindings) == 0 {
			return nil, fmt.Errorf("IIS 场景缺少 bindings（https 端口列表）")
		}
	default:
		if cfg.CertPath == "" {
			return nil, fmt.Errorf("ssh 目标缺少 cert_path")
		}
	}
	client, err := s.dial(cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	switch sceneOf(cfg) {
	case domain.SceneTomcat:
		return deployTomcatOverSSH(client, cfg, payload)
	case domain.SceneIIS:
		hostname := cfg.Hostname
		if hostname == "" {
			hostname = payload.PrimaryDomain
		}
		msg, err := deployIISOverSSHWithHostname(client, cfg.Host, cfg.PfxPath, cfg.Bindings, cfg.SiteName, hostname, payload)
		if err != nil {
			return nil, err
		}
		return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
	case domain.SceneNginx:
		certPath := strings.ReplaceAll(cfg.CertPath, "{domain}", payload.PrimaryDomain)
		keyPath := strings.ReplaceAll(cfg.KeyPath, "{domain}", payload.PrimaryDomain)
		if err := sshPutFile(client, certPath, payload.CertificatePEM, "0644"); err != nil {
			return nil, fmt.Errorf("推送证书: %w", err)
		}
		msg2 := fmt.Sprintf("已推送至 %s:%s", cfg.Host, certPath)
		if keyPath != "" && payload.PrivateKeyPEM != "" {
			if err := sshPutFile(client, keyPath, payload.PrivateKeyPEM, "0600"); err != nil {
				return nil, fmt.Errorf("推送私钥: %w", err)
			}
		}
		hook := cfg.Hook
		if hook == "" {
			hook = "nginx -t && nginx -s reload"
		}
		hook = renderHook(hook, certPath, keyPath, payload)
		if out, err := sshRunRemote(client, "nginx -t", false); err != nil {
			return nil, fmt.Errorf("nginx 配置校验失败: %w (%s)", err, out)
		}
		if out, err := sshRunRemote(client, hook, false); err != nil {
			return nil, fmt.Errorf("远程 hook 执行失败: %w (%s)", err, out)
		} else if out != "" {
			msg2 += "；" + out
		} else {
			msg2 += "；hook 已执行"
		}
		if cfg.ServerNames != "" {
			msg2 += "（" + cfg.ServerNames + "）"
		}
		return &Result{Checksum: ChecksumOf(payload), Message: msg2}, nil
	default:
		// 通用 / Apache：推 PEM + Key + hook
		if err := sshPutFile(client, cfg.CertPath, payload.CertificatePEM, "0644"); err != nil {
			return nil, fmt.Errorf("推送证书: %w", err)
		}
		msg := fmt.Sprintf("已推送至 %s:%s", cfg.Host, cfg.CertPath)
		if cfg.KeyPath != "" && payload.PrivateKeyPEM != "" {
			if err := sshPutFile(client, cfg.KeyPath, payload.PrivateKeyPEM, "0600"); err != nil {
				return nil, fmt.Errorf("推送私钥: %w", err)
			}
		}
		if cfg.Hook != "" {
			hook := renderHook(cfg.Hook, cfg.CertPath, cfg.KeyPath, payload)
			out, err := sshRunRemote(client, hook, false)
			if err != nil {
				return nil, fmt.Errorf("远程 hook 执行失败: %w (%s)", err, out)
			}
			msg += "；hook 已执行"
		}
		return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
	}
}

func (s *SSH) Test(ctx context.Context, config map[string]any) error {
	cfg, err := decodeConfig[sshConfig](config)
	if err != nil {
		return err
	}
	client, err := s.dial(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	// IIS 场景：探测目标是否为 PowerShell 环境（防 Linux 误配）
	if sceneOf(cfg) == domain.SceneIIS {
		out, err := sshRunRemote(client, `$PSVersionTable.PSVersion.Major`, true)
		if err != nil {
			return fmt.Errorf("目标非可用 PowerShell 环境（需 Windows OpenSSH Server）: %w (%s)", err, out)
		}
		if strings.TrimSpace(out) == "" {
			return fmt.Errorf("PowerShell 环境探测失败")
		}
	}
	return nil
}

func (s *SSH) dial(cfg *sshConfig) (*ssh.Client, error) {
	return sshDial(sshCreds{
		Host: cfg.Host, Port: cfg.Port, User: cfg.User,
		PrivateKey: cfg.PrivateKey, Password: cfg.Password, KnownHosts: cfg.KnownHosts,
	}, s.timeout)
}

// deployTomcatOverSSH 服务端合成 PFX 推送 + 可选 hook（Tomcat keystore 替换等）。
func deployTomcatOverSSH(client *ssh.Client, cfg *sshConfig, payload Payload) (*Result, error) {
	if cfg.PfxPath == "" {
		return nil, fmt.Errorf("Tomcat 场景缺少 pfx_path")
	}
	pfxPassword, err := randomPassword(16)
	if err != nil {
		return nil, err
	}
	pfxData, err := BuildPFX(payload.CertificatePEM, payload.PrivateKeyPEM, pfxPassword)
	if err != nil {
		return nil, fmt.Errorf("合成 PFX: %w", err)
	}
	if err := sshPutFileBytes(client, cfg.PfxPath, pfxData, "0600"); err != nil {
		return nil, fmt.Errorf("推送 PFX: %w", err)
	}
	msg := fmt.Sprintf("PFX 已推送至 %s:%s", cfg.Host, cfg.PfxPath)
	if cfg.Hook != "" {
		// hook 模板支持 {pfx_path} {pfx_password} {domain}
		hook := strings.NewReplacer(
			"{pfx_path}", shellQuote(cfg.PfxPath),
			"{pfx_password}", shellQuote(pfxPassword),
			VarDomain, shellQuote(payload.PrimaryDomain),
		).Replace(cfg.Hook)
		out, err := sshRunRemote(client, hook, false)
		if err != nil {
			return nil, fmt.Errorf("远程 hook 执行失败: %w (%s)", err, out)
		}
		msg += "；hook 已执行"
	}
	return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
}
