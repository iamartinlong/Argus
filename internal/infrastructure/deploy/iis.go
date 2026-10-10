package deploy

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// IIS 场景部署（SSH/本机/Agent 通道共用）：Windows Server IIS 证书部署。
// 流程：服务端合成 PFX → 传输 → PowerShell 导入证书 + 更新 https 绑定 → 删除远端 PFX。

// deployIISOverSSH 推送 PFX 并执行内置 IIS 绑定更新脚本（SSH 兼容模式与 iis 适配器共用）
func deployIISOverSSH(client *ssh.Client, hostLabel, pfxPath string, bindings []int, siteName string, payload Payload) (string, error) {
	return deployIISOverSSHWithHostname(client, hostLabel, pfxPath, bindings, siteName, payload.PrimaryDomain, payload)
}

// deployIISOverSSHWithHostname 支持显式 hostname（多站点 SNI），空则回退 PrimaryDomain
func deployIISOverSSHWithHostname(client *ssh.Client, hostLabel, pfxPath string, bindings []int, siteName, hostname string, payload Payload) (string, error) {
	if hostname == "" {
		hostname = payload.PrimaryDomain
	}
	pfxPassword, err := randomPassword(16)
	if err != nil {
		return "", err
	}
	pfxData, err := BuildPFX(payload.CertificatePEM, payload.PrivateKeyPEM, pfxPassword)
	if err != nil {
		return "", fmt.Errorf("合成 PFX: %w", err)
	}
	if err := sshPutFileBytes(client, pfxPath, pfxData, "0600"); err != nil {
		return "", fmt.Errorf("推送 PFX: %w", err)
	}
	// PFX 含私钥：脚本失败（未走到内置删除）时也必须清理远端残留
	defer func() { _ = sshRemoveRemote(client, pfxPath) }()
	script := RenderIISScriptV2WithHostname(pfxPath, pfxPassword, bindings, siteName, hostname)
	out, err := sshRunRemote(client, script, true)
	if err != nil {
		return "", fmt.Errorf("IIS 绑定更新失败: %w (%s)", err, out)
	}
	msg := "IIS https 绑定已更新（" + out + "）"
	if siteName != "" {
		msg += "；站点 " + siteName + " 绑定已同步"
	}
	return msg, nil
}

// IISHookScriptV2 内置 IIS 部署脚本（占位符：{pfx_path} {pfx_password} {iis_bindings} {site_binding_section} {hostname}）
// 流程：导入 PFX 到 LocalMachine\My → netsh 更新 HTTP.sys https 绑定（SNI 时用 hostnameport，单站点无主机名时回退 ipport）
//      → 可选 IIS 站点绑定更新（WebAdministration，site_name 非空时启用，含 HostHeader+SNI）→ 删除远端 PFX
const IISHookScriptV2 = `$ErrorActionPreference = 'Stop'
$pfxPath = '{pfx_path}'
$pfxPass = '{pfx_password}'
$cert = Import-PfxCertificate -FilePath $pfxPath -CertStoreLocation Cert:\LocalMachine\My -Password (ConvertTo-SecureString $pfxPass -AsPlainText -Force) -Exportable
$thumb = $cert.Thumbprint
$appId = '{00000000-0000-0000-0000-000000000000}'
$hostname = '{hostname}'
foreach ($port in @({iis_bindings})) {
    if ($hostname) {
        $hport = "$($hostname):$port"
        netsh http show sslcert hostnameport=$hport 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { netsh http delete sslcert hostnameport=$hport }
        netsh http add sslcert hostnameport=$hport certhash=$thumb appid='{00000000-0000-0000-0000-000000000000}' certstorename=MY
        if ($LASTEXITCODE -ne 0) { throw "netsh add sslcert hostnameport failed for $hport" }
    } else {
        netsh http show sslcert ipport=0.0.0.0:$port 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { netsh http delete sslcert ipport=0.0.0.0:$port }
        netsh http add sslcert ipport=0.0.0.0:$port certhash=$thumb appid='{00000000-0000-0000-0000-000000000000}'
        if ($LASTEXITCODE -ne 0) { throw "netsh add sslcert failed for port $port" }
    }
}
{site_binding_section}
Remove-Item $pfxPath -Force -ErrorAction SilentlyContinue
Write-Output "IIS https bindings updated with cert $thumb"`

