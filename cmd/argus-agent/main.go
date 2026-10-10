package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.5"

var configPath string

type config struct {
	ServerURL    string `json:"server_url"`
	Token        string `json:"token"`
	PollInterval int    `json:"poll_interval"` // 秒
}

type taskPayload struct {
	TaskID         string `json:"task_id"`
	CertPath       string `json:"cert_path"`
	KeyPath        string `json:"key_path"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	Hook           string `json:"hook"`
	IsWindows      bool   `json:"is_windows"`
	PfxPath        string `json:"pfx_path,omitempty"`
	PfxData        string `json:"pfx_data,omitempty"` // base64，服务端合成
	PfxPassword    string `json:"pfx_password,omitempty"`
}

type taskResponse struct {
	Task *taskPayload `json:"task"`
}

type resultRequest struct {
	TaskID   string `json:"task_id"`
	Success  bool   `json:"success"`
	Checksum string `json:"checksum"`
	Message  string `json:"message"`
}

func main() {
	flag.StringVar(&configPath, "config", "agent.json", "配置文件路径")
	upgradeSelfFlag := flag.Bool("upgrade-self", false, "升级替换模式（内部使用）")
	upgradeTarget := flag.String("target", "", "升级目标主文件（内部使用）")
	upgradeSource := flag.String("upgrade-source", "", "升级来源副本（内部使用）")
	flag.Parse()

	if *upgradeSelfFlag {
		runUpgradeSelf(*upgradeTarget, *upgradeSource)
		return
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if cfg.ServerURL == "" || cfg.Token == "" {
		log.Fatalf("配置文件需包含 server_url 与 token")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10
	}
	client := &http.Client{Timeout: 60 * time.Second}

	log.Printf("Argus Agent %s 连接 %s（轮询间隔 %ds）", version, cfg.ServerURL, cfg.PollInterval)

	// 首次注册/心跳
	register(cfg, client)

	for {
		// 检查升级指令
		if err := checkUpgrade(cfg, client); err != nil {
			log.Printf("检查升级失败: %v", err)
		}
		task, err := fetchTask(cfg, client)
		if err != nil {
			log.Printf("拉取任务失败: %v（%ds 后重试）", err, cfg.PollInterval)
			time.Sleep(time.Duration(cfg.PollInterval) * time.Second)
			continue
		}
		if task == nil {
			time.Sleep(time.Duration(cfg.PollInterval) * time.Second)
			continue
		}
		log.Printf("收到任务 %s：%s", task.TaskID, task.CertPath)
		executeTask(cfg, client, task)
	}
}

func loadConfig(path string) (*config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func register(cfg *config, client *http.Client) {
	body, _ := json.Marshal(map[string]string{
		"token": cfg.Token, "version": version, "os": runtime.GOOS + "/" + runtime.GOARCH,
	})
	req, _ := http.NewRequest(http.MethodPost, cfg.ServerURL+"/api/v1/agent/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("注册失败: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	log.Printf("注册完成（%d）", resp.StatusCode)
}

func fetchTask(cfg *config, client *http.Client) (*taskPayload, error) {
	req, _ := http.NewRequest(http.MethodGet, cfg.ServerURL+"/api/v1/agent/tasks", nil)
	req.Header.Set("X-Agent-Token", cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("Agent Token 无效")
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	var tr taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return tr.Task, nil
}

func executeTask(cfg *config, client *http.Client, task *taskPayload) {
	// 1. 写证书与私钥（路径为空则跳过，如纯 IIS/PFX 场景）
	if task.CertPath != "" {
		if err := writeFile(task.CertPath, task.CertificatePEM, 0o644); err != nil {
			submitResult(cfg, client, task.TaskID, false, "", "写入证书失败: "+err.Error())
			return
		}
	}
	if task.KeyPath != "" && task.PrivateKeyPEM != "" {
		if err := writeFile(task.KeyPath, task.PrivateKeyPEM, 0o600); err != nil {
			submitResult(cfg, client, task.TaskID, false, "", "写入私钥失败: "+err.Error())
			return
		}
	}
	// 2. IIS 场景：写入服务端合成的 PFX（agent 仅下载导入，不处理证书格式）
	var pfxBytes []byte
	if task.PfxPath != "" && task.PfxData != "" {
		var err error
		pfxBytes, err = base64.StdEncoding.DecodeString(task.PfxData)
		if err != nil {
			submitResult(cfg, client, task.TaskID, false, "", "PFX 数据解码失败: "+err.Error())
			return
		}
		if err := writeFileBytes(task.PfxPath, pfxBytes, 0o600); err != nil {
			submitResult(cfg, client, task.TaskID, false, "", "写入 PFX 失败: "+err.Error())
			return
		}
		// PFX 含私钥：hook 成功或失败都必须清理，避免失败路径残留
		defer func() { _ = os.Remove(task.PfxPath) }()
	}
	// 3. 执行 hook（IIS 场景由中心端配置的导入脚本引用 {pfx 相关}）
	if task.Hook != "" {
		out, err := runCommand(context.Background(), task.Hook, task.IsWindows)
		if err != nil {
			submitResult(cfg, client, task.TaskID, false, "", "hook 执行失败: "+err.Error()+" ("+out+")")
			return
		}
	}
	// 4. 计算校验和并回传（纳入 PFX 内容，IIS 场景 cert/key 可能为空）
	checksum := checksumOf(task.CertificatePEM, task.PrivateKeyPEM, pfxBytes)
	log.Printf("任务 %s 完成", task.TaskID)
	submitResult(cfg, client, task.TaskID, true, checksum, "部署完成")
}

func submitResult(cfg *config, client *http.Client, taskID string, success bool, checksum, message string) {
	body, _ := json.Marshal(resultRequest{
		TaskID: taskID, Success: success, Checksum: checksum, Message: message,
	})
	req, _ := http.NewRequest(http.MethodPost, cfg.ServerURL+"/api/v1/agent/tasks/"+taskID+"/result", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("回传结果失败: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

// writeFile 原子写文件
func writeFile(path string, content string, perm os.FileMode) error {
	return writeFileBytes(path, []byte(content), perm)
}

// writeFileBytes 原子写文件（含二进制，如 PFX；唯一临时名避免并发任务互相覆盖）
func writeFileBytes(path string, data []byte, perm os.FileMode) error {
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
	if _, err := f.Write(data); err != nil {
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

// runCommand 执行 hook（Windows 用 powershell，其余 sh -c），60s 超时
func runCommand(ctx context.Context, cmdLine string, isWindows bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if isWindows || runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", cmdLine)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdLine)
	}
	configureProcessGroup(cmd)
	// 覆盖默认 Cancel：ctx 取消/超时时终止整个进程组（避免孙进程存活）
	cmd.Cancel = func() error {
		killProcessTree(cmd)
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil && ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return text, fmt.Errorf("hook 执行超时（60s）")
		}
		return text, fmt.Errorf("hook 被取消: %w", ctx.Err())
	}
	return text, err
}

func checksumOf(cert, key string, extra []byte) string {
	h := sha256.New()
	h.Write([]byte(cert))
	h.Write([]byte(key))
	h.Write(extra)
	return hex.EncodeToString(h.Sum(nil))
}

// ---------------- 自升级 ----------------

type upgradeInfo struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type upgradeResponse struct {
	Upgrade *upgradeInfo `json:"upgrade"`
}

// checkUpgrade 查询升级指令 → 下载 → 校验 → 替换重启
func checkUpgrade(cfg *config, client *http.Client) error {
	req, _ := http.NewRequest(http.MethodGet, cfg.ServerURL+"/api/v1/agent/upgrade", nil)
	req.Header.Set("X-Agent-Token", cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var ur upgradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		return err
	}
	if ur.Upgrade == nil || ur.Upgrade.Version == "" || ur.Upgrade.Version == version {
		return nil
	}
	log.Printf("收到升级指令: %s", ur.Upgrade.Version)
	return performUpgrade(cfg, client, ur.Upgrade)
}

// performUpgrade 下载新版本 → SHA256 校验 → 原子替换 → 重启
func performUpgrade(cfg *config, client *http.Client, info *upgradeInfo) error {
	dlReq, _ := http.NewRequest(http.MethodGet,
		cfg.ServerURL+"/api/v1/agent/binaries/"+info.Version+"/download", nil)
	dlReq.Header.Set("X-Agent-Token", cfg.Token)
	resp, err := client.Do(dlReq)
	if err != nil {
		return fmt.Errorf("下载新版本失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载新版本 HTTP %d", resp.StatusCode)
	}
	// 大小限制 64MB（防恶意/故障服务端内存耗尽）
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if len(data) >= 64<<20 {
		return fmt.Errorf("新版本二进制超过 64MB 上限，拒绝安装")
	}
	// SHA256 严格校验（fail-closed：缺失哈希拒绝升级，防完整性校验绕过）
	if info.SHA256 == "" {
		return fmt.Errorf("升级指令缺少 SHA256 哈希，拒绝升级")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != info.SHA256 {
		return fmt.Errorf("SHA256 校验失败，拒绝替换")
	}
	// 替换自身二进制
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	newPath := filepath.Join(dir, "argus-agent.new")
	if err := os.WriteFile(newPath, data, 0o755); err != nil {
		return fmt.Errorf("写入新版本失败: %w", err)
	}
	log.Printf("新版本已下载并校验通过，开始替换重启")
	if runtime.GOOS == "windows" {
		// Windows 无法替换运行中的 exe：通过副本重启，由新进程完成替换
		return restartViaCopy(cfg, client, exe, newPath, info.Version)
	}
	// Linux：覆盖替换后 exec 重启
	if err := os.Rename(newPath, exe); err != nil {
		return fmt.Errorf("替换二进制失败: %w", err)
	}
	args := []string{"--config", configPath}
	if err := syscall.Exec(exe, append([]string{exe}, args...), os.Environ()); err != nil {
		return fmt.Errorf("重启失败: %w", err)
	}
	return nil
}

// restartViaCopy Windows：启动新副本（--upgrade-self），新进程等待旧进程退出后替换主文件并重启
func restartViaCopy(cfg *config, client *http.Client, exe, newPath, newVersion string) error {
	launcher := newPath + ".exe"
	if err := os.Rename(newPath, launcher); err != nil {
		data, rerr := os.ReadFile(newPath)
		if rerr != nil {
			return rerr
		}
		if err := os.WriteFile(launcher, data, 0o755); err != nil {
			return err
		}
	}
	cmd := exec.Command(launcher, "--upgrade-self", "--target", exe, "--upgrade-source", launcher, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新副本失败: %w", err)
	}
	log.Printf("已启动新副本（%s），当前进程退出", newVersion)
	os.Exit(0)
	return nil
}

// runUpgradeSelf 新副本进程：等待旧进程退出 → 替换主文件（复制，源为运行中的副本）→ 重启为正常模式
func runUpgradeSelf(target, source string) {
	// Windows 下运行中的 exe 不可重命名/删除，但可读取：
	// 旧进程已在 restartViaCopy 中 os.Exit(0) 退出（目标文件解锁），
	// 此处用复制覆盖目标（源=本副本自身，允许读），轮询重试避免竞态。
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := replaceFile(target, source); err == nil {
			break
		} else if time.Now().After(deadline) {
			log.Printf("替换主文件失败: %v", err)
			os.Exit(1)
		}
		time.Sleep(500 * time.Millisecond)
	}
	cmd := exec.Command(target, "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("重启失败: %v", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// replaceFile 将 src 内容复制到 dst（覆盖），随后尽力删除 src
func replaceFile(dst, src string) error {
	if err := copyFile(dst, src); err != nil {
		return err
	}
	_ = os.Remove(src) // 残留无害
	return nil
}

// copyFile 复制文件内容（不保留权限）
func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
