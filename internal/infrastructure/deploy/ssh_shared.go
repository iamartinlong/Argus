package deploy

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// sshCreds SSH 连接凭据（SSH 与 IIS 适配器共用）
type sshCreds struct {
	Host       string
	Port       int
	User       string
	PrivateKey string
	Password   string
	KnownHosts string
}

// sshDial 建立 SSH 连接（known_hosts 提供时严格校验主机指纹）
func sshDial(creds sshCreds, timeout time.Duration) (*ssh.Client, error) {
	var auth []ssh.AuthMethod
	if creds.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(creds.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if creds.Password != "" {
		auth = append(auth, ssh.Password(creds.Password))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("ssh 目标缺少认证凭据（私钥或密码）")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	port := creds.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(creds.Host, fmt.Sprintf("%d", port))
	callback, err := sshHostKeyCallback(creds.Host, port, creds.KnownHosts)
	if err != nil {
		return nil, err
	}
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            creds.User,
		Auth:            auth,
		HostKeyCallback: callback,
		Timeout:         timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH 连接 %s 失败: %w", addr, err)
	}
	return client, nil
}

// sshHostKeyCallback 主机指纹校验：配置了 known_hosts 时严格校验（防中间人攻击）
func sshHostKeyCallback(host string, port int, knownHostsContent string) (ssh.HostKeyCallback, error) {
	if knownHostsContent == "" {
		// 未配置 known_hosts：不校验指纹，存在中间人风险（生产建议配置）
		return ssh.InsecureIgnoreHostKey(), nil
	}
	tmp, err := os.CreateTemp("", "argus-known-hosts-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(knownHostsContent); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	callback, err := knownhosts.New(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("解析 known_hosts: %w", err)
	}
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// 同时校验 host 与 host:port 两种形式
		var err error
		if e := callback(hostname, remote, key); e == nil {
			return nil
		} else {
			err = e
		}
		if e := callback(addr, remote, key); e == nil {
			return nil
		} else {
			err = e
		}
		return fmt.Errorf("主机指纹校验失败（known_hosts 未包含 %s）: %w", addr, err)
	}, nil
}

// sshPutFile 通过 SFTP 原子写入文本文件
func sshPutFile(client *ssh.Client, remotePath, content, perm string) error {
	return sshPutFileBytes(client, remotePath, []byte(content), perm)
}

// sshPutFileBytes 通过 SFTP 原子写入文件（唯一临时名 + 重命名，避免并发部署互相覆盖）
func sshPutFileBytes(client *ssh.Client, remotePath string, data []byte, perm string) error {
	cli, err := sftp.NewClient(client, sftp.UseConcurrentReads(true))
	if err != nil {
		return err
	}
	defer cli.Close()
	if err := cli.MkdirAll(path.Dir(remotePath)); err != nil {
		return fmt.Errorf("创建远程目录: %w", err)
	}
	suffix, err := randomHex(8)
	if err != nil {
		return fmt.Errorf("生成临时文件名: %w", err)
	}
	tmp := remotePath + ".tmp-" + suffix
	f, err := cli.Create(tmp)
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = cli.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := cli.Chmod(tmp, os.FileMode(parsePerm(perm))); err != nil {
		return fmt.Errorf("设置权限: %w", err)
	}
	if err := cli.PosixRename(tmp, remotePath); err != nil {
		// 部分服务器不支持 PosixRename，退回删除+重命名
		if err2 := cli.Remove(remotePath); err2 == nil {
			if err3 := cli.Rename(tmp, remotePath); err3 != nil {
				return fmt.Errorf("重命名失败: %w", err3)
			}
		} else {
			return fmt.Errorf("重命名失败: %w", err)
		}
	}
	renamed = true
	return nil
}

// sshRemoveRemote 删除远程文件
func sshRemoveRemote(client *ssh.Client, remotePath string) error {
	cli, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.Remove(remotePath)
}

// sshCmdTimeout 远程命令执行超时：到时强制关闭 session，避免远端 hook 挂起
//导致部署记录永久停在 running、该证书后续部署被拒绝。
const sshCmdTimeout = 120 * time.Second

// sshRunRemote 执行远程命令（Windows 用 powershell，其余 sh -c），带超时兜底
func sshRunRemote(client *ssh.Client, command string, isWindows bool) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	cmdLine := command
	if isWindows {
		// UTF-8 前缀避免 GBK 乱码（Windows 中文系统 netsh 等中文输出）
		utf8Cmd := "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; chcp 65001 >$null; " + command
		cmdLine = `powershell -NoProfile -Command "` + strings.ReplaceAll(utf8Cmd, `"`, `\"`) + `"`
	} else {
		cmdLine = "sh -c " + shellQuote(command)
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(sshCmdTimeout, func() {
		timedOut.Store(true)
		session.Close()
	})
	defer timer.Stop()
	out, err := session.CombinedOutput(cmdLine)
	if err != nil && timedOut.Load() {
		return strings.TrimSpace(decodeSSHOutput(out)), fmt.Errorf("远程命令执行超时（%s）: %w", sshCmdTimeout, err)
	}
	return strings.TrimSpace(decodeSSHOutput(out)), err
}

func decodeSSHOutput(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), b)
	if err != nil {
		return string(b)
	}
	return string(decoded)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func parsePerm(p string) uint32 {
	var m uint32
	for _, c := range p {
		switch c {
		case '4', '5', '6', '7':
			m = m*8 + uint32(c-'0')
		}
	}
	return m
}

// randomHex 生成 n 字节随机数的十六进制串（临时文件名后缀）
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// randomPassword 生成 n 位随机密码（IIS PFX 等场景）；随机源失败必须报错，不得降级为固定串
func randomPassword(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// 拒绝采样上界：丢弃落在 len(chars) 非整数倍尾部的字节，消除取模偏置
	const limit = 256 - 256%len(chars)
	out := make([]byte, n)
	var buf [1]byte
	for i := 0; i < n; {
		if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
			return "", fmt.Errorf("生成随机密码: %w", err)
		}
		if int(buf[0]) >= limit {
			continue
		}
		out[i] = chars[int(buf[0])%len(chars)]
		i++
	}
	return string(out), nil
}