// IISSiteBindingSection IIS 站点绑定更新（WebAdministration，IIS 7+，支持 SNI）
// hostname 非空时以 HostHeader+SNI（SslFlags 1）创建，否则回退为通配绑定（单站点兼容）
const IISSiteBindingSection = `Import-Module WebAdministration -ErrorAction SilentlyContinue
$hostname = '{hostname}'
foreach ($port in @({iis_bindings})) {
    if ($hostname) {
        $binding = Get-WebBinding -Name '{site_name}' -Protocol "https" -Port $port -HostHeader $hostname -ErrorAction SilentlyContinue
        if ($binding) {
            $binding.AddSslCertificate($thumb, "My")
        } else {
            New-WebBinding -Name '{site_name}' -Protocol "https" -Port $port -HostHeader $hostname -SslFlags 1 -ErrorAction Stop
            $nb = Get-WebBinding -Name '{site_name}' -Protocol "https" -Port $port -HostHeader $hostname -ErrorAction Stop
            $nb.AddSslCertificate($thumb, "My")
        }
    } else {
        $binding = Get-WebBinding -Name '{site_name}' -Protocol "https" -Port $port -ErrorAction SilentlyContinue
        if ($binding) {
            $binding.AddSslCertificate($thumb, "My")
        } else {
            New-WebBinding -Name '{site_name}' -Protocol "https" -Port $port -ErrorAction Stop
            $nb = Get-WebBinding -Name '{site_name}' -Protocol "https" -Port $port -ErrorAction Stop
            $nb.AddSslCertificate($thumb, "My")
        }
    }
}`

// escapePSSingleQuote 转义 PowerShell 单引号字符串（单引号内 '' 表示一个单引号）
func escapePSSingleQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

// RenderIISScriptV2 渲染内置 IIS 部署脚本（v2）
func RenderIISScriptV2(pfxPath, pfxPassword string, bindings []int, siteName string) string {
	return RenderIISScriptV2WithHostname(pfxPath, pfxPassword, bindings, siteName, "")
}

// RenderIISScriptV2WithHostname 支持显式 hostname（多站点 SNI），空则回退 PrimaryDomain/通配
func RenderIISScriptV2WithHostname(pfxPath, pfxPassword string, bindings []int, siteName, hostname string) string {
	ports := make([]string, 0, len(bindings))
	for _, p := range bindings {
		ports = append(ports, fmt.Sprintf("%d", p))
	}
	siteSection := ""
	if siteName != "" {
		siteSection = strings.ReplaceAll(IISSiteBindingSection, "{site_name}", escapePSSingleQuote(siteName))
		siteSection = strings.ReplaceAll(siteSection, "{iis_bindings}", strings.Join(ports, ", "))
		siteSection = strings.ReplaceAll(siteSection, "{hostname}", escapePSSingleQuote(hostname))
	}
	r := strings.NewReplacer(
		"{pfx_path}", escapePSSingleQuote(pfxPath),
		"{pfx_password}", escapePSSingleQuote(pfxPassword),
		"{iis_bindings}", strings.Join(ports, ", "),
		"{hostname}", escapePSSingleQuote(hostname),
		"{site_binding_section}", siteSection,
	)
	return r.Replace(IISHookScriptV2)
}

// RenderIISScript 兼容旧版（无站点绑定）
func RenderIISScript(pfxPath, pfxPassword string, bindings []int) string {
	return RenderIISScriptV2(pfxPath, pfxPassword, bindings, "")
}

// IISSiteBindingSectionAppCmd IIS 站点绑定更新（appcmd，无需 WebAdministration 模块，IIS 7+，支持 SNI）
// 语法：appcmd set site "Name" /+bindings.[protocol='https',bindingInformation='*:443:hostname',sslFlags='1']
// hostname 为空时回退为通配（单站点），非空时 SNI；证书与端口关联由 netsh hostnameport/ipport 完成
const IISSiteBindingSectionAppCmd = `$appcmd = Join-Path $env:windir 'system32\inetsrv\appcmd.exe'
foreach ($port in @({iis_bindings})) {
    $h = '{hostname}'
    # 幂等：绑定已存在时先删除再重建（appcmd 重复添加相同 binding 会报错）
    # 注意：$port 后紧跟冒号会被 PowerShell 当作作用域前缀，必须用 ${port} 包裹
    $bindingInfo = if ($h) { "*:${port}:$h" } else { "*:${port}:" }
    $listPat = if ($h) { "https/*:${port}:$h" } else { "https/*:${port}:" }
    $existing = & $appcmd list binding "site/{site_name}" 2>$null | Select-String $listPat
    if ($existing) {
        $delArg = if ($h) { "/-bindings.[protocol='https',bindingInformation='*:${port}:$h',sslFlags='1']" } else { "/-bindings.[protocol='https',bindingInformation='*:${port}:']" }
        & $appcmd set site '{site_name}' $delArg 2>&1 | Out-Null
    }
    $arg = if ($h) { "/+bindings.[protocol='https',bindingInformation='*:${port}:$h',sslFlags='1']" } else { "/+bindings.[protocol='https',bindingInformation='*:${port}:']" }
    $appOut = & $appcmd set site '{site_name}' $arg 2>&1
    if ($LASTEXITCODE -ne 0) { throw "appcmd 添加 https 绑定失败（site={site_name} port=$port hostname=$h）: $appOut" }
    Write-Output ("appcmd site bind: " + ($appOut -join ' '))
}`

