# Windows 部署

## 方式一：Windows 服务（推荐，一键脚本）

以管理员身份运行 PowerShell：

```powershell
# 一键安装：脚本与 argus.exe、config.yaml 放同一目录即可（自动定位 + 失败自动重启）
powershell -ExecutionPolicy Bypass -File deploy\windows\install-service.ps1

# 自定义路径
powershell -ExecutionPolicy Bypass -File deploy\windows\install-service.ps1 `
  -ExePath "D:\apps\argus\argus.exe" `
  -ConfigPath "D:\apps\argus\config.yaml"

# 卸载
powershell -ExecutionPolicy Bypass -File deploy\windows\install-service.ps1 -Uninstall
```

脚本自动完成：管理员权限检查 → 创建服务（`Argus`，开机自启）→ 失败重启策略（5s/60s/60s）→ 启动。
服务名 `Argus`，访问 `http://localhost:8443`，首次打开自动进入安装向导（`≥1.1.0`，密钥+管理员可视化配置，无需手动 `init-admin`）。

## 方式二：前台运行（排障）

```powershell
# ≥1.1.0 可跳过本节：直接 serve，浏览器打开安装向导可视化配置
# 注意：PowerShell 不在当前目录查找可执行文件，需用 .\ 前缀或完整路径
.\argus.exe serve --config "D:\argus\config.yaml"  # 前台直跑可立即看到首错（比服务日志更直接）

# 命令行备选（沿用旧流程亦可）
$env:ARGUS_JWT_SECRET = "..."  # ≥32 字符，internal/config/config.go:153
$env:ARGUS_MASTER_KEY = "..."  # ≥32 字符，原文/hex/base64 解码后 32 字节，internal/infrastructure/crypto/aes.go:35
.\argus.exe --config "D:\argus\config.yaml" init-admin --username admin --password "NewPass1234"
```

## 前置与配置要点

- **系统**：Windows Server 2016+（IIS 10 验证通过），`argus.exe` 为 Go 静态二进制无 .NET 依赖
- **权限**：安装/启停服务需**管理员 PowerShell**（脚本内 `net session` 校验，`deploy/windows/install-service.ps1:19`）
- **PowerShell 执行**：PowerShell 默认**不在当前目录**查找可执行文件，直接输入 `argus.exe` 会报找不到命令；需写 `.\argus.exe`（先 `cd` 到所在目录）或完整路径 `& "D:\argus\argus.exe"`（`cmd.exe` 无此限制）
- **config.yaml**：从 `config.yaml.example` 复制后必改 `storage.dsn` 为**绝对路径**（如 `D:/argus/data/argus.db`），相对路径 `.\data\argus.db` 会导致服务（`cwd=C:\Windows\System32`）与前台（`cwd=D:\argus`）双库，出现“已创建用户但登录 401”；`server.listen: ":8443"` 保持
- **环境变量**：`ARGUS_JWT_SECRET`/`ARGUS_MASTER_KEY` 须设为 **Machine 级**（`[Environment]::SetEnvironmentVariable(...,"Machine")`），`User` 级或会话级 `$env:` 服务进程（`LocalSystem`）读不到，报 `jwt secret is required` `cmd/argus/cmd/serve.go:360`
- **本机 IIS 同机**：`Argus` 服务须以 `LocalSystem` 运行才有 `Import-PfxCertificate`/`netsh` 管理员权限（`site-docs/docs/deploy/deploy-targets/iis.md:52`）

## 版本差异

- `≥1.1.0`：可视化安装向导（`/setup`）：缺密钥时 `serve` 降级为安装模式而非报错退出；密钥一键生成并写回 `config.yaml`（自动备份 `.bak`）；管理员界面创建；安装模式一键重启进完整模式，服务模式降级为 `Restart-Service` 指引
- `≥1.0.1`：已实现原生 `Windows Service Control Handler`（`cmd/argus/service_windows.go:1` `svc.IsWindowsService + svc.Run`），`sc create Argus` 直装即 `RUNNING`，无需 NSSM/WinSW
- `<1.0.1`：控制台程序直装必 `1053 服务没有及时响应启动或控制请求`，需 `NSSM` 包装：`nssm install Argus "D:\argus\argus.exe" "serve --config D:\argus\config.yaml"` + `nssm set Argus AppDirectory D:\argus`

> 从旧版本升级的完整步骤（含备份、回滚、验证清单）见[升级指南](../upgrade.md)。

## 注意事项

- 数据目录建议非系统盘并加入备份；`argus.db` 与 `ARGUS_MASTER_KEY` 同等保密（丢则已加密凭据无法解密）
- 生产经 IIS 反代 HTTPS 后改 `auth.cookie_secure: true`
- Windows 上 Agent（业务机侧）直接运行 `argus-agent.exe`，无需服务化也可

## 常见坑位速查（Windows）

| 症状 | 根因 | 处置 |
|---|---|---|
| `1053 服务没有及时响应启动或控制请求`，`sc query STOPPED`，`netstat` 无 `8443` | 旧版无 ServiceMain 或 Machine 环境变量缺失 | 升级 `≥1.0.1`；`Machine` 级 `ARGUS_*` 后 `Restart-Service Argus` |
| `jwt secret is required` / `缺少主密钥` | 仅设 `$env:` 会话级或长度 `<32` | `Machine` 级重设，`len≥32`（`internal/config/config.go:153` / `aes.go:52`） |
| `系统中已存在用户` 但登录 `401` | `dsn` 相对路径双库 | 改绝对路径，`Stop-Service + Remove-Item 错位库 + --config` 显式重建 |
| 登录后 `PUT /users/me/password ERR_CONNECTION_REFUSED` 或改密后服务 `STOPPED` | `<1.0.1` 旧版服务 `1053` 假死，非改密接口问题 | 升级 `1.0.1` 后 `PUT 200`，首改密 `MustChangePassword` 自动清零 |
| 忘记 admin 密码 | 无邮件找回 | `Stop-Service Argus` 后 `.\argus.exe --config D:\argus\config.yaml reset-password --username admin --password "NewPass1234"`（无需密钥，会话全吊销），详见[故障排查](../../ops/troubleshooting.md) |
| 输入 `argus.exe` / `argus` 提示“无法识别为 cmdlet” | PowerShell 默认不在当前目录查找可执行文件（`cmd.exe` 无此限制） | 使用 `.\argus.exe`（先 `cd` 到目录）或完整路径 `& "D:\argus\argus.exe"` |
| `bind: address already in use :8443` | 端口被 IIS/其他占用 | `netstat -ano \| findstr 8443` 改 `server.listen` |
