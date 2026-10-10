# Argus dev 启动脚本（本地开发用）——显式环境变量，不依赖 shell 继承
$p = New-Object System.Diagnostics.Process
$p.StartInfo.FileName = "D:\opencode\SuperCert\build\argus.exe"
$p.StartInfo.Arguments = "serve"
$p.StartInfo.WorkingDirectory = "D:\opencode\SuperCert"
$p.StartInfo.UseShellExecute = $false
$p.StartInfo.CreateNoWindow = $true
$p.StartInfo.EnvironmentVariables["ARGUS_JWT_SECRET"] = "dev-jwt-secret-0123456789abcdef0123456789abcdef"
$p.StartInfo.EnvironmentVariables["ARGUS_MASTER_KEY"] = "dev-master-key-0123456789abcdef0123456789abcdef"
[void]$p.Start()
Start-Sleep -Seconds 3
$proc = Get-Process argus -ErrorAction SilentlyContinue
if ($proc) { Write-Host "ARGUS_RUNNING PID=$($proc.Id -join ',')" } else { Write-Host "ARGUS_NOT_RUNNING" }