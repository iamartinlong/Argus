# Argus 管理后台 Windows 服务 安装/卸载脚本（需管理员权限）
# 用法：
#   powershell -ExecutionPolicy Bypass -File install-service.ps1               # 安装并启动，成功后自动打开网站
#   powershell -ExecutionPolicy Bypass -File install-service.ps1 -NoBrowser    # 安装并启动，不自动打开浏览器
#   powershell -ExecutionPolicy Bypass -File install-service.ps1 -Uninstall     # 卸载
#
# 默认 ExePath/ConfigPath 取脚本所在目录下的 argus(.exe) 与 config.yaml，
# 将脚本与二进制、配置放在同一目录即可一键服务化。
# 访问地址默认 http://localhost:8443（从 config.yaml server.listen 解析端口，可用 -Url 覆盖）

param(
    [string]$ExePath = "",
    [string]$ConfigPath = "",
    [string]$Url = "",
    [switch]$Uninstall,
    [switch]$NoBrowser
)

$ErrorActionPreference = "Stop"
$serviceName = "Argus"

# 管理员权限检查（net session 仅管理员可调用，非管理员返回错误 5）
cmd /c "net session >nul 2>&1"
if ($LASTEXITCODE -ne 0) {
    Write-Host "需要管理员权限运行。请以管理员身份打开 PowerShell 后重试。" -ForegroundColor Red
    exit 1
}

if (-not $ExePath) {
    $ExePath = Join-Path $PSScriptRoot "argus.exe"
}
if (-not $ConfigPath) {
    $ConfigPath = Join-Path $PSScriptRoot "config.yaml"
}

if ($Uninstall) {
    Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue
    sc.exe delete $serviceName | Out-Null
    Write-Host "Argus 服务已卸载"
    exit 0
}

if (-not (Test-Path $ExePath)) {
    Write-Error "未找到二进制: $ExePath"
    exit 1
}
if (-not (Test-Path $ConfigPath)) {
    Write-Error "未找到配置文件: $ConfigPath"
    exit 1
}

# sc.exe binPath 引号转义：内部路径引号必须以 \" 形式传递。
# 若直接传 `"..."`（裸双引号），PowerShell 拼接原生命令行时不会转义内部引号，
# 路径含空格时 binPath 会被 CommandLineToArgvW 拆坏。
$binPath = '\"{0}\" serve --config \"{1}\"' -f $ExePath, $ConfigPath

if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    sc.exe config $serviceName binPath= $binPath
    Write-Host "Argus 服务已更新"
} else {
    sc.exe create $serviceName binPath= $binPath start= auto DisplayName= "Argus SSL Certificate Manager" | Out-Null
    Write-Host "Argus 服务已创建"
}

# 崩溃自动重启：首次失败 5s 后重启，后续 60s 间隔，24h 后重置计数
sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/60000/restart/60000 | Out-Null

Start-Service -Name $serviceName
Write-Host "Argus 服务已启动（开机自启、崩溃自动重启）"
Write-Host "配置: $ConfigPath"

# 访问地址：-Url 优先，否则从 config.yaml server.listen 解析端口
if (-not $Url) {
    $port = 8443
    $m = Select-String -Path $ConfigPath -Pattern 'listen:\s*["'']?([^"'']+)["'']?' | Select-Object -First 1
    if ($m -and $m.Matches[1].Value -match ':(\d+)\s*$') { $port = $Matches[1] }
    $Url = "http://localhost:$port"
}
$Url = $Url.TrimEnd('/')

# 等待服务就绪（最多 30s），就绪后再打开浏览器，避免首开连接被拒
$healthy = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $resp = Invoke-WebRequest -Uri "$Url/healthz" -UseBasicParsing -TimeoutSec 2
        if ($resp.StatusCode -eq 200) { $healthy = $true; break }
    } catch {}
    Start-Sleep -Seconds 1
}

if ($NoBrowser) {
    Write-Host "访问 $Url"
} elseif ($healthy) {
    Write-Host "服务就绪，正在打开 $Url ..."
    Start-Process $Url
} else {
    Write-Host "服务 30s 内未就绪，仍为你打开页面（稍后可手动刷新）：$Url" -ForegroundColor Yellow
    Start-Process $Url
}