// IISHookScriptAgentV2 agent 场景内置 IIS 部署脚本（占位符：{pfx_path} {pfx_password} {iis_bindings} {site_binding_section} {hostname}）
// 流程：导入 PFX 到 LocalMachine\My → netsh 更新 HTTP.sys https 绑定（SNI 用 hostnameport，否则 ipport） → 可选 appcmd 站点绑定（SNI） → 删除远端 PFX
// 与 IISHookScriptV2 区别：站点绑定用 appcmd（免 WebAdministration），供 agent 通道自动生成。
const IISHookScriptAgentV2 = `$ErrorActionPreference = 'Stop'
$pfxPath = '{pfx_path}'
$pfxPass = '{pfx_password}'
$cert = Import-PfxCertificate -FilePath $pfxPath -CertStoreLocation Cert:\LocalMachine\My -Password (ConvertTo-SecureString $pfxPass -AsPlainText -Force) -Exportable
$thumb = $cert.Thumbprint
$appId = '{00000000-0000-0000-0000-000000000000}'
$hostname = '{hostname}'
foreach ($port in @({iis_bindings})) {
    if ($hostname) {
        $hport = "$($hostname):$port"
        netsh http show sslcert hostnameport=$hport 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { netsh http delete sslcert hostnameport=$hport }
        netsh http add sslcert hostnameport=$hport certhash=$thumb appid='{00000000-0000-0000-0000-000000000000}' certstorename=MY
        if ($LASTEXITCODE -ne 0) { throw "netsh add sslcert hostnameport failed for $hport" }
    } else {
        netsh http show sslcert ipport=0.0.0.0:$port 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { netsh http delete sslcert ipport=0.0.0.0:$port }
        netsh http add sslcert ipport=0.0.0.0:$port certhash=$thumb appid='{00000000-0000-0000-0000-000000000000}'
        if ($LASTEXITCODE -ne 0) { throw "netsh add sslcert failed for port $port" }
    }
}
{site_binding_section}
Remove-Item $pfxPath -Force -ErrorAction SilentlyContinue
Write-Output "IIS https bindings updated with cert $thumb"`

// RenderIISAgentScriptV2 渲染 agent 场景内置 IIS 部署脚本（appcmd 站点绑定）
// siteName 为空时仅做证书导入与 netsh 端口绑定；hostname 用于站点绑定 HostHeader。
func RenderIISAgentScriptV2(pfxPath, pfxPassword string, bindings []int, siteName, hostname string) string {
	ports := make([]string, 0, len(bindings))
	for _, p := range bindings {
		ports = append(ports, fmt.Sprintf("%d", p))
	}
	siteSection := ""
	if siteName != "" {
		siteSection = strings.ReplaceAll(IISSiteBindingSectionAppCmd, "{site_name}", escapePSSingleQuote(siteName))
		siteSection = strings.ReplaceAll(siteSection, "{iis_bindings}", strings.Join(ports, ", "))
		siteSection = strings.ReplaceAll(siteSection, "{hostname}", escapePSSingleQuote(hostname))
	}
	r := strings.NewReplacer(
		"{pfx_path}", escapePSSingleQuote(pfxPath),
		"{pfx_password}", escapePSSingleQuote(pfxPassword),
		"{iis_bindings}", strings.Join(ports, ", "),
		"{site_binding_section}", siteSection,
		"{hostname}", escapePSSingleQuote(hostname),
	)
	return r.Replace(IISHookScriptAgentV2)
}

// BuildPFX 将证书链 + 私钥合成为 PKCS#12（.pfx）
func BuildPFX(certPEM, keyPEM, password string) ([]byte, error) {
	certs, err := parseCertChain(certPEM)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("证书链为空")
	}
	key, err := parseAnyPrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	caCerts := certs[1:]
	pfx, err := pkcs12.Modern.WithRand(rand.Reader).Encode(key, certs[0], caCerts, password)
	if err != nil {
		return nil, fmt.Errorf("合成 PFX: %w", err)
	}
	return pfx, nil
}

func parseCertChain(certPEM string) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := []byte(certPEM)
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

func parseAnyPrivateKey(keyPEM string) (any, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, fmt.Errorf("无效的私钥 PEM")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("无法解析私钥格式")
}
