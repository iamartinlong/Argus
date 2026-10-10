package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"github.com/iamartinlong/Argus/internal/domain"
)

// Local 本机目录部署 + hook 命令。
// 按场景分发：通用/Nginx/Apache 写 PEM+Key+命令；Tomcat 本机合成 PFX；
// IIS 场景本机合成 PFX 并执行内置导入/绑定脚本（需本机为 Windows）。
type Local struct{}

func (Local) Type() string { return "local" }

func (Local) IsAsync() bool { return false }

type localConfig struct {
	Scene       string `json:"scene"` // 场景：generic/nginx/apache/tomcat/iis
	CertPath    string `json:"cert_path"`
	KeyPath     string `json:"key_path"`
	PostCommand string `json:"post_command"`
	PfxPath     string `json:"pfx_path"`  // Tomcat/IIS PFX 落盘路径
	Bindings    []int  `json:"bindings"`  // IIS https 端口
	SiteName    string `json:"site_name"` // IIS 站点名（可选）
	Hostname    string `json:"hostname"`  // IIS 主机名（SNI，多站点必填，否则通配）
	ServerNames string `json:"server_names"` // Nginx SNI，多域逗号/空格分隔（同端口多 server_name）
}

func sceneOfLocal(cfg *localConfig) domain.DeployScene {
	s := domain.DeployScene(cfg.Scene)
	if !s.Valid() {
		return domain.SceneGeneric
	}
	return s
}

func (Local) Deploy(ctx context.Context, payload Payload, config map[string]any) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := decodeConfig[localConfig](config)
	if err != nil {
		return nil, err
	}
	switch sceneOfLocal(cfg) {
	case domain.SceneTomcat:
		return deployLocalPFX(ctx, cfg, payload, "Tomcat")
	case domain.SceneIIS:
		return deployLocalIIS(ctx, cfg, payload)
	case domain.SceneNginx:
		return deployLocalNginx(ctx, cfg, payload)
	default:
		// 通用 / Apache
		if cfg.CertPath == "" {
			return nil, fmt.Errorf("local 目标缺少 cert_path")
		}
		if err := writeFile(cfg.CertPath, payload.CertificatePEM, 0o644); err != nil {
			return nil, fmt.Errorf("写入证书: %w", err)
		}
		if cfg.KeyPath != "" {
			if err := writeFile(cfg.KeyPath, payload.PrivateKeyPEM, 0o600); err != nil {
				return nil, fmt.Errorf("写入私钥: %w", err)
			}
		}
		msg := fmt.Sprintf("证书已写入 %s", cfg.CertPath)
		if cfg.PostCommand != "" {
			cmd := renderHook(cfg.PostCommand, cfg.CertPath, cfg.KeyPath, payload)
			out, err := runCommand(ctx, cmd)
			if err != nil {
				return nil, fmt.Errorf("hook 执行失败: %w (%s)", err, out)
			}
			msg += "；hook 已执行"
		}
		return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
	}
}

// deployLocalPFX 本机合成 PFX 写入 + 可选 hook（Tomcat 等）
func deployLocalPFX(ctx context.Context, cfg *localConfig, payload Payload, label string) (*Result, error) {
	if cfg.PfxPath == "" {
		return nil, fmt.Errorf("%s 场景缺少 pfx_path", label)
	}
	pfxPassword, err := randomPassword(16)
	if err != nil {
		return nil, err
	}
	pfxData, err := BuildPFX(payload.CertificatePEM, payload.PrivateKeyPEM, pfxPassword)
	if err != nil {
		return nil, fmt.Errorf("合成 PFX: %w", err)
	}
	if err := writeFile(cfg.PfxPath, string(pfxData), 0o600); err != nil {
		return nil, fmt.Errorf("写入 PFX: %w", err)
	}
	msg := fmt.Sprintf("PFX 已写入 %s", cfg.PfxPath)
	if cfg.PostCommand != "" {
		hook := strings.NewReplacer(
			"{pfx_path}", shellQuote(cfg.PfxPath),
			"{pfx_password}", shellQuote(pfxPassword),
			VarDomain, shellQuote(payload.PrimaryDomain),
		).Replace(cfg.PostCommand)
		out, err := runCommand(ctx, hook)
		if err != nil {
			return nil, fmt.Errorf("hook 执行失败: %w (%s)", err, out)
		}
		msg += "；hook 已执行"
	}
	return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
}

// deployLocalIIS 本机 IIS 场景：合成 PFX + 执行内置导入/绑定脚本（需本机 Windows）
func deployLocalIIS(ctx context.Context, cfg *localConfig, payload Payload) (*Result, error) {
	if cfg.PfxPath == "" {
		return nil, fmt.Errorf("IIS 场景缺少 pfx_path")
	}
	if len(cfg.Bindings) == 0 {
		return nil, fmt.Errorf("IIS 场景缺少 bindings（https 端口列表）")
	}
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("本机通道 IIS 场景需服务端为 Windows（当前 %s）", runtime.GOOS)
	}
	pfxPassword, err := randomPassword(16)
	if err != nil {
		return nil, err
	}
	pfxData, err := BuildPFX(payload.CertificatePEM, payload.PrivateKeyPEM, pfxPassword)
	if err != nil {
		return nil, fmt.Errorf("合成 PFX: %w", err)
	}
	if err := writeFile(cfg.PfxPath, string(pfxData), 0o600); err != nil {
		return nil, fmt.Errorf("写入 PFX: %w", err)
	}
	// PFX 含私钥：无论脚本成功与否都必须删除（成功路径也无需保留）
	defer func() { _ = os.Remove(cfg.PfxPath) }()
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = payload.PrimaryDomain
	}
	script := RenderIISScriptV2WithHostname(cfg.PfxPath, pfxPassword, cfg.Bindings, cfg.SiteName, hostname)
	out, err := runCommand(ctx, script)
	if err != nil {
		return nil, fmt.Errorf("IIS 绑定更新失败: %w (%s)", err, out)
	}
	msg := "IIS https 绑定已更新（" + out + "）"
	if cfg.SiteName != "" {
		msg += "；站点 " + cfg.SiteName + " 绑定已同步"
	}
	return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
}

