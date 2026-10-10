# Argus Agent Windows 服务 安装/卸载脚本（需管理员权限）
# 用法：
#   powershell -ExecutionPolicy Bypass -File install-windows-service.ps1            # 安装并启动
#   powershell -ExecutionPolicy Bypass -File install-windows-service.ps1 -Uninstall  # 卸载
#
# 默认 ExePath/ConfigPath 取脚本所在目录下的 argus-agent(.exe) 与 agent.json，
# 将脚本与二进制、配置放在同一目录即可一键服务化。

param(
    [string]$ExePath = "",
    [string]$ConfigPath = "",
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$serviceName = "ArgusAgent"

# 管理员权限检查（net session 仅管理员可调用，非管理员返回错误 5）
cmd /c "net session >nul 2>&1"
if ($LASTEXITCODE -ne 0) {
    Write-Host "需要管理员权限运行。请以管理员身份打开 PowerShell 后重试。" -ForegroundColor Red
    exit 1
}

if (-not $ExePath) {
    $ExePath = Join-Path $PSScriptRoot "argus-agent.exe"
}
if (-not $ConfigPath) {
    $ConfigPath = Join-Path $PSScriptRoot "agent.json"
}

if ($Uninstall) {
    Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue
    sc.exe delete $serviceName | Out-Null
    Write-Host "ArgusAgent 服务已卸载"
    exit 0
}

if (-not (Test-Path $ExePath)) {
    Write-Error "未找到 Agent 二进制: $ExePath"
    exit 1
}
if (-not (Test-Path $ConfigPath)) {
    Write-Error "未找到配置文件: $ConfigPath"
    exit 1
}

# sc.exe binPath 引号转义：内部路径引号必须以 \" 形式传递。
# 若直接传 `"..."`（裸双引号），PowerShell 拼接原生命令行时不会转义内部引号，
# 路径含空格时 binPath 会被 CommandLineToArgvW 拆坏。
$binPath = '\"{0}\" --config \"{1}\"' -f $ExePath, $ConfigPath

if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    sc.exe config $serviceName binPath= $binPath
    Write-Host "ArgusAgent 服务已更新"
} else {
    sc.exe create $serviceName binPath= $binPath start= auto DisplayName= "Argus Agent" | Out-Null
    Write-Host "ArgusAgent 服务已创建"
}

# 崩溃自动重启：首次失败 5s 后重启，后续 60s 间隔，24h 后重置计数
sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/60000/restart/60000 | Out-Null

Start-Service -Name $serviceName
Write-Host "ArgusAgent 服务已启动（开机自启、崩溃自动重启）"
Write-Host "配置: $ConfigPath"
