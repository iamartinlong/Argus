# Agent 部署

Agent 用于无法 SSH 直连的业务机（NAT、无入站端口、Windows 服务器），出站连接中心端轮询部署任务。

## 应用场景

Agent 通道支持全部场景（通用/Nginx/Apache/Tomcat/Windows IIS）：
- **通用 / Nginx / Apache**：agent 写 PEM + Key + 执行命令
- **Tomcat**：服务端合成 PFX 附带，agent 写盘 + 可选命令
- **Windows IIS**：配置 PFX 路径 + HTTPS 端口 + 站点名后，中心端自动生成完整 IIS 脚本
  （证书导入 + netsh/appcmd 绑定），详见下方「IIS 场景」。

## 中心端准备

1. 「Agent 管理」→ 添加 Agent → 复制 Token（仅一次）
2. 「部署目标」→ 新建类型「Agent」绑定该 Agent

## 业务机配置

```bash
# 生成 agent.json（权限 600）
chmod 600 agent.json
argus-agent --config agent.json
```

```json
{
  "server_url": "https://cert.example.com",
  "token": "ag_xxxxxxxxxxxxxxxxxxxxxxxx",
  "poll_interval": 10
}
```

## 生产机安装清单

- **二进制**：`argus-agent` 拷贝到固定目录（Linux `/usr/local/bin/`、Windows `C:\argus-agent\`）
- **配置**：`server_url` 必须 HTTPS、`token` 中心端生成、`poll_interval` 建议 5-10s
- **Token 安全**：`agent.json` Linux `chmod 600`；泄漏时中心端删除重建（旧 Token 立即失效）
- **出站连通**：仅需业务机出站访问中心端（NAT/防火墙无需入站端口）
- **管理员权限**（Windows IIS 场景）：Agent 以管理员运行，否则无法导入证书与 netsh/appcmd 绑定
- **常驻**：Windows 一键脚本 `install-windows-service.ps1`、Linux 一键脚本 `install-systemd.sh`（均见下）
- **验证在线**：中心端「Agent 管理」确认状态在线、心跳刷新

## systemd 服务化（Linux）

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

```bash
sudo cp deploy/agent/argus-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now argus-agent
```

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

## Windows 服务化（推荐，开机自启）

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

## IIS 场景（Agent 通道，自动绑定）

配置 Agent 目标的 **PFX 路径 + HTTPS 端口 + IIS 站点名** 后，中心端**自动生成完整
IIS 部署脚本**（无需手写 hook）：

| 配置 | 说明 |
|---|---|
| PFX 路径 | 服务端合成 PFX（随机密码）随任务附带，Agent 写盘 → 导入 → 删除 |
| HTTPS 端口 | 逗号分隔（如 `443`），netsh 更新 HTTP.sys https 绑定 + appcmd 站点绑定 |
| IIS 站点名 | 可选；appcmd 为该站点添加 https 绑定（`*:端口:主域名`） |

自动流程：Agent 下载 PFX → `Import-PfxCertificate` 导入 `LocalMachine\My` →
`netsh http add sslcert` 绑定端口 → `appcmd set site` 添加站点 https 绑定 → 删除 PFX。
无需 `WebAdministration` 模块（用 IIS 自带 `appcmd.exe`），需 Agent 以**管理员权限**运行。

> 手写 hook（高级）：不填 HTTPS 端口时保留自定义 hook，支持模板变量
> `{pfx_path} {pfx_password} {cert_path} {key_path} {domain}`，IIS 导入脚本可引用动态 PFX 密码：
>
> ```powershell
> Import-PfxCertificate -FilePath '{pfx_path}' -CertStoreLocation Cert:\LocalMachine\My -Password (ConvertTo-SecureString '{pfx_password}' -AsPlainText -Force) -Exportable
> ```
>
> 完整 IIS 方案参见 [Windows IIS 场景](/deploy/deploy-targets/iis)。

## 实测验证经验（Docker 容器模拟业务机）

可用 Docker Desktop 在本地起 Linux 容器作为业务机，容器内跑 agent 出站连宿主机中心端，
验证 agent 通道全链路（已实测通过：agent + nginx / apache / tomcat 场景）。

### 拓扑

```
[宿主机] argus 中心端 :8443
  └─ [容器] 业务机镜像
       ├─ argus-agent（Linux 版）轮询 host.docker.internal:8443
       ├─ 共享卷 → 证书目录（agent 写文件）
       └─ 端口映射（宿主机 TLS 验证）
```

| 场景 | 镜像 | 共享卷挂载点 | 部署产物 |
|---|---|---|---|
| nginx | `nginx:alpine` | `/etc/nginx/certs` | PEM + Key（持久） |
| apache | `httpd:alpine` | `/usr/local/apache2/conf/certs` | PEM + Key（持久） |
| tomcat | `tomcat:10-jre17` | `/opt/tomcat/conf/certs` | PFX（临时，hook 消费后删除） |

### 关键步骤与坑

1. **agent 需交叉编译**：宿主机 Windows 构建的是 `.exe`，Linux 容器跑不了。必须：
   ```bash
   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o argus-agent ./cmd/argus-agent
   ```
2. **nginx 启动需证书先存在**：共享卷初始为空时，nginx 会因 `cannot load certificate` 启动失败。
   先用 `alpine` 临时容器把占位证书（可从中心端 `download?kind=pem/key` 下载现有证书）写入共享卷，再启动 nginx+agent 容器。
   （apache/tomcat 无此限制——apache 默认无 SSL 配置、tomcat 无需证书启动）
3. **容器访问宿主机**：`agent.json` 的 `server_url` 用 `http://host.docker.internal:8443`（Docker Desktop 内置别名）。
4. **验证链路**：部署触发后轮询 `deploy-records`/agent 任务 status=success，再 `curl --resolve` 用域名访问映射端口
   验证 TLS（证书 CN 匹配、未加 `-k`）：
   ```bash
   curl --resolve todo.example.com:4443:127.0.0.1 https://todo.example.com:4443/
   # HTTP=200 SSL_VERIFY=OK
   ```
5. **Tomcat/PFX 场景的临时性（重要）**：PFX 含私钥，agent 写盘 → **hook 执行 → 立即删除**。
   验证 PFX 落盘必须在 **hook 内消费**（如 `cp cert.pfx /path/keystore.pfx`），部署完成后原文件不存在是**预期行为**，
   不是失败。部署记录 message 固定为「部署完成」，hook 的 stdout 不回传。

### 已验证结果

- agent 注册上线（os=linux/amd64）
- 部署记录 + agent 任务均 success（nginx/apache/tomcat 三个场景）
- nginx/apache：PEM+Key 按 0644/0600 权限持久落盘共享卷
- tomcat：服务端合成 PFX（PKCS#12 头 `30 82...`）→ hook 内可访问/复制 → 原文件删除
- 宿主机 TLS 验证 HTTP=200、证书 CN 匹配（真实 Let's Encrypt 证书）

> 注意：本机 `curl` 在 PowerShell 中是 `Invoke-WebRequest` 别名，需用 `curl.exe` 显式调用。
