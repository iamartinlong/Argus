# Argus Windows IIS 部署指南

IIS 证书部署与通用 PEM 推送不同：IIS 需要 **PFX（PKCS#12）格式**导入 Windows 证书存储，
并通过 netsh / IIS 站点绑定更新 https 端点。Argus 在**服务端完成 PFX 合成**，
业务机仅接收文件并执行导入脚本（agent 场景）或由中心端经 SSH 推送执行（iis 目标）。

## 1. 前置条件（目标 Windows Server）

- Windows Server 2012 R2+（IIS 7.5+，WebAdministration/IISAdministration 模块）
- **OpenSSH Server**（iis 目标类型需要）：`Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0` + `Start-Service sshd`
- 部署账号具备**管理员权限**（导入证书存储 + netsh 需要）
- 端口未被其他进程占用（netsh https 绑定独占 ip:port）

## 2. 两种部署方式

### 方式 A：iis 部署目标（SSH 通道，推荐）

「部署目标」→ 新建 → 类型「Windows IIS」：

| 配置 | 说明 |
|---|---|
| 主机/端口/用户 | SSH 连接（OpenSSH Server），用户需管理员权限 |
| SSH 私钥 / 密码 | 认证凭据（推荐私钥，AES-GCM 加密存储） |
| PFX 落盘路径 | 服务端合成 PFX 后推送的临时路径（导入后自动删除） |
| HTTPS 绑定端口 | 逗号分隔（如 443,8443），netsh 更新 HTTP.sys https 绑定 |
| IIS 站点名（可选） | 填写后同时更新站点 https 绑定（WebAdministration） |
| known_hosts | 建议配置防中间人 |

流程：证书签发/续期 → 服务端合成 PFX（随机密码）→ SSH 推送 → PowerShell 导入
`LocalMachine\My` + netsh 更新绑定（先 `show sslcert` 检测已存在再替换）→ 可选站点绑定
→ 删除远端 PFX → 部署记录更新。

### 方式 B：Agent 通道（NAT/无 SSH 环境，自动绑定）

Agent 目标配置：

- 普通 PEM 路径（可选，IIS 主要用 PFX）
- **PFX 路径**：填 `C:\certs\example.pfx` 即触发服务端合成并附带（payload 含 base64 PFX + 随机密码）
- **HTTPS 端口**：逗号分隔（如 `443`），触发自动生成完整部署脚本（推荐，无需手写 hook）
- **IIS 站点名**：可选；填写后自动为该站点添加 https 绑定
- **部署后命令（hook）**：可选。**不填 HTTPS 端口时**保留自定义 hook；支持模板变量
  `{pfx_path} {pfx_password} {cert_path} {key_path} {domain}`，可直接引用服务端生成的动态 PFX 密码

Agent 收到 PFX 数据后写盘（0600）→ 执行脚本/hook → 完成后删除 PFX。

> 自动绑定（推荐）：配置 PFX 路径 + HTTPS 端口后，中心端用内置脚本完成
> `Import-PfxCertificate` 导入 `LocalMachine\My` + `netsh http add sslcert` 端口绑定
> + `appcmd set site` 站点绑定（无需 WebAdministration 模块），Agent 需以管理员权限运行。

## 3. 部署语义

- **HTTP.sys 层（netsh）**：`netsh http add sslcert ipport=0.0.0.0:PORT certhash=THUMB appid=...`
  ——对 IIS 站点的 https 绑定同样生效（IIS https 绑定底层走 HTTP.sys）
- **IIS 站点绑定**（site_name 非空）：`WebAdministration` 的 `New-WebBinding` +
  `AddSslCertificate(thumb, "My")`——同步站点级 https 绑定
- 证书导入位置：`LocalMachine\My`（当前固定）

## 4. 验证边界

- 单测覆盖：配置校验、v2 脚本渲染断言（show 预检/站点绑定/PFX 删除）、PFX 合成、agent 合成附带
- 端到端：SSH 传输 + 脚本执行链路（错误处理）；**真实 IIS 导入/绑定需 Windows Server 现场验证**
  （netsh/IISAdministration 属 Windows 专属，CI 无法覆盖）
