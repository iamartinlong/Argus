# Windows IIS 场景

IIS 证书部署使用 PFX（PKCS#12）格式。Argus 在**服务端完成 PFX 合成**，
业务机仅接收文件并执行导入脚本。

> **场景定位**：Windows IIS 是「应用场景」，可通过 SSH、Agent 或本机任一通道部署。
> IIS 需目标为 Windows 是场景的平台属性，非通道限制。

## 前置条件（目标 Windows Server）

- Windows Server 2012 R2+（IIS 7.5+）
- OpenSSH Server（SSH 通道）：`Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0` + `Start-Service sshd`
- 部署账号具备管理员权限（导入证书存储 + netsh）

## 通道方式

### SSH 通道（直连，推荐）

「部署目标」→ 新建 → 类型「SSH」→ 应用场景「Windows IIS」：

| 配置 | 说明 |
|---|---|
| 主机/端口/用户 | SSH 连接（OpenSSH Server），用户需管理员权限 |
| SSH 私钥/密码 | 认证凭据（AES-GCM 加密存储） |
| PFX 路径 | 服务端合成 PFX 推送的落盘路径（导入后自动删除） |
| HTTPS 绑定端口 | 逗号分隔（如 443,8443），netsh 更新 HTTP.sys https 绑定 |
| IIS 站点名（可选） | 填写后同时更新站点 https 绑定（appcmd） |
| known_hosts | 建议配置防中间人 |

### Agent 通道（NAT 环境）

Agent 目标配置应用场景「Windows IIS」：PFX 路径 + HTTPS 端口 + 站点名后，
中心端**自动生成完整 IIS 部署脚本**（agent 写盘 → 导入 → netsh/appcmd 绑定 → 删除）。

### 本机通道（服务端为 Windows）

本机目标配置应用场景「Windows IIS」：服务端本机合成 PFX 并执行导入/绑定脚本。
需服务端为 Windows（其他系统 Test 会拒绝）。

## 绑定语义

- **HTTP.sys 层（netsh）**：`netsh http add sslcert ipport=0.0.0.0:PORT certhash=THUMB appid=...`
  ——IIS 站点 https 绑定底层走 HTTP.sys，此更新同样生效
- **IIS 站点绑定**（site_name 非空）：appcmd `set site /+bindings`（幂等：先查重再重建）

## 实测验证经验

以下为本机（Windows Server / IIS 已启用）实测结论：

### 权限要求（关键）

证书导入 `LocalMachine\My` 需要**管理员权限**。不同通道的权限由谁承担：

| 通道 | 谁需要管理员 | 说明 |
|---|---|---|
| Agent | agent 进程 | agent 以管理员运行即可（建议服务化，见 agent 文档） |
| 本机 | **argus 服务进程** | 服务须以管理员运行，否则 `Import-PfxCertificate` 报 `0x80070005 (E_ACCESSDENIED)`，部署失败 |
| SSH | 远端 SSH 账号 | 账号需管理员权限（写入 LocalMachine 证书库 + netsh） |

> 常见坑：本机通道 IIS 场景部署失败 `0x80070005`，通常不是代码问题，而是 argus 服务以非管理员启动。
> 用管理员 PowerShell 重启服务即可解决（`Start-Process build\argus.exe serve`，首次会弹 UAC 确认）。

### 部署成功后的验证

```powershell
# 1. 检查 443 端口 HTTPS 绑定指纹（应为新证书）
netsh http show sslcert ipport=0.0.0.0:443 | Select-String "证书哈希|IP:端口"

# 2. 确认证书已装入本地机器存储
Get-ChildItem Cert:\LocalMachine\My | Select-Object Thumbprint, Subject, NotAfter

# 3. 确认远端 PFX 已自动清理（导入后删除，无残留）
```

### 实测结果

- **Agent 通道 + IIS 场景**：✅ 已实测通过（agent 以管理员运行）
- **本机通道 + IIS 场景**：✅ 已实测通过（服务以管理员运行）
- **SSH 通道 + IIS 场景**：代码路径与 Agent/本机共用 `deployIISOverSSH` + `RenderIISScriptV2`，
  单测覆盖脚本渲染与配置校验；真实 Windows OpenSSH 环境验证同上述方法

### 证书覆盖

新证书导入后 `netsh` 会**替换**同一端口的旧证书（先 `show sslcert` 预检再删除重建），
历史证书保留在 `LocalMachine\My` 但不影响绑定；如需清理可手动删除。

## 验证边界

单测覆盖配置校验、脚本渲染、PFX 合成；真实 IIS 导入/绑定需在 Windows Server 环境验证
（netsh/IISAdministration 为 Windows 专属）。
