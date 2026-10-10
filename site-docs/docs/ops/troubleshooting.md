# 故障排查

## 服务无法启动（Windows 重点）

| 症状 | 检查 |
|---|---|
| `jwt secret is required` | `Machine` 级 `ARGUS_JWT_SECRET` `len≥32`（`internal/config/config.go:153`），`$env:` 会话级服务读不到 |
| `缺少主密钥` / `master key must be >=32` | `Machine` 级 `ARGUS_MASTER_KEY` `len≥32` 或 `hex/base64` 解码后 32 字节（`internal/infrastructure/crypto/aes.go:52`） |
| `1053 服务没有及时响应启动或控制请求`，`SCM 30000ms 超时` | `<1.0.1` 无 `ServiceMain` 必现，升级 `≥1.0.1`（`cmd/argus/service_windows.go:1` `svc.Run`）；`Machine` 变量改后未 `Restart-Service` |
| `bind: address already in use` | `netstat -ano \| findstr 8443`，端口被 IIS/其他占用，改 `server.listen` |
| 端口监听但页面 500 | 检查数据目录权限、`storage.dsn` 可写；`System32\data` 双库见下 |
| `系统中已存在用户` 但登录 `401` | `storage.dsn` 相对路径 `.\data\argus.db` 导致前台 `D:\argus` 与服务 `C:\Windows\System32` 双库；改绝对路径 `D:/argus/data/argus.db`，`Stop-Service + Remove-Item 错位库 + argus --config D:\argus\config.yaml init-admin` |
| 登录成功但 `PUT /users/me/password ERR_CONNECTION_REFUSED` / 改密后 `STOPPED` | 同 `1053` 假死，`1.0.1` 已修（`PUT 200`，`MustChangePassword` 清零） |

> 前台直跑排障（必做）：`Stop-Service Argus; $env:ARGUS_*=[Environment]::GetEnvironmentVariable(...,"Machine"); & "D:\argus\argus.exe" serve --config "D:\argus\config.yaml"` 首错即根因；`sc qc Argus` 验 `BINARY_PATH_NAME` 引号，`Get-WinEvent System/Provider=Service Control Manager` 验超时。

## 签发失败

- **ACME HTTP-01**：管理服务器 80 端口公网可达？防火墙/NAT 放行？
- **ACME DNS-01**：DNS 凭据正确？域名解析在对应 DNS 服务商？（lego 已验证记录后签发）
- **云厂商"域名验证未完成"**：域名托管在对应云 DNS 且授权？或按提示手动添加 TXT
- 详情页「最近错误」与任务时间线查看具体失败原因

## 自动续期未触发

- 调度器是否启用（`scheduler.enabled`）
- 到期时间是否 < `renew_days_before`（默认 30 天）
- 「任务日志」查看扫描记录；可手动 `POST /api/v1/task-logs/scan` 触发

## 部署失败

- SSH：凭据/known_hosts/目标主机可达性；「测试」按钮验证连接
- Agent：Agent 是否在线（在线状态实时刷新）；任务状态 pending → running → success/failed；
  超时未回执（10 分钟）自动 failed
- IIS：目标必须是 Windows + PowerShell（连接测试会探测）；账号管理员权限
- 部署记录查看校验和与错误消息

## 通知未收到

- 渠道是否在 config.yaml 配置（重启生效）
- 「通知规则」是否启用、事件/天数匹配
- 发送失败会重试 3 次并记录审计（notify.failed）

## Agent 离线

- 检查 agent.json 配置与网络可达性
- 日志：`journalctl -u argus-agent -f`
- Token 失效/被删：重新添加 Agent 获取新 Token

## 忘记管理员密码

无邮件找回流程，用本机 CLI 直连数据库重置（无需 JWT/主密钥，不依赖已加密数据）：

```powershell
# 1.（推荐）先停服务，避免写锁竞争；在线亦可（WAL + busy_timeout 5s）
Stop-Service Argus

# 2. 重置密码（显式指定）
& "D:\argus\argus.exe" --config "D:\argus\config.yaml" reset-password --username admin --password "12位+强口令"

# 或不传密码：随机生成（首次登录强制修改）
& "D:\argus\argus.exe" --config "D:\argus\config.yaml" reset-password --username admin

# 3. 启动服务，用新密码登录
Start-Service Argus
```

要点：

- 用户不存在时报错并提示改用安装向导（`/setup`）或 `init-admin`
- 密码策略与 Web 一致（默认 ≥12 位，含大小写字母与数字，`auth.min_password_len`）
- 该用户全部登录会话立即吊销；已签发访问令牌最长 `auth.access_token_ttl`（默认 15m）内自然过期
- 随机生成的密码会置 `MustChangePassword`，首次登录强制改密
- 操作记入审计日志（`password.reset`）

## 数据库问题

- 迁移自动执行（schema_migrations 表）；异常可检查 `data/argus.db` 完整性
- 备份/恢复参见运维指南