// deployLocalNginx Nginx SNI：路径支持 {domain} 占位，同端口多 server_name 通过多目标或同目标多域实现
func deployLocalNginx(ctx context.Context, cfg *localConfig, payload Payload) (*Result, error) {
	if cfg.CertPath == "" {
		return nil, fmt.Errorf("Nginx 场景缺少 cert_path")
	}
	certPath := strings.ReplaceAll(cfg.CertPath, "{domain}", payload.PrimaryDomain)
	keyPath := strings.ReplaceAll(cfg.KeyPath, "{domain}", payload.PrimaryDomain)
	if err := writeFile(certPath, payload.CertificatePEM, 0o644); err != nil {
		return nil, fmt.Errorf("写入证书: %w", err)
	}
	if keyPath != "" {
		if err := writeFile(keyPath, payload.PrivateKeyPEM, 0o600); err != nil {
			return nil, fmt.Errorf("写入私钥: %w", err)
		}
	}
	msg := fmt.Sprintf("证书已写入 %s", certPath)
	hook := cfg.PostCommand
	if hook == "" {
		hook = "nginx -t && nginx -s reload"
	}
	// 渲染 hook 模板
	cmd := renderHook(hook, certPath, keyPath, payload)
	// 预检：nginx -t
	if out, err := runCommand(ctx, "nginx -t"); err != nil {
		return nil, fmt.Errorf("nginx 配置校验失败: %w (%s)", err, out)
	}
	out, err := runCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("hook 执行失败: %w (%s)", err, out)
	}
	if out != "" {
		msg += "；" + out
	} else {
		msg += "；hook 已执行"
	}
	if cfg.ServerNames != "" {
		msg += "（" + cfg.ServerNames + "）"
	}
	return &Result{Checksum: ChecksumOf(payload), Message: msg}, nil
}

func (Local) Test(_ context.Context, config map[string]any) error {
	cfg, err := decodeConfig[localConfig](config)
	if err != nil {
		return err
	}
	switch sceneOfLocal(cfg) {
	case domain.SceneTomcat:
		if cfg.PfxPath == "" {
			return fmt.Errorf("pfx_path 不能为空")
		}
	case domain.SceneIIS:
		if cfg.PfxPath == "" {
			return fmt.Errorf("pfx_path 不能为空")
		}
		if len(cfg.Bindings) == 0 {
			return fmt.Errorf("bindings（https 端口列表）不能为空")
		}
	default:
		if cfg.CertPath == "" {
			return fmt.Errorf("cert_path 不能为空")
		}
	}
	return nil
}

// ---------------- 公共 ----------------

// writeFile 原子写文件（唯一临时名 + 重命名，避免并发部署互相覆盖）
func writeFile(path string, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	renamed = true
	return nil
}

// renderHook 替换 hook 模板变量；替换值做 shell 单引号转义（纵深防御，防命令注入）
// shellQuote 见 ssh_shared.go（sh 与 PowerShell 通用单引号转义）
func renderHook(tpl, certPath, keyPath string, payload Payload) string {
	r := strings.NewReplacer(
		VarCertPath, shellQuote(certPath),
		VarKeyPath, shellQuote(keyPath),
		VarFullchainPath, shellQuote(certPath), // fullchain 与 cert 内容一致（已含完整链）
		VarDomain, shellQuote(payload.PrimaryDomain),
	)
	return r.Replace(tpl)
}

// runCommand 执行本地命令（Windows 用 powershell，其余用 sh -c），60s 超时
func runCommand(ctx context.Context, cmdLine string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// 强制 UTF-8：PowerShell 默认 GBK(936) 导致中文乱码，需 chcp 65001 + OutputEncoding
		utf8Prefix := "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; chcp 65001 >$null; "
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", utf8Prefix+cmdLine)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdLine)
	}
	configureProcessGroup(cmd)
	// 覆盖默认 Cancel：ctx 取消/超时时终止整个进程组（避免 nginx 等孙进程存活）
	cmd.Cancel = func() error {
		killProcessTree(cmd)
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	text := strings.TrimSpace(decodeOutput(out.Bytes()))
	if err != nil && ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return text, fmt.Errorf("命令执行超时（60s）")
		}
		return text, fmt.Errorf("命令被取消: %w", ctx.Err())
	}
	return text, err
}

// decodeOutput 尝试 UTF-8，失败则按 GBK 解码（Windows 中文系统 PowerShell/netsh 中文输出）
func decodeOutput(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), b)
	if err != nil {
		return string(b)
	}
	if utf8.Valid(decoded) {
		return string(decoded)
	}
	return string(decoded)
}

// decodeConfig JSON 解码适配器配置
func decodeConfig[T any](raw map[string]any) (*T, error) {
	var out T
	data, err := jsonMarshal(raw)
	if err != nil {
		return nil, err
	}
	if err := jsonUnmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析目标配置: %w", err)
	}
	return &out, nil
}
