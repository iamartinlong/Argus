# Argus Agent 部署指南

Agent 用于无法通过 SSH 直连的业务机（NAT 环境、无入站端口、Windows 服务器）。
Agent **出站连接**中心端轮询部署任务，执行本地写文件与 reload。

## 1. 中心端添加 Agent

Web 后台「Agent 管理」→ 添加 Agent → 复制生成的 Token（仅显示一次）。

## 2. 业务机配置

创建 `agent.json`：

```json
{
  "server_url": "https://cert.example.com",
  "token": "ag_xxxxxxxxxxxxxxxxxxxxxxxx",
  "poll_interval": 10
}
```

- `server_url`：中心端地址。**生产环境必须使用 HTTPS**（`https://`），
  证书与私钥经 TLS 加密传输；如需双向认证（mTLS），可在中心端前部署
  反向代理（如 Nginx/网关）强制客户端证书，Agent 无需额外配置。
- `token`：Agent 认证令牌（sha256 哈希存储于中心端）。
- `poll_interval`：任务轮询间隔（秒），默认 10。

启动：

```bash
# Linux（建议 systemd，见下）
argus-agent --config agent.json

# Windows
argus-agent.exe --config agent.json
```

## 3. 生产机安装配置清单

部署 Agent 到生产机前的完整检查项：

- [ ] **二进制**：`argus-agent`（Windows 为 `.exe`），Go 单文件约 8.8MB，拷贝到固定目录（如 Linux `/usr/local/bin/`、Windows `C:\argus-agent\`）
- [ ] **配置** `agent.json`（目录权限收紧）：`server_url` **必须 HTTPS**、`token` 中心端生成、`poll_interval` 建议 5-10s
- [ ] **Token 安全**：`agent.json` Linux `chmod 600`；Token 泄漏时中心端删除并重建（旧 Token 立即失效）
- [ ] **出站连通**：确认业务机可访问中心端 `server_url`（仅需出站 443；NAT/防火墙无需入站端口）
- [ ] **管理员权限**（Windows IIS 场景）：Agent 进程需以管理员运行，否则无法导入 `LocalMachine\My` 证书与 netsh/appcmd 绑定
- [ ] **常驻**：Windows 一键脚本 `install-windows-service.ps1`、Linux 一键脚本 `install-systemd.sh`（见 §6/§7）；`Restart=on-failure`
- [ ] **同步时钟**：Agent/中心端时间偏差过大可能影响心跳超时判定（90s 无心跳置离线）
- [ ] **验证在线**：中心端「Agent 管理」确认状态 `在线`、心跳刷新

## 4. 安全要求

- `agent.json` 权限收紧：Linux `chmod 600 agent.json`（令牌明文存放）。
- 生产环境启用 HTTPS，避免证书私钥明文传输。
- Token 泄漏时：在中心端删除 Agent 并重新创建（旧 Token 立即失效）。
- Agent 回执校验：仅任务归属的 Agent 可回传结果（跨 Agent 回执返回 403）。

## 5. 部署目标绑定

Web 后台「部署目标」→ 新建目标 → 类型选「Agent」→ 选择 Agent、
填写证书/私钥目标路径与部署后命令（如 `nginx -s reload` 或 IIS 绑定更新 PowerShell）。
证书签发/续期成功后自动排队任务，Agent 拉取执行并回传结果，
部署记录显示最终执行结果与校验和。

### IIS 场景（Agent 通道，自动绑定）

配置 Agent 目标的 **PFX 路径 + HTTPS 端口 + IIS 站点名** 后，
中心端**自动生成完整 IIS 部署脚本**（无需手写 hook）：

| 配置 | 说明 |
|---|---|
| PFX 路径 | 服务端合成 PFX（随机密码）随任务附带（base64），Agent 写盘（0600）→ 导入 → 删除 |
| HTTPS 端口 | 逗号分隔（如 `443`），netsh 更新 HTTP.sys https 绑定 + appcmd 站点绑定 |
| IIS 站点名 | 可选；填写后 appcmd 为该站点添加 https 绑定（`*:端口:主域名`） |

自动流程：Agent 下载 PFX → `Import-PfxCertificate` 导入 `LocalMachine\My` →
`netsh http add sslcert` 绑定端口 → `appcmd set site` 添加站点 https 绑定 → 删除 PFX。
无需 `WebAdministration` 模块（用 IIS 自带 `appcmd.exe`），需 Agent 以**管理员权限**运行。

> 手写 hook（高级）：不填 HTTPS 端口时保留自定义 hook，支持模板变量
> `{pfx_path} {pfx_password} {cert_path} {key_path} {domain}`，IIS 导入脚本可引用动态 PFX 密码：
>
> ```powershell
> Import-PfxCertificate -FilePath '{pfx_path}' -CertStoreLocation Cert:\LocalMachine\My -Password (ConvertTo-SecureString '{pfx_password}' -AsPlainText -Force) -Exportable
> netsh http add sslcert ipport=0.0.0.0:443 certhash=$thumb appid='{00000000-0000-0000-0000-000000000000}'
> ```
>
> （iis 目标类型 SSH 通道也已内置导入与绑定脚本，见 deploy/iis/README.md）

## 6. Linux systemd 服务化

### 一键脚本（推荐）

仓库提供 `deploy/agent/install-systemd.sh`，将脚本与 `argus-agent`、
`agent.json` 放在同一目录，以 root 执行：

```bash
# 安装并启动（二进制复制到 /usr/local/bin，配置到 /etc/argus-agent/，开机自启、崩溃自动重启）
sudo ./install-systemd.sh

# 卸载
sudo ./install-systemd.sh --uninstall
```

### 手动配置

标准 systemd 单元文件（`deploy/agent/argus-agent.service`）：

```ini
[Unit]
Description=Argus Agent
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/argus-agent --config /etc/argus-agent/agent.json
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo cp deploy/agent/argus-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now argus-agent
```

## 7. Windows 服务化（推荐，开机自启）

直接以控制台方式运行 `argus-agent.exe` 会随重启/注销失效，
生产环境应注册为 **Windows 服务**（开机自动启动、崩溃自动重启、无需登录）。

### 方式 A：一键脚本（推荐）

仓库提供 `deploy/agent/install-windows-service.ps1`，将脚本与 `argus-agent.exe`、
`agent.json` 放在同一目录，以**管理员** PowerShell 执行：

```powershell
# 安装并启动（服务名 ArgusAgent，开机自启、崩溃自动重启）
powershell -ExecutionPolicy Bypass -File install-windows-service.ps1

# 卸载
powershell -ExecutionPolicy Bypass -File install-windows-service.ps1 -Uninstall
```

脚本会自动完成：创建服务 → 设置失败重启策略（5s/60s/60s）→ 启动服务。
也可通过 `-ExePath`/`-ConfigPath` 指定路径：

```powershell
powershell -ExecutionPolicy Bypass -File install-windows-service.ps1 -ExePath "C:\argus-agent\argus-agent.exe" -ConfigPath "C:\argus-agent\agent.json"
```

### 方式 B：sc.exe 手动创建原生服务

以**管理员** PowerShell 执行：

```powershell
# 1. 创建服务（binPath 中引号需用 \" 转义）
sc.exe create ArgusAgent binPath= "\"C:\argus-agent\argus-agent.exe\" --config \"C:\argus-agent\agent.json\"" start= auto DisplayName= "Argus Agent"

# 2. 设置崩溃自动重启（首次失败 5s 后重启，之后 60s 间隔）
sc.exe failure ArgusAgent reset= 86400 actions= restart/5000/restart/60000/restart/60000

# 3. 启动服务
sc.exe start ArgusAgent
```

验证：

```powershell
sc.exe query ArgusAgent        # STATE: RUNNING
Get-Service ArgusAgent         # Status: Running
Get-Process argus-agent        # 进程存在
```

> 注意：
> - `sc.exe create` 的 `binPath=`、`start=` 等参数 `=` 后必须有空格（语法要求）
> - 服务默认以 **LocalSystem** 运行，天然具备管理员权限（IIS 证书导入/netsh/appcmd 绑定可用）
> - 修改配置后需重启服务：`sc.exe stop ArgusAgent; sc.exe start ArgusAgent`
> - 卸载：`sc.exe stop ArgusAgent; sc.exe delete ArgusAgent`
> - 查看服务日志：`Get-WinEvent -LogName System | Where-Object { $_.ProviderName -eq 'Service Control Manager' }`

### 方式 C：计划任务（开机登录后运行，无需管理员创建服务）

「任务计划程序」→ 创建任务：

- 常规：名称 `ArgusAgent`，勾选「使用最高权限运行」
- 触发器：新建 →「启动时」
- 操作：新建 → 程序/脚本 `C:\argus-agent\argus-agent.exe`，添加参数 `--config C:\argus-agent\agent.json`
- 设置：勾选「如果任务失败，按以下频率重新启动」→ 每 1 分钟，尝试 3 次

命令行等价（管理员 PowerShell）：

```powershell
schtasks /Create /TN "ArgusAgent" /TR "\"C:\argus-agent\argus-agent.exe\" --config \"C:\argus-agent\agent.json\"" /SC ONSTART /RL HIGHEST /F
schtasks /Run /TN "ArgusAgent"
```

> 与服务的区别：计划任务在用户登录会话中运行，注销可能影响；服务方式更推荐。

## 8. 说明

- Agent 二进制约 8.8MB，Go 单文件，Linux/Windows/macOS 通用（交叉编译）。
- 任务超时未回执（10 分钟）自动标记失败；Agent 领取任务后崩溃的场景由中心端
  惰性清理（领取前）与调度器扫描双重保障。
- Agent 在线状态：每次轮询更新心跳；心跳超过 90s 未更新自动置离线，前端 5 秒刷新实时显示。
